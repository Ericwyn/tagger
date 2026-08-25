package filewrite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/tags"
	"github.com/ericwyn/tagger/internal/tags/taglibwasm"
)

type memoryEngine struct {
	mu             sync.Mutex
	initial        map[string][]string
	latest         map[string][]string
	byPath         map[string]map[string][]string
	writes         int
	breakVerifyKey string
}

type artworkMemoryEngine struct {
	*memoryEngine
	artMu    sync.Mutex
	initial  []byte
	latest   []byte
	artworks map[string][]byte
}

func newArtworkMemoryEngine(raw map[string][]string, initial []byte) *artworkMemoryEngine {
	return &artworkMemoryEngine{
		memoryEngine: newMemoryEngine(raw), initial: append([]byte(nil), initial...), artworks: make(map[string][]byte),
	}
}

func (e *artworkMemoryEngine) Read(ctx context.Context, path string) (tags.Snapshot, error) {
	snapshot, err := e.memoryEngine.Read(ctx, path)
	if err != nil {
		return tags.Snapshot{}, err
	}
	imageData, err := e.ReadArtwork(ctx, path, 0)
	if err != nil {
		return tags.Snapshot{}, err
	}
	if len(imageData) == 0 {
		snapshot.ArtworkCount = 0
	} else {
		snapshot.ArtworkCount = 1
	}
	return snapshot, nil
}

func (e *artworkMemoryEngine) ReadArtwork(_ context.Context, path string, _ int) ([]byte, error) {
	e.artMu.Lock()
	defer e.artMu.Unlock()
	data, exists := e.artworks[path]
	if !exists {
		data = e.latest
	}
	if data == nil && e.latest == nil {
		data = e.initial
	}
	return append([]byte(nil), data...), nil
}

func (e *artworkMemoryEngine) WriteArtwork(_ context.Context, path string, _ int, image []byte, _ string) error {
	e.artMu.Lock()
	defer e.artMu.Unlock()
	e.latest = append([]byte{}, image...)
	e.artworks[path] = append([]byte{}, image...)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := file.Write([]byte{byte(len(image) % 251)}); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func newMemoryEngine(raw map[string][]string) *memoryEngine {
	return &memoryEngine{initial: cloneRaw(raw), byPath: make(map[string]map[string][]string)}
}

func (e *memoryEngine) Read(_ context.Context, path string) (tags.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	raw := e.byPath[path]
	if raw == nil {
		raw = e.latest
	}
	if raw == nil {
		raw = e.initial
	}
	return tags.Snapshot{Raw: cloneRaw(raw), DurationSeconds: 120, ArtworkCount: 1}, nil
}

func (e *memoryEngine) Write(_ context.Context, path string, updates map[string][]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.writes++
	next := cloneRaw(e.initial)
	for key, values := range updates {
		if key == e.breakVerifyKey {
			continue
		}
		if len(values) == 0 {
			delete(next, key)
		} else {
			next[key] = append([]string(nil), values...)
		}
	}
	e.byPath[path] = next
	e.latest = cloneRaw(next)
	return nil
}

func (e *memoryEngine) Version() string { return "memory" }

func TestWriterUsesVerifiedTemporaryCopy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.mp3")
	originalBytes := []byte("fake audio bytes remain untouched by the tag double")
	if err := os.WriteFile(path, originalBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	engine := newMemoryEngine(map[string][]string{
		"TITLE":  {"Old title"},
		"ARTIST": {"Artist"},
		"GENRE":  {"Pop"},
		"LYRICS": {"old lyrics"},
	})
	writer, err := New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	ref := testFileRef(t, root, path, domain.FormatMP3, engine)
	patch := domain.TagPatch{
		Title:  &domain.StringFieldPatch{Op: domain.OperationSet, Value: "New title"},
		Genres: &domain.StringsFieldPatch{Op: domain.OperationMerge, Value: []string{"Rock", "Pop"}},
		Lyrics: &domain.StringFieldPatch{Op: domain.OperationDelete},
	}

	preview, err := writer.Write(context.Background(), ref, ref.Revision, patch, true)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun || !preview.Changed || len(preview.Diff) != 3 || engine.writes != 0 {
		t.Fatalf("preview = %#v writes=%d", preview, engine.writes)
	}

	result, err := writer.Write(context.Background(), ref, ref.Revision, patch, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.CurrentRevision == ref.Revision || engine.writes != 1 {
		t.Fatalf("result = %#v writes=%d", result, engine.writes)
	}
	if firstRaw(result.BeforeTags, "TITLE") != "Old title" || firstRaw(result.AfterTags, "TITLE") != "New title" {
		t.Fatalf("writer did not retain history snapshots: before=%v after=%v", result.BeforeTags, result.AfterTags)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(originalBytes) {
		t.Fatalf("audio payload changed: %q", content)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(root, ".song.tagger-*.mp3")); len(leftovers) != 0 {
		t.Fatalf("temporary files leaked: %v", leftovers)
	}

	_, err = writer.Write(context.Background(), ref, ref.Revision, patch, false)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale write error = %v", err)
	}
}

func TestWriterPatchesExtendedEmbeddedFields(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "extended.flac")
	if err := os.WriteFile(path, []byte("fake audio"), 0o640); err != nil {
		t.Fatal(err)
	}
	engine := newMemoryEngine(map[string][]string{
		"TITLE":    {"Song"},
		"COMMENT":  {"old note"},
		"COMPOSER": {"Old Composer"},
		"BPM":      {"90"},
	})
	writer, err := New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	ref := testFileRef(t, root, path, domain.FormatFLAC, engine)
	patch := domain.TagPatch{
		Comment:              &domain.StringFieldPatch{Op: domain.OperationSet, Value: "liner note"},
		Composers:            &domain.StringsFieldPatch{Op: domain.OperationSet, Value: []string{"Composer A", "Composer B"}},
		Conductor:            &domain.StringFieldPatch{Op: domain.OperationSet, Value: "Conductor"},
		Lyricists:            &domain.StringsFieldPatch{Op: domain.OperationSet, Value: []string{"Lyricist"}},
		Copyright:            &domain.StringFieldPatch{Op: domain.OperationSet, Value: "© 2024 Label"},
		BPM:                  &domain.IntFieldPatch{Op: domain.OperationSet, Value: 128},
		ISRC:                 &domain.StringFieldPatch{Op: domain.OperationSet, Value: "US-ABC-24-00001"},
		MusicBrainzTrackID:   &domain.StringFieldPatch{Op: domain.OperationSet, Value: "track-mbid"},
		MusicBrainzReleaseID: &domain.StringFieldPatch{Op: domain.OperationSet, Value: "release-mbid"},
		MusicBrainzArtistIDs: &domain.StringsFieldPatch{Op: domain.OperationSet, Value: []string{"artist-mbid-1", "artist-mbid-2"}},
		AcoustID:             &domain.StringFieldPatch{Op: domain.OperationSet, Value: "acoustid-id"},
		AcoustIDFingerprint:  &domain.StringFieldPatch{Op: domain.OperationSet, Value: "fingerprint"},
	}
	result, err := writer.Write(context.Background(), ref, ref.Revision, patch, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || len(result.Diff) != 12 {
		t.Fatalf("extended write result = %#v", result)
	}
	checks := map[string][]string{
		"COMMENT": {"liner note"}, "COMPOSER": {"Composer A", "Composer B"}, "CONDUCTOR": {"Conductor"},
		"LYRICIST": {"Lyricist"}, "COPYRIGHT": {"© 2024 Label"}, "BPM": {"128"}, "ISRC": {"US-ABC-24-00001"},
		"MUSICBRAINZ_TRACKID": {"track-mbid"}, "MUSICBRAINZ_ALBUMID": {"release-mbid"},
		"MUSICBRAINZ_ARTISTID": {"artist-mbid-1", "artist-mbid-2"}, "ACOUSTID_ID": {"acoustid-id"}, "ACOUSTID_FINGERPRINT": {"fingerprint"},
	}
	for key, want := range checks {
		if got := result.AfterTags[key]; !slices.Equal(got, want) {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestWriterWritesAndDeletesLyricsSidecarWithRevisionGuard(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.mp3")
	if err := os.WriteFile(path, []byte("fake audio"), 0o640); err != nil {
		t.Fatal(err)
	}
	engine := newMemoryEngine(map[string][]string{"TITLE": {"Song"}})
	writer, err := New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	ref := testFileRef(t, root, path, domain.FormatMP3, engine)
	content := "[00:01.00]Hello\n"
	result, err := writer.WriteSidecar(context.Background(), ref, ref.Revision, "", &content, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.CurrentSidecarRevision != domain.SidecarRevision([]byte(content)) || result.BeforeContent != "" || result.AfterContent != content || result.After == nil || !result.After.Exists {
		t.Fatalf("sidecar write result = %#v", result)
	}
	sidecarPath := filepath.Join(root, "song.lrc")
	if got, err := os.ReadFile(sidecarPath); err != nil || string(got) != content {
		t.Fatalf("sidecar content = %q err=%v", got, err)
	}
	if _, err := writer.WriteSidecar(context.Background(), ref, ref.Revision, "stale", &content, false); !errors.Is(err, ErrSidecarConflict) {
		t.Fatalf("stale sidecar write error = %v", err)
	}
	deleted, err := writer.WriteSidecar(context.Background(), ref, ref.Revision, result.CurrentSidecarRevision, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Changed || deleted.BeforeContent != content || deleted.After != nil || deleted.AfterContent != "" {
		t.Fatalf("sidecar delete result = %#v", deleted)
	}
	if _, err := os.Stat(sidecarPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sidecar still exists: %v", err)
	}
}

func TestWriterRestoresManagedSnapshotAndPreservesPrivateTags(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.flac")
	if err := os.WriteFile(path, []byte("fake audio"), 0o640); err != nil {
		t.Fatal(err)
	}
	engine := newMemoryEngine(map[string][]string{
		"TITLE": {"Current title"}, "ARTIST": {"Current artist"}, "GENRE": {"Rock"},
		"PRIVATE:OWNER": {"keep-current-private-value"},
	})
	writer, err := New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	ref := testFileRef(t, root, path, domain.FormatFLAC, engine)
	target := map[string][]string{
		"TITLE": {"Historic title"}, "ARTIST": {"Artist A", "Artist B"},
		"PRIVATE:OWNER": {"historic-private-value-must-not-be-restored"},
	}

	preview, err := writer.Restore(context.Background(), ref, ref.Revision, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun || len(preview.Diff) != 3 || engine.writes != 0 || len(preview.Warnings) != 1 {
		t.Fatalf("restore preview = %#v writes=%d", preview, engine.writes)
	}
	result, err := writer.Restore(context.Background(), ref, ref.Revision, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || engine.writes != 1 {
		t.Fatalf("restore result = %#v writes=%d", result, engine.writes)
	}
	if firstRaw(result.AfterTags, "TITLE") != "Historic title" ||
		!slices.Equal(result.AfterTags["ARTIST"], []string{"Artist A", "Artist B"}) ||
		len(result.AfterTags["GENRE"]) != 0 ||
		firstRaw(result.AfterTags, "PRIVATE:OWNER") != "keep-current-private-value" {
		t.Fatalf("restored tags = %#v", result.AfterTags)
	}
	_, err = writer.Restore(context.Background(), ref, ref.Revision, target, false)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale restore error = %v", err)
	}
}

func TestWriterReplacesAndDeletesArtworkThroughVerifiedCopy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.mp3")
	if err := os.WriteFile(path, []byte("fake audio"), 0o640); err != nil {
		t.Fatal(err)
	}
	initial := testPNG(t, 1, 1)
	target, err := artwork.Validate(testPNG(t, 3, 2), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	engine := newArtworkMemoryEngine(map[string][]string{"TITLE": {"Song"}}, initial)
	writer, err := New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	ref := testFileRef(t, root, path, domain.FormatMP3, engine)
	read, err := writer.ReadArtwork(context.Background(), ref, 0)
	if err != nil || read.Width != 1 || read.Height != 1 {
		t.Fatalf("read artwork = %#v err=%v", read, err)
	}
	preview, err := writer.WriteArtwork(context.Background(), ref, ref.Revision, 0, &target, true)
	if err != nil || !preview.DryRun || !preview.Changed || len(preview.Diff) != 1 {
		t.Fatalf("preview = %#v err=%v", preview, err)
	}
	result, err := writer.WriteArtwork(context.Background(), ref, ref.Revision, 0, &target, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.After == nil || result.After.Hash != target.Hash || result.CurrentRevision == ref.Revision {
		t.Fatalf("write result = %#v", result)
	}

	deleteRef := testFileRef(t, root, path, domain.FormatMP3, engine)
	deleted, err := writer.WriteArtwork(context.Background(), deleteRef, deleteRef.Revision, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Changed || deleted.After != nil {
		t.Fatalf("delete result = %#v", deleted)
	}
	if _, err := writer.ReadArtwork(context.Background(), deleteRef, 0); !errors.Is(err, ErrArtworkNotFound) {
		t.Fatalf("read deleted artwork error = %v", err)
	}
}

func TestWriterDoesNotReplaceSourceWhenVerificationFails(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.flac")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := newMemoryEngine(map[string][]string{"TITLE": {"Old"}})
	engine.breakVerifyKey = "TITLE"
	writer, err := New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	ref := testFileRef(t, root, path, domain.FormatFLAC, engine)
	_, err = writer.Write(context.Background(), ref, ref.Revision, domain.TagPatch{
		Title: &domain.StringFieldPatch{Op: domain.OperationSet, Value: "New"},
	}, false)
	if !errors.Is(err, ErrVerification) {
		t.Fatalf("error = %v, want verification failure", err)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "original" {
		t.Fatalf("source was replaced after failed verification: %q", content)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(root, ".song.tagger-*.flac")); len(leftovers) != 0 {
		t.Fatalf("temporary files leaked: %v", leftovers)
	}
}

func TestWriterRejectsPathEscapeAndSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.mp3")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := newMemoryEngine(map[string][]string{"TITLE": {"Old"}})
	writer, err := New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	patch := domain.TagPatch{Title: &domain.StringFieldPatch{Op: domain.OperationSet, Value: "New"}}
	_, err = writer.Write(context.Background(), library.FileRef{
		RelativePath: "../outside.mp3", AbsolutePath: outside, Revision: "rev", Format: domain.FormatMP3,
	}, "rev", patch, false)
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("escape error = %v", err)
	}

	symlink := filepath.Join(root, "link.mp3")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	_, err = writer.Write(context.Background(), library.FileRef{
		RelativePath: "link.mp3", AbsolutePath: symlink, Revision: "rev", Format: domain.FormatMP3,
	}, "rev", patch, false)
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestWriterSetRootChangesVerifiedBoundary(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "first.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "second.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	writer, err := New(first, newMemoryEngine(map[string][]string{"TITLE": {"song"}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.SetRoot(second); err != nil {
		t.Fatal(err)
	}
	if writer.Root() != second {
		t.Fatalf("writer root=%q", writer.Root())
	}
	ref := library.FileRef{RelativePath: "second.mp3", AbsolutePath: filepath.Join(second, "second.mp3"), Format: domain.FormatMP3}
	if _, err := writer.OpenRead(ref); err != nil {
		t.Fatalf("open switched root file: %v", err)
	}
}

func TestWriterWithCopiedTestMusicMP3AndFLAC(t *testing.T) {
	corpus := os.Getenv("TAGGER_TEST_MUSIC_DIR")
	if corpus == "" {
		corpus = "/home/ericwyn/Downloads/TestMusic"
	}
	if _, err := os.Stat(corpus); err != nil {
		t.Skipf("TestMusic corpus unavailable: %v", err)
	}

	for _, extension := range []string{".mp3", ".flac"} {
		t.Run(strings.TrimPrefix(extension, "."), func(t *testing.T) {
			source := findAudio(t, corpus, extension)
			root := t.TempDir()
			destination := filepath.Join(root, "fixture"+extension)
			copyFixture(t, source, destination)

			engine := taglibwasm.New()
			before, err := engine.Read(context.Background(), destination)
			if err != nil {
				t.Fatal(err)
			}
			originalTitle := firstRaw(before.Raw, "TITLE")
			if originalTitle == "" {
				t.Fatal("fixture has no title")
			}
			writer, err := New(root, engine)
			if err != nil {
				t.Fatal(err)
			}
			ref := testFileRef(t, root, destination, domain.TrackFormat(strings.TrimPrefix(extension, ".")), engine)
			result, err := writer.Write(context.Background(), ref, ref.Revision, domain.TagPatch{
				Title:  &domain.StringFieldPatch{Op: domain.OperationSet, Value: originalTitle + " [Tagger Test]"},
				Lyrics: &domain.StringFieldPatch{Op: domain.OperationDelete},
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Changed {
				t.Fatal("expected real file change")
			}
			after, err := engine.Read(context.Background(), destination)
			if err != nil {
				t.Fatal(err)
			}
			if got := firstRaw(after.Raw, "TITLE"); got != originalTitle+" [Tagger Test]" {
				t.Fatalf("written title = %q", got)
			}
			if got := firstRaw(after.Raw, "LYRICS"); got != "" {
				t.Fatalf("lyrics were not deleted: %q", got)
			}
			if after.ArtworkCount != before.ArtworkCount {
				t.Fatalf("artwork count changed: %d -> %d", before.ArtworkCount, after.ArtworkCount)
			}
			for key, beforeValues := range before.Raw {
				if strings.EqualFold(key, "TITLE") || strings.EqualFold(key, "LYRICS") {
					continue
				}
				if afterValues := rawForKey(after.Raw, key); !slices.Equal(beforeValues, afterValues) {
					t.Fatalf("unmentioned tag %s changed: %q -> %q", key, beforeValues, afterValues)
				}
			}
			originalAfter, err := engine.Read(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			if got := firstRaw(originalAfter.Raw, "TITLE"); got != originalTitle {
				t.Fatalf("source corpus was modified: %q", got)
			}
		})
	}
}

func TestWriterWithCopiedTestMusicArtwork(t *testing.T) {
	corpus := os.Getenv("TAGGER_TEST_MUSIC_DIR")
	if corpus == "" {
		corpus = "/home/ericwyn/Downloads/TestMusic"
	}
	if _, err := os.Stat(corpus); err != nil {
		t.Skipf("TestMusic corpus unavailable: %v", err)
	}
	for _, extension := range []string{".mp3", ".flac"} {
		t.Run(strings.TrimPrefix(extension, "."), func(t *testing.T) {
			source := findAudio(t, corpus, extension)
			root := t.TempDir()
			destination := filepath.Join(root, "artwork-fixture"+extension)
			copyFixture(t, source, destination)
			verifyArtworkRoundTrip(t, root, destination, domain.TrackFormat(strings.TrimPrefix(extension, ".")))
		})
	}
	t.Run("wav", func(t *testing.T) {
		root := t.TempDir()
		destination := filepath.Join(root, "artwork-fixture.wav")
		writeSilentWAV(t, destination)
		verifyArtworkRoundTrip(t, root, destination, domain.FormatWAV)
	})
}

func verifyArtworkRoundTrip(t *testing.T, root, destination string, format domain.TrackFormat) {
	t.Helper()
	engine := taglibwasm.New()
	before, err := engine.Read(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := artwork.Validate(testPNG(t, 4, 3), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	writer, err := New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	ref := testFileRef(t, root, destination, format, engine)
	written, err := writer.WriteArtwork(context.Background(), ref, ref.Revision, 0, &asset, false)
	if err != nil {
		t.Fatal(err)
	}
	if written.After == nil || written.After.Hash != asset.Hash {
		t.Fatalf("written artwork = %#v", written)
	}
	read, err := writer.ReadArtwork(context.Background(), ref, 0)
	if err != nil || read.Hash != asset.Hash || read.Width != 4 || read.Height != 3 {
		t.Fatalf("read artwork = %#v err=%v", read, err)
	}
	afterWrite, err := engine.Read(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	if afterWrite.ArtworkCount != max(1, before.ArtworkCount) || afterWrite.DurationSeconds != before.DurationSeconds {
		t.Fatalf("properties after artwork write = %#v, before=%#v", afterWrite, before)
	}
	deleteRef := testFileRef(t, root, destination, domain.FormatMP3, engine)
	if _, err := writer.WriteArtwork(context.Background(), deleteRef, deleteRef.Revision, 0, nil, false); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := engine.Read(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	if afterDelete.ArtworkCount != afterWrite.ArtworkCount-1 || afterDelete.DurationSeconds != before.DurationSeconds {
		t.Fatalf("properties after artwork delete = %#v", afterDelete)
	}
}

func TestWriterWithGeneratedWAV(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fixture.wav")
	writeSilentWAV(t, path)
	engine := taglibwasm.New()
	writer, err := New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	ref := testFileRef(t, root, path, domain.FormatWAV, engine)
	result, err := writer.Write(context.Background(), ref, ref.Revision, domain.TagPatch{
		Title:   &domain.StringFieldPatch{Op: domain.OperationSet, Value: "WAV Fixture"},
		Artists: &domain.StringsFieldPatch{Op: domain.OperationSet, Value: []string{"Tagger Tests"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected WAV tags to change")
	}
	after, err := engine.Read(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := firstRaw(after.Raw, "TITLE"); got != "WAV Fixture" {
		t.Fatalf("WAV title = %q", got)
	}
	if got := firstRaw(after.Raw, "ARTIST"); got != "Tagger Tests" {
		t.Fatalf("WAV artist = %q", got)
	}
	if after.Properties.Container != "WAV" || after.Properties.Codec != "PCM" {
		t.Fatalf("WAV properties = %#v", after.Properties)
	}
}

func TestWriterExtendedFieldsAcrossCertifiedFormats(t *testing.T) {
	corpus := os.Getenv("TAGGER_TEST_MUSIC_DIR")
	if corpus == "" {
		corpus = "/home/ericwyn/Downloads/TestMusic"
	}
	type fixture struct {
		format domain.TrackFormat
		ext    string
		source string
	}
	fixtures := []fixture{{format: domain.FormatWAV, ext: ".wav"}}
	if _, err := os.Stat(corpus); err == nil {
		for _, ext := range []string{".mp3", ".flac"} {
			fixtures = append(fixtures, fixture{format: domain.TrackFormat(strings.TrimPrefix(ext, ".")), ext: ext, source: findAudio(t, corpus, ext)})
		}
	}

	for _, item := range fixtures {
		t.Run(strings.TrimPrefix(item.ext, "."), func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "extended"+item.ext)
			if item.source != "" {
				copyFixture(t, item.source, destination)
			} else {
				writeSilentWAV(t, destination)
			}
			engine := taglibwasm.New()
			if _, err := engine.Read(context.Background(), destination); err != nil {
				t.Fatal(err)
			}
			writer, err := New(root, engine)
			if err != nil {
				t.Fatal(err)
			}
			ref := testFileRef(t, root, destination, item.format, engine)
			_, err = writer.Write(context.Background(), ref, ref.Revision, domain.TagPatch{
				Comment:              &domain.StringFieldPatch{Op: domain.OperationSet, Value: "Tagger extended comment"},
				Composers:            &domain.StringsFieldPatch{Op: domain.OperationSet, Value: []string{"Composer A", "Composer B"}},
				Conductor:            &domain.StringFieldPatch{Op: domain.OperationSet, Value: "Conductor"},
				Lyricists:            &domain.StringsFieldPatch{Op: domain.OperationSet, Value: []string{"Lyricist"}},
				Copyright:            &domain.StringFieldPatch{Op: domain.OperationSet, Value: "© Tagger"},
				BPM:                  &domain.IntFieldPatch{Op: domain.OperationSet, Value: 128},
				ISRC:                 &domain.StringFieldPatch{Op: domain.OperationSet, Value: "US-TAG-26-00001"},
				MusicBrainzTrackID:   &domain.StringFieldPatch{Op: domain.OperationSet, Value: "track-mbid"},
				MusicBrainzReleaseID: &domain.StringFieldPatch{Op: domain.OperationSet, Value: "release-mbid"},
				MusicBrainzArtistIDs: &domain.StringsFieldPatch{Op: domain.OperationSet, Value: []string{"artist-mbid"}},
				AcoustID:             &domain.StringFieldPatch{Op: domain.OperationSet, Value: "acoustid-id"},
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			after, err := engine.Read(context.Background(), destination)
			if err != nil {
				t.Fatal(err)
			}
			checks := map[string]string{
				"COMMENT": "Tagger extended comment", "CONDUCTOR": "Conductor", "COPYRIGHT": "© Tagger",
				"BPM": "128", "ISRC": "US-TAG-26-00001", "MUSICBRAINZ_TRACKID": "track-mbid",
				"MUSICBRAINZ_ALBUMID": "release-mbid", "ACOUSTID_ID": "acoustid-id",
			}
			for key, want := range checks {
				if got := firstRaw(after.Raw, key); got != want {
					t.Errorf("%s = %q, want %q (raw=%#v)", key, got, want, after.Raw)
				}
			}
			if !slices.Equal(after.Raw["COMPOSER"], []string{"Composer A", "Composer B"}) || !slices.Equal(after.Raw["LYRICIST"], []string{"Lyricist"}) || !slices.Equal(after.Raw["MUSICBRAINZ_ARTISTID"], []string{"artist-mbid"}) {
				t.Fatalf("multi-value extended tags = %#v", after.Raw)
			}
		})
	}
}

func TestWriterEmitsStandardMP3FramesFromV23AndV24Inputs(t *testing.T) {
	for _, version := range []byte{3, 4} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "compat.mp3")
			writeSyntheticMP3(t, path, version)
			beforeBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			beforeAudio := sha256.Sum256(mpegPayload(t, beforeBytes))

			engine := taglibwasm.New()
			before, err := engine.Read(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if firstRaw(before.Raw, "X-COMPAT-TEST") != "keep" {
				t.Fatalf("custom frame not mapped: %#v", before.Raw)
			}
			writer, err := New(root, engine)
			if err != nil {
				t.Fatal(err)
			}
			ref := testFileRef(t, root, path, domain.FormatMP3, engine)
			_, err = writer.Write(context.Background(), ref, ref.Revision, domain.TagPatch{
				Title:        &domain.StringFieldPatch{Op: domain.OperationSet, Value: "brave heart"},
				Artists:      &domain.StringsFieldPatch{Op: domain.OperationSet, Value: []string{"宮崎歩"}},
				Album:        &domain.StringFieldPatch{Op: domain.OperationSet, Value: "デジモンエンディングベスト"},
				AlbumArtists: &domain.StringsFieldPatch{Op: domain.OperationSet, Value: []string{"宮崎歩"}},
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			after, err := engine.Read(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if firstRaw(after.Raw, "TITLE") != "brave heart" || firstRaw(after.Raw, "ARTIST") != "宮崎歩" || firstRaw(after.Raw, "ALBUM") != "デジモンエンディングベスト" || firstRaw(after.Raw, "ALBUMARTIST") != "宮崎歩" {
				t.Fatalf("written properties = %#v", after.Raw)
			}
			if firstRaw(after.Raw, "X-COMPAT-TEST") != "keep" {
				t.Fatalf("custom property was lost: %#v", after.Raw)
			}
			afterBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			frames := id3FrameIDs(t, afterBytes)
			for _, frame := range []string{"TIT2", "TPE1", "TALB", "TPE2", "TXXX"} {
				if !slices.Contains(frames, frame) {
					t.Fatalf("frame %s missing from %v", frame, frames)
				}
			}
			afterAudio := sha256.Sum256(mpegPayload(t, afterBytes))
			if beforeAudio != afterAudio {
				t.Fatal("MPEG audio payload changed during tag write")
			}
		})
	}
}

func writeSyntheticMP3(t *testing.T, path string, version byte) {
	t.Helper()
	description := []byte("X-COMPAT-TEST")
	framePayload := append([]byte{0}, description...)
	framePayload = append(framePayload, 0)
	framePayload = append(framePayload, []byte("keep")...)
	frame := bytes.NewBuffer(nil)
	frame.WriteString("TXXX")
	if version == 4 {
		size := synchsafe(len(framePayload))
		frame.Write(size[:])
	} else {
		_ = binary.Write(frame, binary.BigEndian, uint32(len(framePayload)))
	}
	frame.Write([]byte{0, 0})
	frame.Write(framePayload)

	buffer := bytes.NewBuffer(nil)
	buffer.Write([]byte{'I', 'D', '3', version, 0, 0})
	tagSize := synchsafe(frame.Len())
	buffer.Write(tagSize[:])
	buffer.Write(frame.Bytes())
	const frameLength = 417
	for range 100 {
		mpegFrame := make([]byte, frameLength)
		copy(mpegFrame, []byte{0xff, 0xfb, 0x90, 0x64})
		buffer.Write(mpegFrame)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func synchsafe(value int) [4]byte {
	return [4]byte{byte(value >> 21 & 0x7f), byte(value >> 14 & 0x7f), byte(value >> 7 & 0x7f), byte(value & 0x7f)}
}

func synchsafeValue(value []byte) int {
	if len(value) < 4 {
		return 0
	}
	return int(value[0]&0x7f)<<21 | int(value[1]&0x7f)<<14 | int(value[2]&0x7f)<<7 | int(value[3]&0x7f)
}

func id3FrameIDs(t *testing.T, data []byte) []string {
	t.Helper()
	if len(data) < 10 || string(data[:3]) != "ID3" {
		t.Fatal("missing ID3v2 header")
	}
	version := data[3]
	end := 10 + synchsafeValue(data[6:10])
	if end > len(data) {
		t.Fatal("invalid ID3v2 size")
	}
	frames := make([]string, 0)
	for offset := 10; offset+10 <= end; {
		id := string(data[offset : offset+4])
		if id == "\x00\x00\x00\x00" {
			break
		}
		size := int(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
		if version == 4 {
			size = synchsafeValue(data[offset+4 : offset+8])
		}
		if size < 0 || offset+10+size > end {
			t.Fatalf("invalid frame %q size %d", id, size)
		}
		frames = append(frames, id)
		offset += 10 + size
	}
	return frames
}

func mpegPayload(t *testing.T, data []byte) []byte {
	t.Helper()
	payload := data
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		offset := 10 + synchsafeValue(data[6:10])
		if data[5]&0x10 != 0 {
			offset += 10
		}
		if offset <= len(data) {
			payload = data[offset:]
		}
	}
	if len(payload) >= 128 && string(payload[len(payload)-128:len(payload)-125]) == "TAG" {
		payload = payload[:len(payload)-128]
	}
	return payload
}

func testFileRef(t *testing.T, root, path string, format domain.TrackFormat, engine tags.Engine) library.FileRef {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Read(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return library.FileRef{
		ID:           "trk-test",
		RelativePath: filepath.ToSlash(relative),
		AbsolutePath: path,
		Revision:     scanner.FileRevision(filepath.ToSlash(relative), info, snapshot.Raw),
		Format:       format,
	}
}

func cloneRaw(raw map[string][]string) map[string][]string {
	result := make(map[string][]string, len(raw))
	for key, values := range raw {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func findAudio(t *testing.T, root, extension string) string {
	t.Helper()
	var found string
	errStop := errors.New("found")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), extension) {
			found = path
			return errStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		t.Fatal(err)
	}
	if found == "" {
		t.Skipf("no %s fixture in %s", extension, root)
	}
	return found
}

func copyFixture(t *testing.T, source, destination string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func firstRaw(raw map[string][]string, key string) string {
	for rawKey, values := range raw {
		if strings.EqualFold(rawKey, key) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func rawForKey(raw map[string][]string, key string) []string {
	for rawKey, values := range raw {
		if strings.EqualFold(rawKey, key) {
			return values
		}
	}
	return nil
}

func writeSilentWAV(t *testing.T, path string) {
	t.Helper()
	const (
		sampleRate    = 8000
		channels      = 1
		bitsPerSample = 16
		samples       = 800
	)
	dataSize := samples * channels * bitsPerSample / 8
	buffer := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buffer.WriteString("RIFF")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(36+dataSize))
	buffer.WriteString("WAVE")
	buffer.WriteString("fmt ")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(16))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(1))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(channels))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(sampleRate*channels*bitsPerSample/8))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(channels*bitsPerSample/8))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(bitsPerSample))
	buffer.WriteString("data")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(dataSize))
	buffer.Write(make([]byte, dataSize))
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
