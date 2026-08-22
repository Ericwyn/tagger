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
	"time"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/jobs"
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
		ArtworkURL: "https://images.example.test/cover.jpg",
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

func TestAudioAPIProvidesRangeStreamAndETag(t *testing.T) {
	s := newTestServer(t)
	track := s.library.ListTracks(library.TrackFilter{})[0]
	ref, err := s.library.FileRef(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("0123456789")
	if err := os.WriteFile(ref.AbsolutePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	ranged := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/audio", nil,
		ut.Header{Key: "Range", Value: "bytes=2-5"})
	if ranged.Code != 206 || ranged.Body.String() != "2345" ||
		ranged.Result().Header.Get("Content-Range") != "bytes 2-5/10" ||
		ranged.Result().Header.Get("Accept-Ranges") != "bytes" ||
		ranged.Result().Header.Get("Content-Type") != "audio/mpeg" ||
		ranged.Result().Header.Get("ETag") != `"`+track.Revision+`"` {
		t.Fatalf("range audio = %d contentRange=%q contentType=%q body=%q", ranged.Code,
			ranged.Result().Header.Get("Content-Range"), ranged.Result().Header.Get("Content-Type"), ranged.Body.String())
	}

	notModified := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/audio", nil,
		ut.Header{Key: "If-None-Match", Value: `"` + track.Revision + `"`})
	if notModified.Code != 304 || notModified.Body.Len() != 0 {
		t.Fatalf("audio etag = %d body=%q", notModified.Code, notModified.Body.String())
	}

	invalid := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/audio", nil,
		ut.Header{Key: "Range", Value: "bytes=99-100"})
	if invalid.Code != 416 || invalid.Result().Header.Get("Content-Range") != "bytes */10" {
		t.Fatalf("invalid audio range = %d contentRange=%q", invalid.Code, invalid.Result().Header.Get("Content-Range"))
	}
}

func TestRawTagsAPIReadsLosslessPropertyMap(t *testing.T) {
	s := newTestServer(t)
	track := s.library.ListTracks(library.TrackFilter{})[0]
	response := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/raw-tags", nil)
	if response.Code != 200 || response.Result().Header.Get("ETag") != `"`+track.Revision+`"` ||
		!containsJSON(response.Body.Bytes(), `"trackId":"`+track.ID+`"`) ||
		!containsJSON(response.Body.Bytes(), `"TITLE":["`+track.Title+`"]`) ||
		!containsJSON(response.Body.Bytes(), `"ARTIST":["歌手"]`) {
		t.Fatalf("raw tags = %d etag=%q body=%s", response.Code, response.Result().Header.Get("ETag"), response.Body.String())
	}
}

func TestLyricsSidecarAPIWritesReadsAndGuardsRevision(t *testing.T) {
	s := newTestServer(t)
	track := s.library.ListTracks(library.TrackFilter{})[0]
	content := "[00:01.00]歌词测试\n"
	putBody, err := json.Marshal(map[string]any{
		"baseRevision": track.Revision, "baseSidecarRevision": "", "content": content,
	})
	if err != nil {
		t.Fatal(err)
	}
	put := ut.PerformRequest(s.h.Engine, "PUT", "/api/v1/tracks/"+track.ID+"/lyrics-sidecar",
		&ut.Body{Body: bytes.NewReader(putBody), Len: len(putBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + track.Revision + `"`})
	if put.Code != 200 || !containsJSON(put.Body.Bytes(), `"currentSidecarRevision":"sidecar-`) {
		t.Fatalf("sidecar put = %d %s", put.Code, put.Body.String())
	}
	revisions, err := s.store.ListRevisions(context.Background(), 10)
	if err != nil || len(revisions) != 1 || revisions[0].Action != "写入歌词 sidecar" || revisions[0].BeforeSidecar != nil || revisions[0].AfterSidecar == nil || revisions[0].AfterSidecar.Content != content {
		t.Fatalf("sidecar revisions = %#v err=%v", revisions, err)
	}
	updated, err := s.library.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Lyrics != content || updated.LyricsSidecar == nil || updated.LyricsSidecar.Revision == "" {
		t.Fatalf("updated sidecar track = %#v", updated)
	}
	read := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/tracks/"+track.ID+"/lyrics-sidecar", nil)
	if read.Code != 200 || !containsJSON(read.Body.Bytes(), `"content":"[00:01.00]歌词测试\n"`) {
		t.Fatalf("sidecar get = %d %s", read.Code, read.Body.String())
	}
	staleBody, err := json.Marshal(map[string]any{"baseRevision": updated.Revision, "baseSidecarRevision": ""})
	if err != nil {
		t.Fatal(err)
	}
	stale := ut.PerformRequest(s.h.Engine, "DELETE", "/api/v1/tracks/"+track.ID+"/lyrics-sidecar",
		&ut.Body{Body: bytes.NewReader(staleBody), Len: len(staleBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + updated.Revision + `"`})
	if stale.Code != 409 || !containsJSON(stale.Body.Bytes(), `"code":"sidecar_revision_conflict"`) {
		t.Fatalf("stale sidecar delete = %d %s", stale.Code, stale.Body.String())
	}
	deleteBody, err := json.Marshal(map[string]any{"baseRevision": updated.Revision, "baseSidecarRevision": updated.LyricsSidecar.Revision})
	if err != nil {
		t.Fatal(err)
	}
	deleted := ut.PerformRequest(s.h.Engine, "DELETE", "/api/v1/tracks/"+track.ID+"/lyrics-sidecar",
		&ut.Body{Body: bytes.NewReader(deleteBody), Len: len(deleteBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + updated.Revision + `"`})
	if deleted.Code != 200 || !containsJSON(deleted.Body.Bytes(), `"changed":true`) {
		t.Fatalf("sidecar delete = %d %s", deleted.Code, deleted.Body.String())
	}
}

func TestLyricsSidecarRevisionHistoryRestoresPreviousFile(t *testing.T) {
	s := newTestServer(t)
	track := s.library.ListTracks(library.TrackFilter{})[0]
	content := "[00:02.00]可恢复歌词\n"
	body, err := json.Marshal(map[string]any{"baseRevision": track.Revision, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	put := ut.PerformRequest(s.h.Engine, "PUT", "/api/v1/tracks/"+track.ID+"/lyrics-sidecar",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + track.Revision + `"`})
	if put.Code != 200 {
		t.Fatalf("sidecar put = %d %s", put.Code, put.Body.String())
	}
	revisions, err := s.store.ListRevisions(context.Background(), 10)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("revisions = %#v err=%v", revisions, err)
	}
	updated, err := s.library.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	restoreBody, err := json.Marshal(map[string]any{"baseRevision": updated.Revision, "target": "before"})
	if err != nil {
		t.Fatal(err)
	}
	preview := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/revisions/"+revisions[0].ID+"/restore-preview",
		&ut.Body{Body: bytes.NewReader(restoreBody), Len: len(restoreBody)},
		ut.Header{Key: "content-type", Value: "application/json"},
		ut.Header{Key: "If-Match", Value: `"` + updated.Revision + `"`})
	if preview.Code != 200 || !containsJSON(preview.Body.Bytes(), `"field":"lyricsSidecar"`) {
		t.Fatalf("sidecar restore preview = %d %s", preview.Code, preview.Body.String())
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
		t.Fatalf("restored sidecar still exists = %d %s", read.Code, read.Body.String())
	}
}

func TestRescanRejectsUnknownLibrary(t *testing.T) {
	s := newTestServer(t)
	response := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/libraries/unknown/scans", nil)
	if response.Code != 404 || !containsJSON(response.Body.Bytes(), `"code":"library_not_found"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestRescanQueuesPersistentJobAndExposesStatus(t *testing.T) {
	s := newTestServer(t)
	manager := jobs.New(s.store)
	manager.Register(domain.JobScan, func(ctx context.Context, _ domain.Job, progress jobs.Progress) error {
		if err := progress(0, 2, 0, 0, "扫描中"); err != nil {
			return err
		}
		if err := s.library.Rescan(ctx); err != nil {
			return err
		}
		return progress(2, 2, 2, 0, "扫描完成")
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	s.SetJobManager(manager)

	response := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/libraries/"+s.library.Library().ID+"/scans", nil)
	if response.Code != 202 || !containsJSON(response.Body.Bytes(), `"state":"waiting"`) {
		t.Fatalf("enqueue = %d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data jobResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.Get(context.Background(), envelope.Data.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == domain.JobSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	detail := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/jobs/"+envelope.Data.ID, nil)
	if detail.Code != 200 || !containsJSON(detail.Body.Bytes(), `"state":"succeeded"`) || !containsJSON(detail.Body.Bytes(), `"processed":2`) {
		t.Fatalf("job detail = %d %s", detail.Code, detail.Body.String())
	}
	list := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/jobs", nil)
	if list.Code != 200 || !containsJSON(list.Body.Bytes(), `"id":"`+envelope.Data.ID+`"`) {
		t.Fatalf("jobs = %d %s", list.Code, list.Body.String())
	}
}

func TestJobCancelAPIImmediatelyCancelsWaitingJob(t *testing.T) {
	s := newTestServer(t)
	manager := jobs.New(s.store)
	s.SetJobManager(manager)
	created, err := manager.Enqueue(context.Background(), domain.Job{Kind: domain.JobScan, LibraryID: s.library.Library().ID, Title: "Cancelable", Detail: "waiting"})
	if err != nil {
		t.Fatal(err)
	}
	response := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/jobs/"+created.ID+"/cancel", nil)
	if response.Code != 200 || !containsJSON(response.Body.Bytes(), `"state":"cancelled"`) {
		t.Fatalf("cancel = %d %s", response.Code, response.Body.String())
	}
}

func TestJobRetryAPIOnlyResubmitsFailedMatchItems(t *testing.T) {
	s := newTestServer(t)
	manager := jobs.New(s.store)
	s.SetJobManager(manager)
	payload := `{"trackIds":["trk-failed","trk-ok"],"providerIds":[],"limit":5}`
	created, err := manager.Enqueue(context.Background(), domain.Job{ID: "job-retry-match", Kind: domain.JobMatch, LibraryID: s.library.Library().ID, Title: "Match", Detail: "partial", State: domain.JobPartial, Payload: payload, Total: 2, Processed: 2, Succeeded: 1, Failed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.UpsertMatchItem(context.Background(), store.MatchItem{JobID: created.ID, TrackID: "trk-failed", State: "failed"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.UpsertMatchItem(context.Background(), store.MatchItem{JobID: created.ID, TrackID: "trk-ok", State: "review"}); err != nil {
		t.Fatal(err)
	}
	response := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/jobs/"+created.ID+"/retry", nil)
	if response.Code != 202 || !containsJSON(response.Body.Bytes(), `"state":"waiting"`) {
		t.Fatalf("retry = %d %s", response.Code, response.Body.String())
	}
	retried, err := s.store.Job(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var filtered struct {
		TrackIDs []string `json:"trackIds"`
	}
	if err := json.Unmarshal([]byte(retried.Payload), &filtered); err != nil || len(filtered.TrackIDs) != 1 || filtered.TrackIDs[0] != "trk-failed" {
		t.Fatalf("retry payload = %q", retried.Payload)
	}
}

func TestJobRetryAPIResubmitsArtworkFailureWithCurrentRevision(t *testing.T) {
	s := newTestServer(t)
	manager := jobs.New(s.store)
	s.SetJobManager(manager)
	matchJob, err := manager.Enqueue(context.Background(), domain.Job{ID: "job-match-artwork", Kind: domain.JobMatch, LibraryID: s.library.Library().ID, Title: "Match", Detail: "review", State: domain.JobReview})
	if err != nil {
		t.Fatal(err)
	}
	track := s.library.ListTracks(library.TrackFilter{})[0]
	candidates, _ := json.Marshal([]providers.MatchCandidate{{ID: "cand-artwork", ProviderID: "test-provider", Title: providers.Field[string]{Value: track.Title}}})
	if err := s.store.UpsertMatchItem(context.Background(), store.MatchItem{JobID: matchJob.ID, TrackID: track.ID, State: "artwork_failed", Candidates: candidates, SelectedCandidateID: "cand-artwork"}); err != nil {
		t.Fatal(err)
	}
	writePayload := `{"matchJobId":"` + matchJob.ID + `","items":[{"trackId":"` + track.ID + `","candidateId":"cand-artwork","baseRevision":"stale-revision","fields":["title"],"artwork":true}]}`
	writeJob, err := manager.Enqueue(context.Background(), domain.Job{ID: "job-write-artwork", Kind: domain.JobWrite, LibraryID: s.library.Library().ID, Title: "Write", Detail: "partial", State: domain.JobPartial, Payload: writePayload, Total: 1, Processed: 1, Failed: 1})
	if err != nil {
		t.Fatal(err)
	}
	response := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/jobs/"+writeJob.ID+"/retry", nil)
	if response.Code != 202 || !containsJSON(response.Body.Bytes(), `"state":"waiting"`) {
		t.Fatalf("retry = %d %s", response.Code, response.Body.String())
	}
	retried, err := s.store.Job(context.Background(), writeJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	var filtered struct {
		Items []struct {
			BaseRevision string   `json:"baseRevision"`
			Fields       []string `json:"fields"`
			Artwork      bool     `json:"artwork"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(retried.Payload), &filtered); err != nil || len(filtered.Items) != 1 {
		t.Fatalf("retry payload = %q err=%v", retried.Payload, err)
	}
	if filtered.Items[0].BaseRevision != track.Revision || len(filtered.Items[0].Fields) != 0 || !filtered.Items[0].Artwork {
		t.Fatalf("artwork retry item = %#v want revision %s", filtered.Items[0], track.Revision)
	}
}

func TestBatchEditAPIQueuesRevisionGuardedJob(t *testing.T) {
	s := newTestServer(t)
	manager := jobs.New(s.store)
	s.SetJobManager(manager)
	track := s.library.ListTracks(library.TrackFilter{})[0]
	body := []byte(`{"items":[{"trackId":"` + track.ID + `","baseRevision":"` + track.Revision + `"}],"operations":[{"field":"genres","mode":"append","value":"Live"}],"sequenceTracks":true}`)
	response := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/tracks/batch-edit",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)}, ut.Header{Key: "content-type", Value: "application/json"})
	if response.Code != 202 || !containsJSON(response.Body.Bytes(), `"kind":"batch_edit"`) || !containsJSON(response.Body.Bytes(), `"total":1`) {
		t.Fatalf("batch edit = %d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data jobResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	job, err := s.store.Job(context.Background(), envelope.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	var payload domain.BatchEditPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil || len(payload.Items) != 1 || payload.Items[0].BaseRevision != track.Revision {
		t.Fatalf("batch payload = %#v err=%v", payload, err)
	}
	itemsResponse := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/jobs/"+envelope.Data.ID+"/batch-edit-items", nil)
	if itemsResponse.Code != 200 || !containsJSON(itemsResponse.Body.Bytes(), "[]") {
		t.Fatalf("batch edit items = %d %s", itemsResponse.Code, itemsResponse.Body.String())
	}
	invalid := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/tracks/batch-edit",
		&ut.Body{Body: bytes.NewReader([]byte(`{"items":[{"trackId":"` + track.ID + `"}],"operations":[],"sequenceTracks":false}`)), Len: len([]byte(`{"items":[{"trackId":"` + track.ID + `"}],"operations":[],"sequenceTracks":false}`))},
		ut.Header{Key: "content-type", Value: "application/json"})
	if invalid.Code != 400 || !containsJSON(invalid.Body.Bytes(), `"code":"invalid_request"`) {
		t.Fatalf("invalid batch edit = %d %s", invalid.Code, invalid.Body.String())
	}
	invalidYearBody := []byte(`{"items":[{"trackId":"` + track.ID + `"}],"operations":[{"field":"year","mode":"set","value":"20x"}],"sequenceTracks":false}`)
	invalidYear := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/tracks/batch-edit",
		&ut.Body{Body: bytes.NewReader(invalidYearBody), Len: len(invalidYearBody)},
		ut.Header{Key: "content-type", Value: "application/json"})
	if invalidYear.Code != 400 || !containsJSON(invalidYear.Body.Bytes(), `"年份必须是正整数"`) {
		t.Fatalf("invalid year batch edit = %d %s", invalidYear.Code, invalidYear.Body.String())
	}
	extendedBody := []byte(`{"items":[{"trackId":"` + track.ID + `"}],"operations":[{"field":"comment","mode":"set","value":"liner note"},{"field":"composers","mode":"append","value":"Composer"},{"field":"bpm","mode":"set","value":"128"}],"sequenceTracks":false}`)
	extended := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/tracks/batch-edit",
		&ut.Body{Body: bytes.NewReader(extendedBody), Len: len(extendedBody)}, ut.Header{Key: "content-type", Value: "application/json"})
	if extended.Code != 202 || !containsJSON(extended.Body.Bytes(), `"kind":"batch_edit"`) {
		t.Fatalf("extended batch edit = %d %s", extended.Code, extended.Body.String())
	}
	invalidAppendBody := []byte(`{"items":[{"trackId":"` + track.ID + `"}],"operations":[{"field":"comment","mode":"append","value":"extra"}],"sequenceTracks":false}`)
	invalidAppend := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/tracks/batch-edit",
		&ut.Body{Body: bytes.NewReader(invalidAppendBody), Len: len(invalidAppendBody)}, ut.Header{Key: "content-type", Value: "application/json"})
	if invalidAppend.Code != 400 || !containsJSON(invalidAppend.Body.Bytes(), `不支持追加操作`) {
		t.Fatalf("invalid extended append = %d %s", invalidAppend.Code, invalidAppend.Body.String())
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

func TestProviderSettingsAndConnectionTestAPI(t *testing.T) {
	s := newTestServer(t)
	disableBody := []byte(`{"enabled":false}`)
	disabled := ut.PerformRequest(s.h.Engine, "PATCH", "/api/v1/providers/test-provider",
		&ut.Body{Body: bytes.NewReader(disableBody), Len: len(disableBody)}, ut.Header{Key: "content-type", Value: "application/json"})
	if disabled.Code != 200 || !containsJSON(disabled.Body.Bytes(), `"enabled":false`) || !containsJSON(disabled.Body.Bytes(), `"health":"disabled"`) {
		t.Fatalf("disable provider = %d %s", disabled.Code, disabled.Body.String())
	}
	testDisabled := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/providers/test-provider/test", nil)
	if testDisabled.Code != 422 || !containsJSON(testDisabled.Body.Bytes(), `"code":"provider_disabled"`) {
		t.Fatalf("test disabled = %d %s", testDisabled.Code, testDisabled.Body.String())
	}
	enableBody := []byte(`{"enabled":true}`)
	enabled := ut.PerformRequest(s.h.Engine, "PATCH", "/api/v1/providers/test-provider",
		&ut.Body{Body: bytes.NewReader(enableBody), Len: len(enableBody)}, ut.Header{Key: "content-type", Value: "application/json"})
	if enabled.Code != 200 || !containsJSON(enabled.Body.Bytes(), `"enabled":true`) {
		t.Fatalf("enable = %d %s", enabled.Code, enabled.Body.String())
	}
	tested := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/providers/test-provider/test", nil)
	if tested.Code != 200 || !containsJSON(tested.Body.Bytes(), `"status":"ok"`) {
		t.Fatalf("test provider = %d %s", tested.Code, tested.Body.String())
	}
}

func TestMatchItemsAPIReadsPersistedCandidates(t *testing.T) {
	s := newTestServer(t)
	job, err := s.store.CreateJob(context.Background(), domain.Job{ID: "job-match-test", Kind: domain.JobMatch, Title: "Match", Detail: "review"})
	if err != nil {
		t.Fatal(err)
	}
	candidates, _ := json.Marshal([]providers.MatchCandidate{{ID: "cand-1", ProviderID: "test-provider", Title: providers.Field[string]{Value: "Alpha", Source: "Test"}}})
	if err := s.store.UpsertMatchItem(context.Background(), store.MatchItem{JobID: job.ID, TrackID: "trk-test", State: "review", Candidates: candidates}); err != nil {
		t.Fatal(err)
	}
	response := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/jobs/"+job.ID+"/matches", nil)
	if response.Code != 200 || !containsJSON(response.Body.Bytes(), `"id":"cand-1"`) || !containsJSON(response.Body.Bytes(), `"trackId":"trk-test"`) {
		t.Fatalf("match items = %d %s", response.Code, response.Body.String())
	}
}

func TestMatchWriteAPIMarksAcceptedItemsWritePending(t *testing.T) {
	s := newTestServer(t)
	manager := jobs.New(s.store)
	s.SetJobManager(manager)
	track := s.library.ListTracks(library.TrackFilter{})[0]
	matchJob, err := manager.Enqueue(context.Background(), domain.Job{
		ID: "job-match-review", Kind: domain.JobMatch, LibraryID: s.library.Library().ID,
		Title: "Match", Detail: "review", State: domain.JobReview, Total: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, _ := json.Marshal([]providers.MatchCandidate{{
		ID: "candidate-accepted", ProviderID: "test-provider",
		Title: providers.Field[string]{Value: track.Title, Source: "Test"},
	}})
	if err := s.store.UpsertMatchItem(context.Background(), store.MatchItem{
		JobID: matchJob.ID, TrackID: track.ID, State: "review", Candidates: candidates,
	}); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"items":[{"trackId":"` + track.ID + `","candidateId":"candidate-accepted","baseRevision":"` + track.Revision + `","fields":["title"]}]}`)
	response := ut.PerformRequest(s.h.Engine, "POST", "/api/v1/matches/jobs/"+matchJob.ID+"/write",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)}, ut.Header{Key: "content-type", Value: "application/json"})
	if response.Code != 202 || !containsJSON(response.Body.Bytes(), `"kind":"write"`) {
		t.Fatalf("match write = %d %s", response.Code, response.Body.String())
	}
	item, err := s.store.MatchItem(context.Background(), matchJob.ID, track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != "write_pending" || item.SelectedCandidateID != "candidate-accepted" {
		t.Fatalf("match item after write enqueue = %#v", item)
	}
}

func TestMatchReviewStateAPIUpdatesPersistedDecision(t *testing.T) {
	s := newTestServer(t)
	manager := jobs.New(s.store)
	s.SetJobManager(manager)
	track := s.library.ListTracks(library.TrackFilter{})[0]
	matchJob, err := manager.Enqueue(context.Background(), domain.Job{
		ID: "job-review-state", Kind: domain.JobMatch, LibraryID: s.library.Library().ID,
		Title: "Match", Detail: "review", State: domain.JobReview, Total: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, _ := json.Marshal([]providers.MatchCandidate{{ID: "candidate-review", ProviderID: "test-provider", Title: providers.Field[string]{Value: "Song", Source: "Test"}}})
	if err := s.store.UpsertMatchItem(context.Background(), store.MatchItem{JobID: matchJob.ID, TrackID: track.ID, State: "review", Candidates: candidates}); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"state":"accepted","selectedCandidateId":"candidate-review","fields":["title","comment"],"artwork":true}`)
	accepted := ut.PerformRequest(s.h.Engine, "PATCH", "/api/v1/matches/jobs/"+matchJob.ID+"/items/"+track.ID,
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)}, ut.Header{Key: "content-type", Value: "application/json"})
	if accepted.Code != 200 || !containsJSON(accepted.Body.Bytes(), `"state":"accepted"`) || !containsJSON(accepted.Body.Bytes(), `"selectedCandidateId":"candidate-review"`) || !containsJSON(accepted.Body.Bytes(), `"reviewFields":["title","comment"]`) || !containsJSON(accepted.Body.Bytes(), `"reviewArtwork":true`) {
		t.Fatalf("accepted review state = %d %s", accepted.Code, accepted.Body.String())
	}
	item, err := s.store.MatchItem(context.Background(), matchJob.ID, track.ID)
	if err != nil || item.State != "accepted" || item.SelectedCandidateID != "candidate-review" || len(item.ReviewFields) != 2 || !item.ReviewArtwork {
		t.Fatalf("accepted item = %#v err=%v", item, err)
	}
	skippedBody := []byte(`{"state":"skipped"}`)
	skipped := ut.PerformRequest(s.h.Engine, "PATCH", "/api/v1/matches/jobs/"+matchJob.ID+"/items/"+track.ID,
		&ut.Body{Body: bytes.NewReader(skippedBody), Len: len(skippedBody)}, ut.Header{Key: "content-type", Value: "application/json"})
	if skipped.Code != 200 || !containsJSON(skipped.Body.Bytes(), `"state":"skipped"`) || !containsJSON(skipped.Body.Bytes(), `"reviewFields":["title","comment"]`) || !containsJSON(skipped.Body.Bytes(), `"reviewArtwork":true`) {
		t.Fatalf("skipped review state = %d %s", skipped.Code, skipped.Body.String())
	}
	invalidBody := []byte(`{"state":"accepted","selectedCandidateId":"missing"}`)
	invalid := ut.PerformRequest(s.h.Engine, "PATCH", "/api/v1/matches/jobs/"+matchJob.ID+"/items/"+track.ID,
		&ut.Body{Body: bytes.NewReader(invalidBody), Len: len(invalidBody)}, ut.Header{Key: "content-type", Value: "application/json"})
	if invalid.Code != 400 || !containsJSON(invalid.Body.Bytes(), `candidateId`) {
		t.Fatalf("invalid candidate review state = %d %s", invalid.Code, invalid.Body.String())
	}
}

func TestCandidateArtworkPreviewUsesShortLivedProviderReference(t *testing.T) {
	s := newTestServer(t)
	result, err := s.providers.Search(context.Background(), providers.Query{Title: "Preview"}, nil, 1)
	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("provider search = %#v err=%v", result, err)
	}
	asset := artwork.Asset{MIME: "image/png", Data: []byte("preview-image"), Width: 1, Height: 1, Size: 13, Hash: "preview-hash", Format: "png"}
	s.downloadArtwork = func(_ context.Context, reference providers.ArtworkReference) (artwork.Asset, error) {
		if reference.CandidateID != result.Candidates[0].ID {
			t.Fatalf("artwork reference = %#v", reference)
		}
		return asset, nil
	}
	response := ut.PerformRequest(s.h.Engine, "GET", "/api/v1/matches/candidates/"+result.Candidates[0].ID+"/artwork", nil)
	if response.Code != 200 || response.Body.String() != "preview-image" || response.Result().Header.Get("Content-Type") != "image/png" || response.Result().Header.Get("Cache-Control") != "private, max-age=300" {
		t.Fatalf("candidate preview = %d type=%q cache=%q body=%q", response.Code, response.Result().Header.Get("Content-Type"), response.Result().Header.Get("Cache-Control"), response.Body.String())
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
