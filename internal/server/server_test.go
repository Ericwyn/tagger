package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/store"
	"github.com/ericwyn/tagger/internal/tags"
)

type serverEngine struct{}

type serverProvider struct{}

func (serverProvider) Descriptor() providers.Descriptor {
	return providers.Descriptor{ID: "test-provider", Name: "Test Provider", Enabled: true, Health: providers.HealthReady}
}

func (serverProvider) Search(_ context.Context, query providers.Query, _ int) ([]providers.Candidate, error) {
	return []providers.Candidate{{
		ProviderID: "test-provider", ExternalID: "external-1", Title: query.Title,
		Artists: query.Artists, Album: query.Album, DurationSeconds: query.DurationSeconds,
	}}, nil
}

func (serverEngine) Read(_ context.Context, path string) (tags.Snapshot, error) {
	name := filepath.Base(path)
	return tags.Snapshot{
		Raw:             map[string][]string{"TITLE": {name[:len(name)-len(filepath.Ext(name))]}, "ARTIST": {"歌手"}},
		DurationSeconds: 180,
		ArtworkCount:    1,
	}, nil
}

func (serverEngine) Write(context.Context, string, map[string][]string) error { return nil }

func (serverEngine) Version() string { return "test-engine" }

func TestLibraryAPIAndFrontendFallback(t *testing.T) {
	s := newTestServer(t)

	health := ut.PerformRequest(s.h.Engine, "GET", "/healthz", nil)
	if health.Code != 200 || !containsJSON(health.Body.Bytes(), `"status":"ok"`) {
		t.Fatalf("health = %d %s", health.Code, health.Body.String())
	}

	libraries := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/libraries", nil)
	if libraries.Code != 200 || !containsJSON(libraries.Body.Bytes(), `"trackCount":2`) {
		t.Fatalf("libraries = %d %s", libraries.Code, libraries.Body.String())
	}

	tracks := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks?format=flac", nil)
	if tracks.Code != 200 || !containsJSON(tracks.Body.Bytes(), `"total":1`) || !containsJSON(tracks.Body.Bytes(), `"title":"Beta"`) {
		t.Fatalf("tracks = %d %s", tracks.Code, tracks.Body.String())
	}

	var listEnvelope struct {
		Data struct {
			Tracks []struct {
				ID string `json:"id"`
			} `json:"tracks"`
		} `json:"data"`
	}
	allTracks := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks", nil)
	if !containsJSON(allTracks.Body.Bytes(), `"genres":[]`) {
		t.Fatalf("empty multi-value fields must be arrays: %s", allTracks.Body.String())
	}
	if err := json.Unmarshal(allTracks.Body.Bytes(), &listEnvelope); err != nil || len(listEnvelope.Data.Tracks) != 2 {
		t.Fatalf("decode tracks: %v body=%s", err, allTracks.Body.String())
	}
	detail := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+listEnvelope.Data.Tracks[0].ID, nil)
	if detail.Code != 200 || detail.Result().Header.Get("ETag") == "" {
		t.Fatalf("detail = %d etag=%q body=%s", detail.Code, detail.Result().Header.Get("ETag"), detail.Body.String())
	}

	missing := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/missing", nil)
	if missing.Code != 404 || !containsJSON(missing.Body.Bytes(), `"code":"track_not_found"`) {
		t.Fatalf("missing = %d %s", missing.Code, missing.Body.String())
	}

	index := ut.PerformRequest(s.h.Engine, "GET", "/", nil)
	if index.Code != 200 || index.Result().Header.Get("Cache-Control") != "no-cache" || index.Body.String() != "<main>Tagger</main>" {
		t.Fatalf("index = %d cache=%q body=%s", index.Code, index.Result().Header.Get("Cache-Control"), index.Body.String())
	}
	asset := ut.PerformRequest(s.h.Engine, "GET", "/assets/app.js", nil)
	if asset.Code != 200 || asset.Result().Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset = %d cache=%q", asset.Code, asset.Result().Header.Get("Cache-Control"))
	}
	spa := ut.PerformRequest(s.h.Engine, "GET", "/library/album", nil, ut.Header{Key: "Accept", Value: "text/html"})
	if spa.Code != 200 || spa.Body.String() != "<main>Tagger</main>" {
		t.Fatalf("spa = %d %s", spa.Code, spa.Body.String())
	}
	unknownAPI := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/unknown", nil, ut.Header{Key: "Accept", Value: "text/html"})
	if unknownAPI.Code != 404 || !containsJSON(unknownAPI.Body.Bytes(), `"code":"not_found"`) {
		t.Fatalf("unknown api = %d %s", unknownAPI.Code, unknownAPI.Body.String())
	}
}

func TestRescanRejectsUnknownLibrary(t *testing.T) {
	s := newTestServer(t)
	response := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/libraries/unknown/scans", nil)
	if response.Code != 404 || !containsJSON(response.Body.Bytes(), `"code":"library_not_found"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestTagWriteDryRunAndRevisionConflict(t *testing.T) {
	s := newTestServer(t)
	allTracks := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks", nil)
	var listEnvelope struct {
		Data struct {
			Tracks []struct {
				ID       string `json:"id"`
				Revision string `json:"revision"`
			} `json:"tracks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(allTracks.Body.Bytes(), &listEnvelope); err != nil || len(listEnvelope.Data.Tracks) == 0 {
		t.Fatalf("decode tracks: %v body=%s", err, allTracks.Body.String())
	}
	track := listEnvelope.Data.Tracks[0]
	body := []byte(`{"baseRevision":"` + track.Revision + `","patch":{"title":{"op":"set","value":"Changed"}},"dryRun":true}`)
	preview := ut.PerformRequest(s.h.Engine, "PATCH", "/api/v1/tracks/"+track.ID+"/tags", &ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "content-type", Value: "application/json"}, ut.Header{Key: "If-Match", Value: `"` + track.Revision + `"`})
	if preview.Code != 200 || !containsJSON(preview.Body.Bytes(), `"changed":true`) || !containsJSON(preview.Body.Bytes(), `"field":"title"`) {
		t.Fatalf("preview = %d %s", preview.Code, preview.Body.String())
	}

	conflictBody := []byte(`{"baseRevision":"stale","patch":{"title":{"op":"set","value":"Changed"}},"dryRun":true}`)
	conflict := ut.PerformRequest(s.h.Engine, "PATCH", "/api/v1/tracks/"+track.ID+"/tags", &ut.Body{Body: bytes.NewReader(conflictBody), Len: len(conflictBody)},
		ut.Header{Key: "content-type", Value: "application/json"}, ut.Header{Key: "If-Match", Value: `"stale"`})
	if conflict.Code != 409 || !containsJSON(conflict.Body.Bytes(), `"code":"revision_conflict"`) {
		t.Fatalf("conflict = %d %s", conflict.Code, conflict.Body.String())
	}
}

func TestProviderListAndTrackMatchSearch(t *testing.T) {
	s := newTestServer(t)
	providerList := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/providers", nil)
	if providerList.Code != 200 || !containsJSON(providerList.Body.Bytes(), `"id":"test-provider"`) {
		t.Fatalf("providers = %d %s", providerList.Code, providerList.Body.String())
	}

	allTracks := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks", nil)
	var listEnvelope struct {
		Data struct {
			Tracks []struct {
				ID string `json:"id"`
			} `json:"tracks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(allTracks.Body.Bytes(), &listEnvelope); err != nil || len(listEnvelope.Data.Tracks) == 0 {
		t.Fatalf("decode tracks: %v", err)
	}
	body := []byte(`{"fileId":"` + listEnvelope.Data.Tracks[0].ID + `","providerIds":["test-provider"],"limitPerProvider":3}`)
	response := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/matches/tracks/search", &ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "content-type", Value: "application/json"})
	if response.Code != 200 || !containsJSON(response.Body.Bytes(), `"providerId":"test-provider"`) || !containsJSON(response.Body.Bytes(), `"value":"Alpha"`) {
		t.Fatalf("match = %d %s", response.Code, response.Body.String())
	}
}

func TestRevisionHistoryAPI(t *testing.T) {
	s := newTestServer(t)
	track := s.library.ListTracks(library.TrackFilter{})[0]
	created, err := s.store.CreateRevision(context.Background(), domain.Revision{
		ID: "revlog-test", LibraryID: s.library.Library().ID, TrackID: track.ID,
		TrackTitle: "Changed song", FileName: "changed.flac", Action: "修改标签", Source: "手工编辑",
		BaseRevision: "before", ResultRevision: "after", CoverTone: domain.CoverMoss,
		Diff:       []domain.RevisionDiff{{Field: "title", Operation: domain.OperationSet, Before: "Old", After: "Changed song"}},
		BeforeTags: map[string][]string{"TITLE": {"Old"}}, AfterTags: map[string][]string{"TITLE": {"Changed song"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	list := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/revisions", nil)
	if list.Code != 200 || !containsJSON(list.Body.Bytes(), `"id":"revlog-test"`) ||
		!containsJSON(list.Body.Bytes(), `"before":"Old"`) || !containsJSON(list.Body.Bytes(), `"currentRevision":"`+track.Revision+`"`) {
		t.Fatalf("revision list = %d %s", list.Code, list.Body.String())
	}
	detail := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/revisions/"+created.ID, nil)
	if detail.Code != 200 || !containsJSON(detail.Body.Bytes(), `"resultRevision":"after"`) {
		t.Fatalf("revision detail = %d %s", detail.Code, detail.Body.String())
	}
	missing := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/revisions/missing", nil)
	if missing.Code != 404 || !containsJSON(missing.Body.Bytes(), `"code":"revision_not_found"`) {
		t.Fatalf("missing revision = %d %s", missing.Code, missing.Body.String())
	}
	invalidBody := []byte(`{"baseRevision":"` + track.Revision + `","target":"unknown"}`)
	invalid := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/revisions/"+created.ID+"/restore-preview",
		&ut.Body{Body: bytes.NewReader(invalidBody), Len: len(invalidBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + track.Revision + `"`})
	if invalid.Code != 400 || !containsJSON(invalid.Body.Bytes(), `"code":"invalid_request"`) {
		t.Fatalf("invalid restore target = %d %s", invalid.Code, invalid.Body.String())
	}
}

func TestArtworkAPIRejectsInvalidImagesBeforeWriting(t *testing.T) {
	s := newTestServer(t)
	track := s.library.ListTracks(library.TrackFilter{})[0]
	body := []byte("not an image")
	response := ut.PerformRequest(s.h.Engine, "PUT", "/api/v1/tracks/"+track.ID+"/artwork/0",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "content-type", Value: "image/jpeg"},
		ut.Header{Key: "If-Match", Value: `"` + track.Revision + `"`})
	if response.Code != 422 || !containsJSON(response.Body.Bytes(), `"code":"invalid_artwork"`) {
		t.Fatalf("invalid artwork = %d %s", response.Code, response.Body.String())
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"Alpha.mp3", "Beta.flac"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	musicScanner, err := scanner.New(serverEngine{}, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, err := library.New(context.Background(), musicScanner)
	dataStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	service, err = library.New(context.Background(), musicScanner, dataStore)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := filewrite.New(root, serverEngine{})
	if err != nil {
		t.Fatal(err)
	}
	frontend := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<main>Tagger</main>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('tagger')")},
	}
	registry := providers.NewRegistry(serverProvider{})
	return New("127.0.0.1:0", service, writer, registry, dataStore, fs.FS(frontend), "test-version", serverEngine{}.Version())
}

func containsJSON(body []byte, fragment string) bool {
	var compact any
	if json.Unmarshal(body, &compact) != nil {
		return false
	}
	encoded, _ := json.Marshal(compact)
	return stringContains(string(encoded), fragment)
}

func stringContains(value, fragment string) bool {
	if fragment == "" {
		return true
	}
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
