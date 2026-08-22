package filewrite

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

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
