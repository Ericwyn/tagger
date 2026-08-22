package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if result.Library.TrackCount != 3 || result.Library.FolderCount != 2 {
		t.Fatalf("library = %#v", result.Library)
	}
	if result.Report.Discovered != 3 || result.Report.Parsed != 2 || result.Report.Failed != 1 {
		t.Fatalf("report = %#v", result.Report)
	}

	var flac, mp3, wav domain.Track
	for _, track := range result.Tracks {
		switch track.Format {
		case domain.FormatFLAC:
			flac = track
		case domain.FormatMP3:
			mp3 = track
		case domain.FormatWAV:
			wav = track
		}
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
	if mp3.Title != "Song Two" || len(mp3.Artists) != 1 || mp3.Artists[0] != "Singer" || mp3.Album != "Album" {
		t.Fatalf("filename fallback = %#v", mp3)
	}
	if mp3.Lyrics != "[00:01.00]sidecar" || mp3.Health != domain.HealthMissingArtwork {
		t.Fatalf("sidecar/health = %#v", mp3)
	}
	if mp3.LyricsSidecar == nil || !mp3.LyricsSidecar.Exists || mp3.LyricsSidecar.Revision == "" || mp3.LyricsSidecar.SizeBytes == 0 {
		t.Fatalf("sidecar info = %#v", mp3.LyricsSidecar)
	}
	if wav.Health != domain.HealthParseError || wav.ParseError == "" {
		t.Fatalf("parse error = %#v", wav)
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
	if len(result.Tracks) != 24 {
		t.Fatalf("tracks = %d, want 24", len(result.Tracks))
	}
	formats := map[domain.TrackFormat]int{}
	for _, track := range result.Tracks {
		formats[track.Format]++
		if track.Title == "" || track.DurationSeconds <= 0 || track.Properties.SampleRateHz <= 0 {
			t.Errorf("incomplete parsed track: %#v", track)
		}
	}
	if formats[domain.FormatFLAC] != 15 || formats[domain.FormatMP3] != 9 {
		t.Fatalf("format counts = %#v", formats)
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
