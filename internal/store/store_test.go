package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/pressly/goose/v3"
)

func TestOpenMigratesAndConfiguresSQLite(t *testing.T) {
	dataStore := openTestStore(t)

	var foreignKeys int
	if err := dataStore.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	var journalMode string
	if err := dataStore.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
	var migrations int
	if err := dataStore.db.QueryRow(`SELECT COUNT(*) FROM goose_db_version WHERE is_applied = 1`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations == 0 {
		t.Fatal("migration table contains no applied migration")
	}
}

func TestEmbeddedTagProjectionMigrationMarksPresentFilesDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projection.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	migrationMu.Lock()
	goose.SetBaseFS(migrationFiles)
	if err := goose.SetDialect("sqlite3"); err != nil {
		migrationMu.Unlock()
		t.Fatal(err)
	}
	err = goose.UpToContext(context.Background(), db, "migrations", 14, goose.WithNoColor(true))
	migrationMu.Unlock()
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO libraries(id, root_path, name, summary_json, report_json, scan_token, updated_at) VALUES('lib-1', '/music', 'Music', '{}', '{}', 'scan', 'now')`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO library_files(library_id, relative_path, track_id, folder_id, format, file_size, file_mtime_ns, sidecar_size, sidecar_mtime_ns, writable, present, sync_state, parse_error, missing_since, updated_at) VALUES('lib-1', 'song.mp3', 'trk-1', 'folder-root', 'mp3', 1, 1, 0, 0, 1, 1, 'indexed', 'old error', '', 'now')`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	dataStore, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	var state, parseError string
	if err := dataStore.db.QueryRow(`SELECT sync_state, parse_error FROM library_files WHERE library_id='lib-1' AND relative_path='song.mp3'`).Scan(&state, &parseError); err != nil {
		t.Fatal(err)
	}
	if state != string(domain.SyncDraft) || parseError != "" {
		t.Fatalf("projection state=%q parseError=%q", state, parseError)
	}
}

func TestJobStateConsistencyMigrationMarksAllFailedJobsFailed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job-state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	migrationMu.Lock()
	goose.SetBaseFS(migrationFiles)
	if err := goose.SetDialect("sqlite3"); err != nil {
		migrationMu.Unlock()
		t.Fatal(err)
	}
	err = goose.UpToContext(context.Background(), db, "migrations", 15, goose.WithNoColor(true))
	migrationMu.Unlock()
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	const timestamp = "2026-08-28T08:03:01Z"
	if _, err := db.Exec(`
		INSERT INTO jobs(id, kind, state, library_id, title, detail, processed, total, succeeded, failed, error_text, payload_json, created_at, updated_at)
		VALUES
			('job-all-failed', 'write', 'partial', 'lib-1', 'Write', '已写入 8/8 首曲目', 8, 8, 0, 8, '', '{}', ?, ?),
			('job-mixed', 'write', 'partial', 'lib-1', 'Write', 'mixed result', 8, 8, 3, 5, '', '{}', ?, ?)`,
		timestamp, timestamp, timestamp, timestamp); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	dataStore, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	failedJob, err := dataStore.Job(context.Background(), "job-all-failed")
	if err != nil {
		t.Fatal(err)
	}
	if failedJob.State != domain.JobFailed || failedJob.Detail != "处理完成：成功 0 项，失败 8 项" {
		t.Fatalf("migrated all-failed job = %#v", failedJob)
	}
	mixedJob, err := dataStore.Job(context.Background(), "job-mixed")
	if err != nil {
		t.Fatal(err)
	}
	if mixedJob.State != domain.JobPartial || mixedJob.Detail != "mixed result" {
		t.Fatalf("migration changed mixed job = %#v", mixedJob)
	}
}

func TestSaveLoadAndReplaceScan(t *testing.T) {
	dataStore := openTestStore(t)
	root := "/music/archive"
	first := testScanResult("lib-1", "Archive", testTrack("trk-1", "one.mp3"), testTrack("trk-2", "two.flac"))
	if err := dataStore.SaveScan(context.Background(), root, first); err != nil {
		t.Fatal(err)
	}
	var inventoryCount int
	if err := dataStore.db.QueryRow(`SELECT COUNT(*) FROM library_files WHERE library_id=? AND present=1 AND sync_state='indexed'`, "lib-1").Scan(&inventoryCount); err != nil || inventoryCount != 2 {
		t.Fatalf("live inventory count=%d err=%v", inventoryCount, err)
	}

	loaded, found, err := dataStore.LoadScan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !reflect.DeepEqual(loaded, first) {
		t.Fatalf("loaded scan mismatch:\n got %#v\nwant %#v", loaded, first)
	}

	second := testScanResult("lib-1", "Archive", testTrack("trk-2", "two.flac"), testTrack("trk-3", "three.wav"))
	if err := dataStore.SaveScan(context.Background(), root, second); err != nil {
		t.Fatal(err)
	}
	loaded, found, err = dataStore.LoadScan(context.Background(), root)
	if err != nil || !found {
		t.Fatalf("load replaced scan: found=%v err=%v", found, err)
	}
	if len(loaded.Tracks) != 2 || loaded.Tracks[0].ID != "trk-3" || loaded.Tracks[1].ID != "trk-2" {
		t.Fatalf("stale track was not removed or sort is wrong: %#v", loaded.Tracks)
	}
}

func TestListLibrariesMarksActiveAndKeepsRootsIndependent(t *testing.T) {
	dataStore := openTestStore(t)
	firstRoot := filepath.Join(t.TempDir(), "First")
	secondRoot := filepath.Join(t.TempDir(), "Second")
	if err := dataStore.SaveScan(context.Background(), firstRoot, testScanResult("lib-first", "First", testTrack("trk-first", "one.mp3"))); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SaveScan(context.Background(), secondRoot, testScanResult("lib-second", "Second", testTrack("trk-second", "two.flac"))); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SetLibraryRoot(context.Background(), secondRoot); err != nil {
		t.Fatal(err)
	}
	libraries, err := dataStore.ListLibraries(context.Background(), secondRoot)
	if err != nil || len(libraries) != 2 {
		t.Fatalf("libraries=%#v err=%v", libraries, err)
	}
	if libraries[0].ID != "lib-second" || !libraries[0].Active || libraries[1].Active {
		t.Fatalf("active ordering/flag mismatch: %#v", libraries)
	}
	_, root, found, err := dataStore.LibraryByID(context.Background(), "lib-first")
	if err != nil || !found || root != firstRoot {
		t.Fatalf("library lookup root=%q found=%v err=%v", root, found, err)
	}
}

func TestLibrariesWithSameRelativeTrackPathRemainIndependent(t *testing.T) {
	dataStore := openTestStore(t)
	firstRoot := filepath.Join(t.TempDir(), "First")
	secondRoot := filepath.Join(t.TempDir(), "Second")
	track := testTrack("same-track", "Artist/Album/01.mp3")
	if err := dataStore.SaveScan(context.Background(), firstRoot, testScanResult("lib-first", "First", track)); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SaveScan(context.Background(), secondRoot, testScanResult("lib-second", "Second", track)); err != nil {
		t.Fatal(err)
	}
	first, found, err := dataStore.LoadScan(context.Background(), firstRoot)
	if err != nil || !found || len(first.Tracks) != 1 || first.Tracks[0].ID != track.ID {
		t.Fatalf("first library = %#v found=%v err=%v", first, found, err)
	}
	second, found, err := dataStore.LoadScan(context.Background(), secondRoot)
	if err != nil || !found || len(second.Tracks) != 1 || second.Tracks[0].ID != track.ID {
		t.Fatalf("second library = %#v found=%v err=%v", second, found, err)
	}
}

func TestDeleteLibraryCleansTaggerDataButLeavesMusicFiles(t *testing.T) {
	dataStore := openTestStore(t)
	root := filepath.Join(t.TempDir(), "Music")
	path := filepath.Join(root, "song.mp3")
	content := []byte("audio sentinel")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	track := testTrack("track-delete", "song.mp3")
	track.FileFingerprint = domain.FileFingerprint{SizeBytes: int64(len(content)), ModifiedUnixNano: 1}
	if err := dataStore.SaveScan(context.Background(), root, testScanResult("lib-delete", "Delete me", track)); err != nil {
		t.Fatal(err)
	}
	job, err := dataStore.CreateJob(context.Background(), domain.Job{Kind: domain.JobMatch, LibraryID: "lib-delete", Title: "match"})
	if err != nil {
		t.Fatal(err)
	}
	if err := dataStore.UpsertMatchItem(context.Background(), MatchItem{JobID: job.ID, TrackID: track.ID, State: "review", Candidates: []byte("[]")}); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.UpsertBatchEditItem(context.Background(), BatchEditItem{JobID: job.ID, TrackID: track.ID, State: "pending"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.AddLibraryMatchQueryHistory(context.Background(), "lib-delete", track.ID, []byte(`{"title":"song"}`), nil, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.CreateRevision(context.Background(), domain.Revision{LibraryID: "lib-delete", TrackID: track.ID, TrackTitle: "song", FileName: "song.mp3", Action: "test", Source: "test", BaseRevision: "a", ResultRevision: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.DeleteLibrary(context.Background(), "lib-delete"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(content) {
		t.Fatalf("music file changed or disappeared: %q err=%v", got, err)
	}
	var count int
	for _, table := range []string{"libraries", "tracks", "jobs", "match_items", "batch_edit_items", "match_query_history", "revisions"} {
		if err := dataStore.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("table %s still has %d rows", table, count)
		}
	}
}

func TestPurgeMissingRemovesOnlyMarkedIndexes(t *testing.T) {
	dataStore := openTestStore(t)
	root := filepath.Join(t.TempDir(), "Music")
	keep := testTrack("track-keep", "keep.mp3")
	missing := testTrack("track-missing", "missing.mp3")
	missing.Missing = true
	missing.MissingSince = "2026-08-21T00:00:00Z"
	missing.Health = domain.HealthMissing
	if err := dataStore.SaveScan(context.Background(), root, testScanResult("lib-purge", "Purge", keep, missing)); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.AddMatchQueryHistory(context.Background(), missing.ID, []byte(`{"title":"missing"}`), nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.AddLibraryMatchQueryHistory(context.Background(), "lib-purge", missing.ID, []byte(`{"title":"missing"}`), nil, 0); err != nil {
		t.Fatal(err)
	}
	removed, err := dataStore.PurgeMissing(context.Background(), "lib-purge")
	if err != nil || removed != 1 {
		t.Fatalf("purge removed=%d err=%v, want one", removed, err)
	}
	loaded, found, err := dataStore.LoadScan(context.Background(), root)
	if err != nil || !found || len(loaded.Tracks) != 1 || loaded.Tracks[0].ID != keep.ID {
		t.Fatalf("remaining scan=%#v found=%v err=%v", loaded, found, err)
	}
	history, err := dataStore.ListLibraryMatchQueryHistory(context.Background(), "lib-purge", missing.ID, 20)
	if err != nil || len(history) != 0 {
		t.Fatalf("missing history=%#v err=%v, want empty", history, err)
	}
}

func TestScanAndRevisionSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "tagger.db")
	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	result := testScanResult("lib-1", "Archive", testTrack("trk-1", "one.mp3"))
	if err := first.SaveScan(context.Background(), "/music/archive", result); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 20, 12, 30, 0, 0, time.UTC)
	first.now = func() time.Time { return createdAt }
	created, err := first.CreateRevision(context.Background(), domain.Revision{
		LibraryID: "lib-1", TrackID: "trk-1", TrackTitle: "New title", FileName: "one.mp3",
		Action: "修改标签", Source: "手工编辑", BaseRevision: "before", ResultRevision: "after",
		Diff:       []domain.RevisionDiff{{Field: "title", Operation: domain.OperationSet, Before: "Old title", After: "New title"}},
		CoverTone:  domain.CoverMoss,
		BeforeTags: map[string][]string{"TITLE": {"Old title"}},
		AfterTags:  map[string][]string{"TITLE": {"New title"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	loaded, found, err := reopened.LoadScan(context.Background(), "/music/archive")
	if err != nil || !found || len(loaded.Tracks) != 1 {
		t.Fatalf("persistent scan: found=%v tracks=%d err=%v", found, len(loaded.Tracks), err)
	}
	revision, err := reopened.Revision(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(revision.Diff, created.Diff) || !reflect.DeepEqual(revision.BeforeTags, created.BeforeTags) || !revision.CreatedAt.Equal(createdAt) {
		t.Fatalf("persistent revision mismatch:\n got %#v\nwant %#v", revision, created)
	}
}

func TestRevisionPersistsLyricsSidecarSnapshots(t *testing.T) {
	dataStore := openTestStore(t)
	created, err := dataStore.CreateRevision(context.Background(), domain.Revision{
		LibraryID: "lib-1", TrackID: "trk-1", TrackTitle: "Song", FileName: "song.mp3",
		Action: "写入歌词 sidecar", Source: "手工编辑", BaseRevision: "audio-before", ResultRevision: "audio-after",
		Diff:          []domain.RevisionDiff{{Field: "lyricsSidecar", Operation: domain.OperationSet, Before: nil, After: map[string]any{"exists": true}}},
		BeforeSidecar: &domain.SidecarSnapshot{Exists: true, Revision: "sidecar-before", SizeBytes: 4, ModifiedAt: "2026-08-20 12:00", Content: "old\n"},
		AfterSidecar:  &domain.SidecarSnapshot{Exists: true, Revision: "sidecar-after", SizeBytes: 4, ModifiedAt: "2026-08-20 12:01", Content: "new\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := dataStore.Revision(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BeforeSidecar == nil || loaded.AfterSidecar == nil || loaded.BeforeSidecar.Content != "old\n" || loaded.AfterSidecar.Content != "new\n" {
		t.Fatalf("sidecar snapshots = %#v", loaded)
	}
}

func TestListRevisionsIsNewestFirstAndConcurrentSafe(t *testing.T) {
	dataStore := openTestStore(t)
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	var wait sync.WaitGroup
	for index := range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := dataStore.CreateRevision(context.Background(), domain.Revision{
				ID: "revision-" + time.Duration(index).String(), LibraryID: "lib-1", TrackID: "trk-1",
				TrackTitle: "Song", FileName: "song.mp3", Action: "修改标签", Source: "测试",
				CreatedAt: base.Add(time.Duration(index) * time.Minute), Fields: []string{}, Diff: []domain.RevisionDiff{},
			})
			if err != nil {
				t.Errorf("create revision %d: %v", index, err)
			}
		}()
	}
	wait.Wait()

	revisions, err := dataStore.ListRevisions(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 5 || !revisions[0].CreatedAt.Equal(base.Add(11*time.Minute)) || !revisions[4].CreatedAt.Equal(base.Add(7*time.Minute)) {
		t.Fatalf("newest revisions = %#v", revisions)
	}
}

func TestHistoryRetentionPrunesPerTrackAndPersists(t *testing.T) {
	dataStore := openTestStore(t)
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 5; index++ {
		_, err := dataStore.CreateRevision(context.Background(), domain.Revision{
			ID: "retention-" + strconv.Itoa(index), LibraryID: "lib-1", TrackID: "track-1",
			TrackTitle: "Song", FileName: "song.mp3", Action: "修改标签", Source: "测试",
			CreatedAt: base.Add(time.Duration(index) * time.Minute), Diff: []domain.RevisionDiff{{Field: "title"}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := dataStore.SetHistoryRetention(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	revisions, err := dataStore.ListRevisions(context.Background(), 10)
	if err != nil || len(revisions) != 3 || revisions[0].ID != "retention-4" || revisions[2].ID != "retention-2" {
		t.Fatalf("retained revisions = %#v err=%v", revisions, err)
	}
	if got := dataStore.HistoryRetention(context.Background()); got != 3 {
		t.Fatalf("retention = %d, want 3", got)
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteHistorySettingDefaultsOnAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-toggle.db")
	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.WriteHistory(context.Background()) {
		t.Fatal("write history should default to enabled")
	}
	if err := first.SetWriteHistory(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if first.WriteHistory(context.Background()) {
		t.Fatal("write history should be disabled immediately")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.WriteHistory(context.Background()) {
		t.Fatal("write history setting did not persist across reopen")
	}
}

func TestBatchTrackLimitDefaultsAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "batch-limit.db")
	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := first.BatchTrackLimit(context.Background()); got != domain.DefaultBatchTrackLimit {
		t.Fatalf("default batch track limit = %d, want %d", got, domain.DefaultBatchTrackLimit)
	}
	if err := first.SetBatchTrackLimit(context.Background(), 5000); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got := second.BatchTrackLimit(context.Background()); got != 5000 {
		t.Fatalf("persisted batch track limit = %d, want 5000", got)
	}
	if err := second.SetBatchTrackLimit(context.Background(), domain.MaxBatchTrackLimit+1); err == nil {
		t.Fatal("expected oversized batch track limit to be rejected")
	}
}

func TestJobsClaimInOrderAndRecoverAfterRestart(t *testing.T) {
	dataStore := openTestStore(t)
	first, err := dataStore.CreateJob(context.Background(), domain.Job{Kind: domain.JobScan, Title: "first"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.CreateJob(context.Background(), domain.Job{Kind: domain.JobScan, Title: "second"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, found, err := dataStore.ClaimJob(context.Background())
	if err != nil || !found || claimed.ID != first.ID || claimed.State != domain.JobRunning {
		t.Fatalf("claim = %#v found=%v err=%v", claimed, found, err)
	}
	if err := dataStore.RecoverRunningJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := dataStore.Job(context.Background(), first.ID)
	if err != nil || recovered.State != domain.JobWaiting || !recovered.StartedAt.IsZero() {
		t.Fatalf("recovered = %#v err=%v", recovered, err)
	}
	jobs, err := dataStore.ListJobs(context.Background(), 10)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs = %#v err=%v", jobs, err)
	}
}

func TestProviderCacheExpiryAndSettings(t *testing.T) {
	dataStore := openTestStore(t)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dataStore.now = func() time.Time { return now }
	if err := dataStore.SaveProviderCache(context.Background(), "key", "apple", []byte(`[{"Title":"Song"}]`), time.Hour); err != nil {
		t.Fatal(err)
	}
	payload, found, err := dataStore.LoadProviderCache(context.Background(), "key")
	if err != nil || !found || string(payload) != `[{"Title":"Song"}]` {
		t.Fatalf("cache = %q found=%v err=%v", payload, found, err)
	}
	if err := dataStore.SaveProviderEnabled(context.Background(), "apple", false); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SaveProviderConfiguration(context.Background(), "apple", map[string]string{"baseUrl": "https://example.test/search", "auth": "secret"}); err != nil {
		t.Fatal(err)
	}
	settings, err := dataStore.LoadProviderSettings(context.Background())
	if err != nil || settings["apple"] {
		t.Fatalf("settings = %#v err=%v", settings, err)
	}
	configurations, err := dataStore.LoadProviderConfigurations(context.Background())
	if err != nil || configurations["apple"]["baseUrl"] != "https://example.test/search" || configurations["apple"]["auth"] != "secret" {
		t.Fatalf("provider configurations = %#v err=%v", configurations, err)
	}
	now = now.Add(2 * time.Hour)
	_, found, err = dataStore.LoadProviderCache(context.Background(), "key")
	if err != nil || found {
		t.Fatalf("expired cache found=%v err=%v", found, err)
	}
	if err := dataStore.DeleteExpiredProviderCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SaveArtworkReference(context.Background(), "candidate-1", "apple", "https://is1-ssl.mzstatic.com/cover.jpg", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	artworkPayload, err := dataStore.LoadArtworkReferences(context.Background())
	if err != nil || !strings.Contains(string(artworkPayload), "candidate-1") {
		t.Fatalf("artwork references = %s err=%v", artworkPayload, err)
	}
	now = now.Add(2 * time.Hour)
	if err := dataStore.DeleteExpiredArtworkReferences(context.Background()); err != nil {
		t.Fatal(err)
	}
	artworkPayload, err = dataStore.LoadArtworkReferences(context.Background())
	if err != nil || string(artworkPayload) != "[]" {
		t.Fatalf("expired artwork references = %s err=%v", artworkPayload, err)
	}
}

func TestLibraryRootSettingPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagger.db")
	dataStore, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "Music")
	if err := dataStore.SetLibraryRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := dataStore.LibraryRoot(context.Background())
	if err != nil || !found || loaded != root {
		t.Fatalf("root=%q found=%v err=%v", loaded, found, err)
	}
	if err := dataStore.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, found, err = reopened.LibraryRoot(context.Background())
	if err != nil || !found || loaded != root {
		t.Fatalf("reopened root=%q found=%v err=%v", loaded, found, err)
	}
	if err := reopened.ClearLibraryRoot(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, found, err = reopened.LibraryRoot(context.Background())
	if err != nil || found || loaded != "" {
		t.Fatalf("cleared root=%q found=%v err=%v", loaded, found, err)
	}
}

func TestMatchQueryHistoryPersistsNewestQueriesFirst(t *testing.T) {
	dataStore := openTestStore(t)
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dataStore.now = func() time.Time { return base }
	first, err := dataStore.AddMatchQueryHistory(context.Background(), "trk-1", []byte(`{"title":"第一首"}`), []string{"musicbrainz"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	dataStore.now = func() time.Time { return base.Add(time.Minute) }
	second, err := dataStore.AddMatchQueryHistory(context.Background(), "trk-1", []byte(`{"title":"第二首"}`), []string{"apple", "netease"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("query history IDs must be unique")
	}
	history, err := dataStore.ListMatchQueryHistory(context.Background(), "trk-1", 10)
	if err != nil || len(history) != 2 {
		t.Fatalf("history = %#v err=%v", history, err)
	}
	if history[0].ID != second.ID || string(history[0].Query) != `{"title":"第二首"}` || len(history[0].ProviderIDs) != 2 || history[0].ResultCount != 3 {
		t.Fatalf("newest history = %#v", history[0])
	}
	other, err := dataStore.ListMatchQueryHistory(context.Background(), "trk-2", 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("other track history = %#v err=%v", other, err)
	}
}

func TestRevisionStoresDeduplicatedArtworkBlobAndHydratesOnRead(t *testing.T) {
	dataStore := openTestStore(t)
	snapshot := &domain.ArtworkSnapshot{MIME: "image/png", Format: "PNG", Width: 2, Height: 2, Size: 5, Hash: "art-hash", Data: []byte("image")}
	base := domain.Revision{LibraryID: "lib", TrackID: "track", TrackTitle: "Song", FileName: "song.mp3", Action: "替换封面", Source: "测试", BaseRevision: "r1", ResultRevision: "r2", Diff: []domain.RevisionDiff{{Field: "artwork", Operation: domain.OperationSet}}, CoverTone: domain.CoverMoss, BeforeArtwork: snapshot}
	first, err := dataStore.CreateRevision(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	base.ID = "revision-two"
	base.BaseRevision, base.ResultRevision = "r2", "r3"
	base.AfterArtwork = snapshot
	second, err := dataStore.CreateRevision(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := dataStore.db.QueryRow(`SELECT COUNT(*) FROM artwork_blobs WHERE hash=?`, snapshot.Hash).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("artwork blob count = %d", count)
	}
	loaded, err := dataStore.Revision(context.Background(), first.ID)
	if err != nil || loaded.BeforeArtwork == nil || string(loaded.BeforeArtwork.Data) != "image" {
		t.Fatalf("loaded first revision = %#v err=%v", loaded, err)
	}
	loaded, err = dataStore.Revision(context.Background(), second.ID)
	if err != nil || loaded.AfterArtwork == nil || string(loaded.AfterArtwork.Data) != "image" {
		t.Fatalf("loaded second revision = %#v err=%v", loaded, err)
	}
}

func TestBatchEditItemsPersistIndependentResults(t *testing.T) {
	dataStore := openTestStore(t)
	if err := dataStore.UpsertBatchEditItem(context.Background(), BatchEditItem{JobID: "job-1", TrackID: "track-1", State: "failed", Error: "revision conflict", Diff: []byte(`[]`)}); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.UpsertBatchEditItem(context.Background(), BatchEditItem{JobID: "job-1", TrackID: "track-2", State: "written", Diff: []byte(`[{"field":"genres"}]`)}); err != nil {
		t.Fatal(err)
	}
	items, err := dataStore.ListBatchEditItems(context.Background(), "job-1")
	if err != nil || len(items) != 2 || items[0].State != "failed" || items[1].State != "written" {
		t.Fatalf("batch edit items = %#v err=%v", items, err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dataStore, err := Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	return dataStore
}

func testScanResult(libraryID, name string, tracks ...domain.Track) scanner.Result {
	return scanner.Result{
		Library: domain.LibrarySummary{
			ID: libraryID, Name: name, RootLabel: "archive", TrackCount: len(tracks), FolderCount: 1,
			Writable: true, LastScanLabel: "2026-08-20 12:00", Folders: []domain.FolderNode{},
		},
		Tracks: tracks,
		Report: domain.ScanReport{
			StartedAt: "2026-08-20T12:00:00Z", CompletedAt: "2026-08-20T12:00:01Z",
			Discovered: len(tracks), Parsed: len(tracks),
		},
	}
}

func testTrack(id, relativePath string) domain.Track {
	format := domain.TrackFormat(filepath.Ext(relativePath)[1:])
	return domain.Track{
		ID: id, FileName: filepath.Base(relativePath), RelativePath: relativePath, FolderID: "folder-root",
		Format: format, Title: id, Artists: []string{"Artist"}, Album: "Album", AlbumArtists: []string{},
		Genres: []string{}, CoverTone: domain.CoverMoss, Health: domain.HealthComplete,
		Writable: true, Revision: "revision-" + id, ModifiedAt: "2026-08-20T12:00:00Z", SyncState: domain.SyncIndexed,
	}
}
