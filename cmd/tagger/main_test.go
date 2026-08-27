package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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

type artworkTestStrategy struct{}

type failingArtworkWriteEngine struct {
	mu  sync.Mutex
	raw map[string][]string
}

func (e *failingArtworkWriteEngine) Read(context.Context, string) (tags.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return tags.Snapshot{Raw: cloneTestTags(e.raw), DurationSeconds: 120}, nil
}

func (e *failingArtworkWriteEngine) Write(_ context.Context, _ string, updates map[string][]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for key, values := range updates {
		if len(values) == 0 {
			delete(e.raw, key)
			continue
		}
		e.raw[key] = append([]string(nil), values...)
	}
	return nil
}

func (*failingArtworkWriteEngine) Version() string { return "failing-artwork-test" }

func (*failingArtworkWriteEngine) ReadArtwork(context.Context, string, int) ([]byte, error) {
	return nil, nil
}

func (*failingArtworkWriteEngine) WriteArtwork(context.Context, string, int, []byte, string) error {
	return errors.New("simulated artwork write failure")
}

func cloneTestTags(raw map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(raw))
	for key, values := range raw {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}

func (artworkTestStrategy) Descriptor() providers.Descriptor {
	return providers.Descriptor{ID: "artwork-test", Name: "Artwork Test", Enabled: true, Health: providers.HealthReady}
}

func (artworkTestStrategy) Search(context.Context, providers.Query, int) ([]providers.Candidate, error) {
	return []providers.Candidate{{ProviderID: "artwork-test", ExternalID: "external-1", Title: "Song", ArtworkURL: "https://images.example.test/cover.jpg"}}, nil
}

func TestPatchFromCandidateDistinguishesOmittedAndEmptyFieldLists(t *testing.T) {
	candidate := providers.MatchCandidate{
		Title:     providers.Field[string]{Value: "新标题"},
		DiscTotal: providers.Field[int]{Value: 2},
		Lyrics:    &providers.Field[string]{Value: "歌词"},
	}

	allFields := patchFromCandidate(candidate, nil)
	if allFields.Title == nil || allFields.DiscTotal == nil || allFields.Lyrics == nil {
		t.Fatalf("nil fields should preserve the backwards-compatible all-fields behavior: %#v", allFields)
	}

	noFields := patchFromCandidate(candidate, []string{})
	if noFields.Title != nil || noFields.DiscTotal != nil || noFields.Lyrics != nil {
		t.Fatalf("an explicit empty field list must produce a no-op patch: %#v", noFields)
	}

	selected := patchFromCandidate(candidate, []string{"title"})
	if selected.Title == nil || selected.DiscTotal != nil || selected.Lyrics != nil {
		t.Fatalf("selected fields must be applied precisely: %#v", selected)
	}
}

func TestFormatMatchProgressIncludesProviderAndCandidateCounts(t *testing.T) {
	if got := formatMatchProgress(3, 10, 9, 14, 0); got != "已分析 3/10 首曲目 · 已查询 9 次数据源 · 返回 14 个候选" {
		t.Fatalf("progress = %q", got)
	}
	if got := formatMatchProgress(10, 10, 20, 14, 2); !strings.Contains(got, "2 首失败或无匹配") {
		t.Fatalf("failed progress = %q", got)
	}
}

func TestFormatItemProgressSeparatesProcessedAndSucceeded(t *testing.T) {
	got := formatItemProgress("写入", 8, 8, 0, 8, errors.New("create temporary copy: permission denied"))
	for _, want := range []string{"已处理 8/8 首曲目", "写入成功 0 首", "失败 8 首", "最近失败：create temporary copy: permission denied"} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress %q does not contain %q", got, want)
		}
	}
}

func TestFinalizeMatchJobAfterWriteClosesCompletedReview(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	matchJob, err := manager.Enqueue(context.Background(), domain.Job{
		ID: "job-match-finalize", Kind: domain.JobMatch, State: domain.JobReview,
		Title: "批量抓取元数据", Detail: "等待审核", Total: 3, Processed: 3, Succeeded: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	for trackID, state := range map[string]string{"written-1": "written", "written-2": "written", "skipped-1": "skipped"} {
		if err := repository.UpsertMatchItem(context.Background(), store.MatchItem{JobID: matchJob.ID, TrackID: trackID, State: state}); err != nil {
			t.Fatal(err)
		}
	}

	if err := finalizeMatchJobAfterWrite(context.Background(), manager, repository, matchJob.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Get(context.Background(), matchJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != domain.JobSucceeded || updated.Processed != 3 || updated.Succeeded != 3 || updated.Failed != 0 || !strings.Contains(updated.Detail, "已写入 2 首") || !strings.Contains(updated.Detail, "跳过 1 首") {
		t.Fatalf("finalized match job = %#v", updated)
	}
}

func TestFinalizeMatchJobAfterWriteKeepsReviewWhenItemsRemain(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	matchJob, err := manager.Enqueue(context.Background(), domain.Job{
		ID: "job-match-pending", Kind: domain.JobMatch, State: domain.JobReview,
		Title: "批量抓取元数据", Detail: "等待审核", Total: 2, Processed: 2, Succeeded: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for trackID, state := range map[string]string{"written-1": "written", "review-1": "review"} {
		if err := repository.UpsertMatchItem(context.Background(), store.MatchItem{JobID: matchJob.ID, TrackID: trackID, State: state}); err != nil {
			t.Fatal(err)
		}
	}

	if err := finalizeMatchJobAfterWrite(context.Background(), manager, repository, matchJob.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Get(context.Background(), matchJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != domain.JobReview {
		t.Fatalf("pending review should remain open, got %#v", updated)
	}
}

func TestFinalizeMatchJobAfterWriteMarksFailuresPartial(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	matchJob, err := manager.Enqueue(context.Background(), domain.Job{
		ID: "job-match-partial", Kind: domain.JobMatch, State: domain.JobReview,
		Title: "批量抓取元数据", Detail: "等待审核", Total: 2, Processed: 2, Succeeded: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for trackID, state := range map[string]string{"written-1": "written", "failed-1": "write_failed"} {
		if err := repository.UpsertMatchItem(context.Background(), store.MatchItem{JobID: matchJob.ID, TrackID: trackID, State: state}); err != nil {
			t.Fatal(err)
		}
	}

	if err := finalizeMatchJobAfterWrite(context.Background(), manager, repository, matchJob.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Get(context.Background(), matchJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != domain.JobPartial || updated.Succeeded != 1 || updated.Failed != 1 || !strings.Contains(updated.Detail, "1 首失败") {
		t.Fatalf("partial match job = %#v", updated)
	}
}

func TestFinalizeMatchJobAfterWriteMarksAllFailuresFailed(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	matchJob, err := manager.Enqueue(context.Background(), domain.Job{
		ID: "job-match-all-failed", Kind: domain.JobMatch, State: domain.JobReview,
		Title: "批量抓取元数据", Detail: "等待审核", Total: 2, Processed: 2, Succeeded: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, trackID := range []string{"failed-1", "failed-2"} {
		if err := repository.UpsertMatchItem(context.Background(), store.MatchItem{JobID: matchJob.ID, TrackID: trackID, State: "write_failed"}); err != nil {
			t.Fatal(err)
		}
	}

	if err := finalizeMatchJobAfterWrite(context.Background(), manager, repository, matchJob.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Get(context.Background(), matchJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != domain.JobFailed || updated.Succeeded != 0 || updated.Failed != 2 || !strings.Contains(updated.Detail, "审核失败") {
		t.Fatalf("all-failed match job = %#v", updated)
	}
}

func TestFinalizeMatchJobAfterWriteClearsEarlierFailureAfterRematch(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager := jobs.New(repository)
	matchJob, err := manager.Enqueue(context.Background(), domain.Job{
		ID: "job-match-rematched", Kind: domain.JobMatch, State: domain.JobFailed,
		Title: "批量抓取元数据", Detail: "匹配失败", Total: 1, Processed: 1, Failed: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.UpsertMatchItem(context.Background(), store.MatchItem{JobID: matchJob.ID, TrackID: "rematched-1", State: "written"}); err != nil {
		t.Fatal(err)
	}

	if err := finalizeMatchJobAfterWrite(context.Background(), manager, repository, matchJob.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Get(context.Background(), matchJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != domain.JobSucceeded || updated.Succeeded != 1 || updated.Failed != 0 {
		t.Fatalf("rematched job retained stale failure = %#v", updated)
	}
}

func TestBatchEditCountsArtworkFailureOnceAndReindexesWrittenTags(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := &failingArtworkWriteEngine{raw: map[string][]string{
		"TITLE": {"Old title"}, "ARTIST": {"Artist"}, "ALBUM": {"Album"}, "ALBUMARTIST": {"Artist"},
	}}
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service, err := library.New(context.Background(), musicScanner, repository)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := filewrite.New(root, engine)
	if err != nil {
		t.Fatal(err)
	}
	track := service.ListTracks(library.TrackFilter{})[0]
	payload := domain.BatchEditPayload{
		Items:      []domain.BatchEditItem{{TrackID: track.ID, BaseRevision: track.Revision}},
		Operations: []domain.BatchEditOperation{{Field: "title", Mode: domain.BatchEditSet, Value: "New title"}},
		Artwork: &domain.BatchArtwork{
			Action: domain.BatchArtworkReplace, Data: batchArtworkData(t), MIME: "image/png",
		},
	}
	manager := jobs.New(repository)
	manager.Register(domain.JobBatchEdit, newBatchEditHandler(service, writer, repository))
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Enqueue(context.Background(), domain.Job{
		Kind: domain.JobBatchEdit, Title: "Batch", Total: 1, Payload: string(payloadJSON),
	})
	if err != nil {
		t.Fatal(err)
	}
	job := waitBatchEditJob(t, manager, created.ID)
	if job.State != domain.JobFailed || job.Processed != 1 || job.Succeeded != 0 || job.Failed != 1 {
		t.Fatalf("artwork failure counters = %#v", job)
	}
	updated, err := service.Track(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "New title" {
		t.Fatalf("successful tag phase was not reindexed: %#v", updated)
	}
	items, err := repository.ListBatchEditItems(context.Background(), created.ID)
	if err != nil || len(items) != 1 || items[0].State != "failed" || !strings.Contains(items[0].Error, "simulated artwork write failure") {
		t.Fatalf("batch item = %#v err=%v", items, err)
	}
	revisions, err := repository.ListRevisions(context.Background(), 10)
	if err != nil || len(revisions) != 1 || revisions[0].Action != "批量编辑标签" {
		t.Fatalf("tag-phase revision = %#v err=%v", revisions, err)
	}
}

func TestPatchFromCandidateIncludesSelectedExtendedFields(t *testing.T) {
	candidate := providers.MatchCandidate{
		Comment:              providers.Field[string]{Value: "liner note"},
		Composers:            providers.Field[[]string]{Value: []string{"Composer"}},
		Conductor:            providers.Field[string]{Value: "Conductor"},
		Lyricists:            providers.Field[[]string]{Value: []string{"Lyricist"}},
		Copyright:            providers.Field[string]{Value: "© Tagger"},
		BPM:                  providers.Field[int]{Value: 128},
		ISRC:                 providers.Field[string]{Value: "US-TAG-26-00001"},
		MusicBrainzTrackID:   providers.Field[string]{Value: "track-mbid"},
		MusicBrainzReleaseID: providers.Field[string]{Value: "release-mbid"},
		MusicBrainzArtistIDs: providers.Field[[]string]{Value: []string{"artist-mbid"}},
		AcoustID:             providers.Field[string]{Value: "acoustid-id"},
		AcoustIDFingerprint:  providers.Field[string]{Value: "fingerprint"},
	}
	patch := patchFromCandidate(candidate, []string{"comment", "composers", "conductor", "lyricists", "copyright", "bpm", "isrc", "musicbrainzTrackId", "musicbrainzReleaseId", "musicbrainzArtistIds", "acoustidId", "acoustidFingerprint"})
	if patch.Comment == nil || patch.Composers == nil || patch.Conductor == nil || patch.Lyricists == nil || patch.Copyright == nil || patch.BPM == nil || patch.ISRC == nil || patch.MusicBrainzTrackID == nil || patch.MusicBrainzReleaseID == nil || patch.MusicBrainzArtistIDs == nil || patch.AcoustID == nil || patch.AcoustIDFingerprint == nil {
		t.Fatalf("extended candidate patch = %#v", patch)
	}
}

func TestPrepareCandidateArtworkUsesRegistryReference(t *testing.T) {
	registry := providers.NewRegistry(artworkTestStrategy{})
	result, err := registry.Search(context.Background(), providers.Query{Title: "Song"}, nil, 1)
	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("search = %#v err=%v", result, err)
	}
	want := artwork.Asset{MIME: "image/png", Data: []byte("image")}
	got, err := prepareCandidateArtwork(context.Background(), registry, result.Candidates[0], func(_ context.Context, reference providers.ArtworkReference) (artwork.Asset, error) {
		if reference.ProviderID != "artwork-test" || reference.URL == "" {
			t.Fatalf("reference = %#v", reference)
		}
		return want, nil
	})
	if err != nil || got == nil || string(got.Data) != "image" {
		t.Fatalf("asset = %#v err=%v", got, err)
	}
	_, err = prepareCandidateArtwork(context.Background(), registry, providers.MatchCandidate{ID: "missing"}, func(context.Context, providers.ArtworkReference) (artwork.Asset, error) {
		return want, nil
	})
	if !errors.Is(err, providers.ErrArtworkReferenceNotFound) {
		t.Fatalf("missing reference error = %v", err)
	}
}

func TestPatchFromBatchEditBuildsExplicitOperations(t *testing.T) {
	track := domain.Track{Title: "[Live] 旧标题", Artists: []string{"原艺人 (原唱)", "原艺人 (原唱)"}, Album: "旧专辑", AlbumArtists: []string{"原艺人"}, Genres: []string{"Pop"}, Year: ptr(2020), ISRC: "US-OLD-001"}
	patch := patchFromBatchEdit(track, []domain.BatchEditOperation{
		{Field: "album", Mode: domain.BatchEditSet, Value: "新专辑"},
		{Field: "albumArtists", Mode: domain.BatchEditAppend, Value: "制作人"},
		{Field: "genres", Mode: domain.BatchEditAppend, Value: "Live, Pop"},
	}, true, 2, 5)
	if patch.Album == nil || patch.Album.Value != "新专辑" || patch.AlbumArtists == nil || len(patch.AlbumArtists.Value) != 2 ||
		patch.Genres == nil || len(patch.Genres.Value) != 2 || patch.TrackNumber == nil || patch.TrackNumber.Value != 3 || patch.TrackTotal == nil || patch.TrackTotal.Value != 5 {
		t.Fatalf("batch patch = %#v", patch)
	}
	deleted := patchFromBatchEdit(track, []domain.BatchEditOperation{
		{Field: "genres", Mode: domain.BatchEditDelete},
		{Field: "year", Mode: domain.BatchEditDelete},
	}, false, 0, 1)
	if deleted.Genres == nil || deleted.Genres.Op != domain.OperationDelete || deleted.Year == nil || deleted.Year.Op != domain.OperationDelete {
		t.Fatalf("delete batch patch = %#v", deleted)
	}
	extended := patchFromBatchEdit(track, []domain.BatchEditOperation{
		{Field: "comment", Mode: domain.BatchEditSet, Value: "liner note"},
		{Field: "composers", Mode: domain.BatchEditSet, Value: "Composer A, Composer B"},
		{Field: "bpm", Mode: domain.BatchEditSet, Value: "128"},
	}, false, 0, 1)
	if extended.Comment == nil || extended.Comment.Value != "liner note" || extended.Composers == nil || len(extended.Composers.Value) != 2 || extended.BPM == nil || extended.BPM.Value != 128 {
		t.Fatalf("extended batch patch = %#v", extended)
	}
	replaced := patchFromBatchEdit(track, []domain.BatchEditOperation{
		{Field: "title", Mode: domain.BatchEditReplace, Find: "[Live] ", Value: ""},
		{Field: "artists", Mode: domain.BatchEditReplace, Find: " (原唱)", Value: ""},
		{Field: "isrc", Mode: domain.BatchEditReplace, Find: "OLD", Value: "NEW"},
	}, false, 0, 1)
	if replaced.Title == nil || replaced.Title.Value != "旧标题" || replaced.Artists == nil || len(replaced.Artists.Value) != 1 || replaced.Artists.Value[0] != "原艺人" || replaced.ISRC == nil || replaced.ISRC.Value != "US-NEW-001" {
		t.Fatalf("replace batch patch = %#v", replaced)
	}
}

func ptr(value int) *int { return &value }
