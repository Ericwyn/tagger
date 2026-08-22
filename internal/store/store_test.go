package store

import (
	"context"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/scanner"
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

func TestSaveLoadAndReplaceScan(t *testing.T) {
	dataStore := openTestStore(t)
	root := "/music/archive"
	first := testScanResult("lib-1", "Archive", testTrack("trk-1", "one.mp3"), testTrack("trk-2", "two.flac"))
	if err := dataStore.SaveScan(context.Background(), root, first); err != nil {
		t.Fatal(err)
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
		Writable: true, Revision: "revision-" + id, ModifiedAt: "2026-08-20T12:00:00Z",
	}
}
