package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudwego/hertz/pkg/common/ut"
	artworkpkg "github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/domain"
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
	revisions, err := dataStore.ListRevisions(context.Background(), 10)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("stored revisions = %#v err=%v", revisions, err)
	}
	updatedTrack, err := service.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	restoreBody, err := json.Marshal(map[string]any{"baseRevision": updatedTrack.Revision, "target": "before"})
	if err != nil {
		t.Fatal(err)
	}
	preview := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/revisions/"+revisions[0].ID+"/restore-preview",
		&ut.Body{Body: bytes.NewReader(restoreBody), Len: len(restoreBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + updatedTrack.Revision + `"`})
	if preview.Code != 200 || !containsJSON(preview.Body.Bytes(), `"before":"`+newTitle+`"`) ||
		!containsJSON(preview.Body.Bytes(), `"after":"`+track.Title+`"`) {
		t.Fatalf("restore preview = %d %s", preview.Code, preview.Body.String())
	}
	restore := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/revisions/"+revisions[0].ID+"/restore",
		&ut.Body{Body: bytes.NewReader(restoreBody), Len: len(restoreBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + updatedTrack.Revision + `"`})
	if restore.Code != 200 || !containsJSON(restore.Body.Bytes(), `"title":"`+track.Title+`"`) ||
		!containsJSON(restore.Body.Bytes(), `"changed":true`) {
		t.Fatalf("restore = %d %s", restore.Code, restore.Body.String())
	}
	staleRestore := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/revisions/"+revisions[0].ID+"/restore",
		&ut.Body{Body: bytes.NewReader(restoreBody), Len: len(restoreBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + updatedTrack.Revision + `"`})
	if staleRestore.Code != 409 || !containsJSON(staleRestore.Body.Bytes(), `"code":"revision_conflict"`) {
		t.Fatalf("stale restore = %d %s", staleRestore.Code, staleRestore.Body.String())
	}
	restoredTrack, err := service.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	var providerCover bytes.Buffer
	if err := png.Encode(&providerCover, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	providerAsset, err := artworkpkg.Validate(providerCover.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	s.downloadArtwork = func(_ context.Context, reference providers.ArtworkReference) (artworkpkg.Asset, error) {
		if reference.ProviderID != "test-provider" {
			t.Fatalf("artwork provider = %q", reference.ProviderID)
		}
		return providerAsset, nil
	}
	searchResult, err := s.providers.Search(context.Background(), providers.Query{
		Title: restoredTrack.Title, Artists: restoredTrack.Artists, Album: restoredTrack.Album,
	}, []string{"test-provider"}, 1)
	if err != nil || len(searchResult.Candidates) != 1 {
		t.Fatalf("provider candidates = %#v err=%v", searchResult, err)
	}
	providerBody, err := json.Marshal(map[string]any{
		"candidateId": searchResult.Candidates[0].ID, "baseRevision": restoredTrack.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	providerWrite := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/matches/tracks/"+track.ID+"/artwork",
		&ut.Body{Body: bytes.NewReader(providerBody), Len: len(providerBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + restoredTrack.Revision + `"`})
	if providerWrite.Code != 200 || !containsJSON(providerWrite.Body.Bytes(), `"width":2`) {
		t.Fatalf("provider artwork = %d %s", providerWrite.Code, providerWrite.Body.String())
	}
	providerTrack, err := service.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	var cover bytes.Buffer
	if err := png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	coverBytes := cover.Bytes()
	coverWrite := ut.PerformRequest(s.h.Engine, "PUT", "/api/v1/tracks/"+track.ID+"/artwork/0",
		&ut.Body{Body: bytes.NewReader(coverBytes), Len: len(coverBytes)},
		ut.Header{Key: "content-type", Value: "image/png"},
		ut.Header{Key: "If-Match", Value: `"` + providerTrack.Revision + `"`})
	if coverWrite.Code != 200 || !containsJSON(coverWrite.Body.Bytes(), `"field":"artwork"`) ||
		!containsJSON(coverWrite.Body.Bytes(), `"width":4`) {
		t.Fatalf("cover write = %d %s", coverWrite.Code, coverWrite.Body.String())
	}
	coverRead := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/artwork/0", nil)
	if coverRead.Code != 200 || coverRead.Result().Header.Get("Content-Type") != "image/png" ||
		coverRead.Result().Header.Get("X-Tagger-Artwork-Size") != "4x3" || !bytes.Equal(coverRead.Body.Bytes(), coverBytes) {
		t.Fatalf("cover read = %d type=%q size=%q bytes=%d", coverRead.Code,
			coverRead.Result().Header.Get("Content-Type"), coverRead.Result().Header.Get("X-Tagger-Artwork-Size"), coverRead.Body.Len())
	}
	coveredTrack, err := service.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	coverHistory, err := dataStore.ListRevisions(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	var coverRevision domain.Revision
	for _, candidateRevision := range coverHistory {
		if candidateRevision.Action == "替换封面" {
			coverRevision = candidateRevision
			break
		}
	}
	if coverRevision.ID == "" {
		t.Fatal("cover revision was not persisted")
	}
	coverRestoreBody, err := json.Marshal(map[string]any{"baseRevision": coveredTrack.Revision, "target": "before"})
	if err != nil {
		t.Fatal(err)
	}
	coverRestorePreview := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/revisions/"+coverRevision.ID+"/restore-preview",
		&ut.Body{Body: bytes.NewReader(coverRestoreBody), Len: len(coverRestoreBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + coveredTrack.Revision + `"`})
	if coverRestorePreview.Code != 200 || !containsJSON(coverRestorePreview.Body.Bytes(), `"field":"artwork"`) ||
		!containsJSON(coverRestorePreview.Body.Bytes(), `"width":4`) || !containsJSON(coverRestorePreview.Body.Bytes(), `"width":2`) {
		t.Fatalf("cover restore preview = %d %s", coverRestorePreview.Code, coverRestorePreview.Body.String())
	}
	coverRestore := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/revisions/"+coverRevision.ID+"/restore",
		&ut.Body{Body: bytes.NewReader(coverRestoreBody), Len: len(coverRestoreBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + coveredTrack.Revision + `"`})
	if coverRestore.Code != 200 || !containsJSON(coverRestore.Body.Bytes(), `"width":2`) {
		t.Fatalf("cover restore = %d %s", coverRestore.Code, coverRestore.Body.String())
	}
	restoredCoverTrack, err := service.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	restoredCover := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/artwork/0", nil)
	if restoredCover.Code != 200 || restoredCover.Result().Header.Get("X-Tagger-Artwork-Size") != "2x2" {
		t.Fatalf("restored cover = %d size=%q", restoredCover.Code, restoredCover.Result().Header.Get("X-Tagger-Artwork-Size"))
	}
	coverDelete := ut.PerformRequest(s.h.Engine, "DELETE", "/api/v1/tracks/"+track.ID+"/artwork/0", nil,
		ut.Header{Key: "If-Match", Value: `"` + restoredCoverTrack.Revision + `"`})
	if coverDelete.Code != 200 || !containsJSON(coverDelete.Body.Bytes(), `"artworkCount":0`) {
		t.Fatalf("cover delete = %d %s", coverDelete.Code, coverDelete.Body.String())
	}
	missingCover := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/artwork/0", nil)
	if missingCover.Code != 404 || !containsJSON(missingCover.Body.Bytes(), `"code":"artwork_not_found"`) {
		t.Fatalf("missing cover = %d %s", missingCover.Code, missingCover.Body.String())
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(context.Background(), dataPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	revisions, err = reopened.ListRevisions(context.Background(), 10)
	if err != nil || len(revisions) != 6 || revisions[0].Action != "删除封面" || revisions[1].Action != "恢复到修订前" ||
		revisions[2].Action != "替换封面" || revisions[3].Action != "采用数据源封面" || revisions[3].Source != "Test Provider" ||
		revisions[4].Action != "恢复到修订前" || revisions[4].Source != "历史修订 "+revisions[5].ID ||
		revisions[4].Diff[0].After != track.Title {
		t.Fatalf("reopened history = %#v err=%v", revisions, err)
	}
	loaded, found, err := reopened.LoadScan(context.Background(), root)
	if err != nil || !found || len(loaded.Tracks) != 1 || loaded.Tracks[0].Title != track.Title || loaded.Tracks[0].ArtworkCount != 0 {
		t.Fatalf("reopened scan = %#v found=%v err=%v", loaded, found, err)
	}
}

func TestSuccessfulRealSidecarWriteCreatesPersistentRevision(t *testing.T) {
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
	dataStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
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

	track := service.ListTracks(library.TrackFilter{})[0]
	content := "[00:03.00] real sidecar history\n"
	body, err := json.Marshal(map[string]any{"baseRevision": track.Revision, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	write := ut.PerformRequest(s.h.Engine, "PUT", "/api/v1/tracks/"+track.ID+"/lyrics-sidecar",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + track.Revision + `"`})
	if write.Code != 200 || !containsJSON(write.Body.Bytes(), `"currentSidecarRevision":"sidecar-`) {
		t.Fatalf("sidecar write = %d %s", write.Code, write.Body.String())
	}
	revisions, err := dataStore.ListRevisions(context.Background(), 10)
	if err != nil || len(revisions) != 1 || revisions[0].Action != "写入歌词 sidecar" || revisions[0].AfterSidecar == nil || revisions[0].AfterSidecar.Content != content {
		t.Fatalf("sidecar history = %#v err=%v", revisions, err)
	}
	updated, err := service.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	restoreBody, err := json.Marshal(map[string]any{"baseRevision": updated.Revision, "target": "before"})
	if err != nil {
		t.Fatal(err)
	}
	restore := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/revisions/"+revisions[0].ID+"/restore",
		&ut.Body{Body: bytes.NewReader(restoreBody), Len: len(restoreBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + updated.Revision + `"`})
	if restore.Code != 200 {
		t.Fatalf("sidecar restore = %d %s", restore.Code, restore.Body.String())
	}
	read := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/lyrics-sidecar", nil)
	if read.Code != 200 || containsJSON(read.Body.Bytes(), `"exists":true`) {
		t.Fatalf("restored real sidecar = %d %s", read.Code, read.Body.String())
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
