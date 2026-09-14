package scanner

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/tags"
	"github.com/ericwyn/tagger/internal/tags/taglibwasm"
)

type fakeEngine struct {
	snapshots map[string]tags.Snapshot
	errors    map[string]error
}

type artworkFakeEngine struct {
	fakeEngine
	artwork map[string][]byte
}

func (f artworkFakeEngine) ReadArtwork(_ context.Context, path string, _ int) ([]byte, error) {
	return f.artwork[filepath.Base(path)], nil
}

func (f artworkFakeEngine) WriteArtwork(context.Context, string, int, []byte, string) error {
	return nil
}

func (f fakeEngine) Read(_ context.Context, path string) (tags.Snapshot, error) {
	name := filepath.Base(path)
	if err := f.errors[name]; err != nil {
		return tags.Snapshot{}, err
	}
	if snapshot, ok := f.snapshots[name]; ok {
		return snapshot, nil
	}
	return tags.Snapshot{}, nil
}

func (f fakeEngine) Write(context.Context, string, map[string][]string) error { return nil }

func (f fakeEngine) Version() string { return "fake" }

func TestScanDiscoversAndNormalizesSupportedAudio(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Singer-Album", "Singer-Song.flac"), nil)
	mustWriteFile(t, filepath.Join(root, "Singer-Album", "Song Two.mp3"), nil)
	mustWriteFile(t, filepath.Join(root, "Singer-Album", "Song Two.lrc"), []byte("[00:01.00]sidecar"))
	mustWriteFile(t, filepath.Join(root, "root-artist.wav"), nil)
	mustWriteFile(t, filepath.Join(root, "voice.ogg"), nil)
	mustWriteFile(t, filepath.Join(root, "podcast.OPUS"), nil)
	mustWriteFile(t, filepath.Join(root, "notes.txt"), nil)
	mustWriteFile(t, filepath.Join(root, ".hidden", "ignored.mp3"), nil)
	mustWriteFile(t, filepath.Join(root, ".song.tagger-inflight.mp3"), nil)
	mustWriteFile(t, filepath.Join(root, ".hidden-file.flac"), nil)

	engine := fakeEngine{
		snapshots: map[string]tags.Snapshot{
			"Singer-Song.flac": {
				Raw: map[string][]string{
					"TITLE":                {"Song"},
					"ARTIST":               {"Singer", "Guest"},
					"ALBUM":                {"Album"},
					"ALBUMARTIST":          {"Singer"},
					"TRACKNUMBER":          {"2/10"},
					"DISCNUMBER":           {"1"},
					"DISCTOTAL":            {"2"},
					"DATE":                 {"2024-05-01"},
					"GENRE":                {"Pop", "Rock"},
					"LYRICS":               {"embedded lyrics"},
					"COMMENT":              {"liner note"},
					"COMPOSER":             {"Composer A", "Composer B"},
					"CONDUCTOR":            {"Conductor"},
					"LYRICIST":             {"Lyricist"},
					"COPYRIGHT":            {"© 2024 Label"},
					"BPM":                  {"128"},
					"ISRC":                 {"US-ABC-24-00001"},
					"MUSICBRAINZ_TRACKID":  {"track-mbid"},
					"MUSICBRAINZ_ALBUMID":  {"release-mbid"},
					"MUSICBRAINZ_ARTISTID": {"artist-mbid-1", "artist-mbid-2"},
					"ACOUSTID_ID":          {"acoustid-id"},
					"ACOUSTID_FINGERPRINT": {"fingerprint"},
				},
				DurationSeconds: 241,
				ArtworkCount:    1,
				Properties: domain.TrackProperties{
					Container: "FLAC", Codec: "FLAC", BitrateKbps: 900,
					SampleRateHz: 44100, BitDepth: 16, Channels: 2,
				},
			},
		},
		errors: map[string]error{"root-artist.wav": errors.New("broken RIFF")},
	}

	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	musicScanner, err := New(engine, Options{Root: root, LibraryName: "Fixture", Workers: 2, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	result, err := musicScanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Library.TrackCount != 5 || result.Library.FolderCount != 2 {
		t.Fatalf("library = %#v", result.Library)
	}
	if result.Report.Discovered != 5 || result.Report.Parsed != 4 || result.Report.Failed != 1 {
		t.Fatalf("report = %#v", result.Report)
	}

	var flac, mp3, wav domain.Track
	oggCount := 0
	for _, track := range result.Tracks {
		switch track.Format {
		case domain.FormatFLAC:
			flac = track
		case domain.FormatMP3:
			mp3 = track
		case domain.FormatWAV:
			wav = track
		case domain.FormatOGG:
			oggCount++
		}
	}
	if oggCount != 2 {
		t.Fatalf("ogg tracks = %d, want .ogg and .opus", oggCount)
	}
	if flac.Title != "Song" || len(flac.Artists) != 2 || flac.DurationSeconds != 241 || flac.Health != domain.HealthComplete {
		t.Fatalf("flac normalization = %#v", flac)
	}
	if flac.TrackNumber == nil || *flac.TrackNumber != 2 || flac.TrackTotal == nil || *flac.TrackTotal != 10 {
		t.Fatalf("flac track index = %v/%v", flac.TrackNumber, flac.TrackTotal)
	}
	if flac.Year == nil || *flac.Year != 2024 || flac.DiscTotal == nil || *flac.DiscTotal != 2 {
		t.Fatalf("flac date/disc = %#v", flac)
	}
	if flac.Comment != "liner note" || len(flac.Composers) != 2 || flac.Conductor != "Conductor" || len(flac.Lyricists) != 1 || flac.Copyright != "© 2024 Label" {
		t.Fatalf("flac people/description = %#v", flac)
	}
	if flac.BPM == nil || *flac.BPM != 128 || flac.ISRC != "US-ABC-24-00001" || flac.MusicBrainzTrackID != "track-mbid" || flac.MusicBrainzReleaseID != "release-mbid" || len(flac.MusicBrainzArtistIDs) != 2 || flac.AcoustID != "acoustid-id" || flac.AcoustIDFingerprint != "fingerprint" {
		t.Fatalf("flac identifiers = %#v", flac)
	}
	if mp3.Title != "" || len(mp3.Artists) != 0 || mp3.Album != "" || len(mp3.AlbumArtists) != 0 {
		t.Fatalf("embedded tags must remain empty = %#v", mp3)
	}
	if len(mp3.TagHints) != 1 || mp3.TagHints[0].Title != "Song Two" || !slices.Equal(mp3.TagHints[0].Artists, []string{"Singer"}) || mp3.TagHints[0].Album != "Album" {
		t.Fatalf("path hints = %#v", mp3.TagHints)
	}
	if !slices.Equal(mp3.TagIssues, []domain.TagIssue{domain.TagIssueMissingTitle, domain.TagIssueMissingArtist, domain.TagIssueMissingAlbum, domain.TagIssueMissingAlbumArtist}) {
		t.Fatalf("tag issues = %#v", mp3.TagIssues)
	}
	if mp3.Lyrics != "[00:01.00]sidecar" || mp3.Health != domain.HealthTagCompatibility {
		t.Fatalf("sidecar/health = %#v", mp3)
	}
	if mp3.LyricsSidecar == nil || !mp3.LyricsSidecar.Exists || mp3.LyricsSidecar.Revision == "" || mp3.LyricsSidecar.SizeBytes == 0 {
		t.Fatalf("sidecar info = %#v", mp3.LyricsSidecar)
	}
	if wav.Health != domain.HealthParseError || wav.ParseError == "" {
		t.Fatalf("parse error = %#v", wav)
	}
}

func TestFilenameHintsRemainAmbiguousAndNeverBecomeTags(t *testing.T) {
	hints := tagHintsFromPath("music/数码宝贝/宮崎歩-brave heart.mp3")
	if len(hints) != 2 {
		t.Fatalf("hints = %#v", hints)
	}
	if hints[0].Pattern != "title-artist" || hints[0].Title != "宮崎歩" || !slices.Equal(hints[0].Artists, []string{"brave heart"}) {
		t.Fatalf("title-artist hint = %#v", hints[0])
	}
	if hints[1].Pattern != "artist-title" || hints[1].Title != "brave heart" || !slices.Equal(hints[1].Artists, []string{"宮崎歩"}) {
		t.Fatalf("artist-title hint = %#v", hints[1])
	}
	track := fallbackTrack("music/数码宝贝/宮崎歩-brave heart.mp3", domain.FormatMP3)
	if track.Title != "" || len(track.Artists) != 0 || track.Health != domain.HealthTagCompatibility {
		t.Fatalf("fallback track promoted a hint = %#v", track)
	}
}

func TestSupportedAudioExtensionMappingIsExplicit(t *testing.T) {
	tests := map[string]domain.TrackFormat{
		"track.mp3":  domain.FormatMP3,
		"track.FLAC": domain.FormatFLAC,
		"track.wave": domain.FormatWAV,
		"track.ogg":  domain.FormatOGG,
		"track.OPUS": domain.FormatOGG,
	}
	for path, want := range tests {
		got, ok := formatFromPath(path)
		if !ok || got != want || !isSupportedAudio(path) {
			t.Errorf("formatFromPath(%q) = %q, %v; want %q, true", path, got, ok, want)
		}
	}
	if got, ok := formatFromPath("track.aac"); ok || got != "" || isSupportedAudio("track.aac") {
		t.Fatalf("unknown extension mapped to %q, %v", got, ok)
	}
}

func TestFilenameHintsStripTrackPrefixesAndUnicodeSeparators(t *testing.T) {
	hints := tagHintsFromPath("Artist/Album/CD1 07 - Artist – Song Title.flac")
	if len(hints) != 2 {
		t.Fatalf("hints = %#v", hints)
	}
	if hints[0].Title != "Artist" || !slices.Equal(hints[0].Artists, []string{"Song Title"}) {
		t.Fatalf("title-artist hint = %#v", hints[0])
	}
	if hints[1].Title != "Song Title" || !slices.Equal(hints[1].Artists, []string{"Artist"}) {
		t.Fatalf("artist-title hint = %#v", hints[1])
	}
}

func TestTagIssuesDetectSuspiciousAlbumArtist(t *testing.T) {
	track := fallbackTrack("brave heart-宮崎歩.mp3", domain.FormatMP3)
	applySnapshot(&track, tags.Snapshot{Raw: map[string][]string{
		"TITLE": {"brave heart"}, "ARTIST": {"宮崎歩"},
		"ALBUM": {"デジモンエンディングベスト"}, "ALBUMARTIST": {"デジモンエンディングベスト"},
	}})
	track.Health = healthFor(track)
	if track.Health != domain.HealthTagCompatibility || !slices.Contains(track.TagIssues, domain.TagIssueSuspiciousAlbumArtist) {
		t.Fatalf("suspicious album artist = %#v", track)
	}
}

func TestLegacyAliasTagsDoNotHideMissingStandardCompatibilityFields(t *testing.T) {
	track := fallbackTrack("legacy.mp3", domain.FormatMP3)
	applySnapshot(&track, tags.Snapshot{Raw: map[string][]string{
		"SUBTITLE":     {"Not a TIT2 title"},
		"ARTISTS":      {"Not a TPE1 artist"},
		"ALBUM":        {"Album"},
		"ALBUM ARTIST": {"Legacy album artist"},
	}})
	if track.Title != "" || len(track.Artists) != 0 || len(track.AlbumArtists) != 0 {
		t.Fatalf("legacy aliases promoted to standard fields: %#v", track)
	}
	for _, issue := range []domain.TagIssue{domain.TagIssueMissingTitle, domain.TagIssueMissingArtist, domain.TagIssueMissingAlbumArtist} {
		if !slices.Contains(track.TagIssues, issue) {
			t.Fatalf("missing issue %q in %#v", issue, track.TagIssues)
		}
	}
}

func TestScanRecordsEmbeddedArtworkDimensions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cover.flac")
	mustWriteFile(t, path, nil)
	var imageData bytes.Buffer
	imageToEncode := image.NewRGBA(image.Rect(0, 0, 8, 4))
	imageToEncode.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&imageData, imageToEncode); err != nil {
		t.Fatal(err)
	}
	engine := artworkFakeEngine{
		fakeEngine: fakeEngine{snapshots: map[string]tags.Snapshot{
			"cover.flac": {Raw: map[string][]string{"TITLE": {"Cover"}}, ArtworkCount: 1},
		}},
		artwork: map[string][]byte{"cover.flac": imageData.Bytes()},
	}
	musicScanner, err := New(engine, Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	result, err := musicScanner.Scan(context.Background())
	if err != nil || len(result.Tracks) != 1 {
		t.Fatalf("scan = %#v err=%v", result, err)
	}
	track := result.Tracks[0]
	if track.ArtworkWidth != 8 || track.ArtworkHeight != 4 || track.ArtworkSizeBytes != int64(len(imageData.Bytes())) {
		t.Fatalf("artwork dimensions = %dx%d %d, want 8x4 %d", track.ArtworkWidth, track.ArtworkHeight, track.ArtworkSizeBytes, imageData.Len())
	}
}

func TestScanDoesNotFollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.mp3")
	mustWriteFile(t, outside, nil)
	if err := os.Symlink(outside, filepath.Join(root, "escape.mp3")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	musicScanner, err := New(fakeEngine{}, Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := musicScanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tracks) != 0 {
		t.Fatalf("symlink target was scanned: %#v", result.Tracks)
	}
}

func TestScannerReadsTestMusicCorpus(t *testing.T) {
	root := os.Getenv("TAGGER_TEST_MUSIC_DIR")
	if root == "" {
		root = "/home/ericwyn/Downloads/TestMusic"
	}
	if _, err := os.Stat(root); err != nil {
		t.Skipf("TestMusic corpus unavailable: %v", err)
	}

	musicScanner, err := New(taglibwasm.New(), Options{Root: root, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := musicScanner.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Failed != 0 {
		for _, track := range result.Tracks {
			if track.ParseError != "" {
				t.Logf("parse error: %s: %s", track.RelativePath, track.ParseError)
			}
		}
		t.Fatalf("failed to parse %d/%d TestMusic files", result.Report.Failed, result.Report.Discovered)
	}
	expectedFormats := map[domain.TrackFormat]int{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".flac":
			expectedFormats[domain.FormatFLAC]++
		case ".mp3":
			expectedFormats[domain.FormatMP3]++
		case ".wav":
			expectedFormats[domain.FormatWAV]++
		case ".ogg", ".opus":
			expectedFormats[domain.FormatOGG]++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedTotal := expectedFormats[domain.FormatFLAC] + expectedFormats[domain.FormatMP3] + expectedFormats[domain.FormatWAV] + expectedFormats[domain.FormatOGG]
	if len(result.Tracks) != expectedTotal {
		t.Fatalf("tracks = %d, want %d supported files", len(result.Tracks), expectedTotal)
	}
	formats := map[domain.TrackFormat]int{}
	for _, track := range result.Tracks {
		formats[track.Format]++
		if track.Title == "" || track.DurationSeconds <= 0 || track.Properties.SampleRateHz <= 0 {
			t.Errorf("incomplete parsed track: %#v", track)
		}
	}
	if formats[domain.FormatFLAC] != expectedFormats[domain.FormatFLAC] || formats[domain.FormatMP3] != expectedFormats[domain.FormatMP3] || formats[domain.FormatWAV] != expectedFormats[domain.FormatWAV] || formats[domain.FormatOGG] != expectedFormats[domain.FormatOGG] {
		t.Fatalf("format counts = %#v, want %#v", formats, expectedFormats)
	}
}

func TestScannerWithRootPreservesEngineAndWorkerPolicy(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(second, "next.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	original, err := New(fakeEngine{}, Options{Root: first, LibraryName: "First", Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	next, err := original.WithRoot(second)
	if err != nil {
		t.Fatal(err)
	}
	if next.Root() == original.Root() || next.Root() != second {
		t.Fatalf("roots original=%q next=%q", original.Root(), next.Root())
	}
	result, err := next.Scan(context.Background())
	if err != nil || len(result.Tracks) != 1 {
		t.Fatalf("next scan tracks=%d err=%v", len(result.Tracks), err)
	}
}

func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}
