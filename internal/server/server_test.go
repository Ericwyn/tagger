package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/tags"
)

type serverEngine struct{}

func (serverEngine) Read(_ context.Context, path string) (tags.Snapshot, error) {
	name := filepath.Base(path)
	return tags.Snapshot{
		Raw:             map[string][]string{"TITLE": {name[:len(name)-len(filepath.Ext(name))]}, "ARTIST": {"歌手"}},
		DurationSeconds: 180,
		ArtworkCount:    1,
	}, nil
}

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
	if err != nil {
		t.Fatal(err)
	}
	frontend := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<main>Tagger</main>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('tagger')")},
	}
	return New("127.0.0.1:0", service, fs.FS(frontend), "test-version", serverEngine{}.Version())
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
