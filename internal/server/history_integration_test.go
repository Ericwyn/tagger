package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/store"
	"github.com/ericwyn/tagger/internal/tags/taglibwasm"
)

func TestSuccessfulRealTagWriteCreatesPersistentRevision(t *testing.T) {
	corpus := os.Getenv("TAGGER_TEST_MUSIC_DIR")
	if corpus == "" {
		corpus = "/home/ericwyn/Downloads/TestMusic"
	}
	source := findIntegrationAudio(t, corpus, ".mp3")
	root := t.TempDir()
	destination := filepath.Join(root, filepath.Base(source))
	copyIntegrationFile(t, source, destination)

	engine := taglibwasm.New()
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	dataPath := filepath.Join(t.TempDir(), "tagger.db")
	dataStore, err := store.Open(context.Background(), dataPath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := library.New(context.Background(), musicScanner, dataStore)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := filewrite.New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	frontend := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("Tagger")}}
	s := New("127.0.0.1:0", service, writer, providers.NewRegistry(serverProvider{}), dataStore, fs.FS(frontend), "test", engine.Version())

	tracks := service.ListTracks(library.TrackFilter{})
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(tracks))
	}
	track := tracks[0]
	newTitle := track.Title + " [API History Test]"
	body, err := json.Marshal(map[string]any{
		"baseRevision": track.Revision,
		"patch":        map[string]any{"title": map[string]any{"op": "set", "value": newTitle}},
		"dryRun":       false,
		"provenance":   map[string]string{"providerId": "test-provider"},
	})
	if err != nil {
		t.Fatal(err)
	}
	write := ut.PerformRequest(s.h.Engine, "PATCH", "/api/v1/tracks/"+track.ID+"/tags",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + track.Revision + `"`})
	if write.Code != 200 || !containsJSON(write.Body.Bytes(), `"title":"`+newTitle+`"`) {
		t.Fatalf("write = %d %s", write.Code, write.Body.String())
	}

	history := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/revisions", nil)
	if history.Code != 200 || !containsJSON(history.Body.Bytes(), `"field":"title"`) ||
		!containsJSON(history.Body.Bytes(), `"source":"Test Provider"`) ||
		!containsJSON(history.Body.Bytes(), `"before":"`+track.Title+`"`) ||
		!containsJSON(history.Body.Bytes(), `"after":"`+newTitle+`"`) {
		t.Fatalf("history = %d %s", history.Code, history.Body.String())
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(context.Background(), dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	revisions, err := reopened.ListRevisions(context.Background(), 10)
	if err != nil || len(revisions) != 1 || revisions[0].Diff[0].After != newTitle {
		t.Fatalf("reopened history = %#v err=%v", revisions, err)
	}
}

func findIntegrationAudio(t *testing.T, root, extension string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), extension) {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil || found == "" {
		t.Skipf("integration fixture %s unavailable under %s: %v", extension, root, err)
	}
	return found
}

func copyIntegrationFile(t *testing.T, source, destination string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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
