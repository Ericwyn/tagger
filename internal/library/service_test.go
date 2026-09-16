package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/store"
	"github.com/ericwyn/tagger/internal/tags"
)

type serviceEngine struct{}

type countingServiceEngine struct{ reads atomic.Int32 }

type memoryServiceRepository struct {
	scans      map[string]scanner.Result
	savedRoots []string
	activeRoot string
}

func (repo *memoryServiceRepository) LoadScan(_ context.Context, root string) (scanner.Result, bool, error) {
	result, found := repo.scans[root]
	return result, found, nil
}

func (repo *memoryServiceRepository) SaveScan(_ context.Context, root string, result scanner.Result) error {
	if repo.scans == nil {
		repo.scans = make(map[string]scanner.Result)
	}
	repo.scans[root] = result
	repo.savedRoots = append(repo.savedRoots, root)
	return nil
}

func (repo *memoryServiceRepository) SetLibraryRoot(_ context.Context, root string) error {
	repo.activeRoot = root
	return nil
}

func (engine *countingServiceEngine) Read(ctx context.Context, path string) (tags.Snapshot, error) {
	engine.reads.Add(1)
	return serviceEngine{}.Read(ctx, path)
}

func (*countingServiceEngine) Write(context.Context, string, map[string][]string) error { return nil }

func (*countingServiceEngine) Version() string { return "counting-test" }

func (serviceEngine) Read(_ context.Context, path string) (tags.Snapshot, error) {
	name := filepath.Base(path)
	return tags.Snapshot{
		Raw: map[string][]string{
			"TITLE":  {name[:len(name)-len(filepath.Ext(name))]},
			"ARTIST": {"测试歌手"},
		},
		DurationSeconds: 120,
		ArtworkCount:    1,
	}, nil
}

func (serviceEngine) Write(context.Context, string, map[string][]string) error { return nil }

func (serviceEngine) Version() string { return "test" }

func TestServiceFiltersAndReturnsDefensiveCopies(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Alpha.mp3", "Beta.flac"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	musicScanner, err := scanner.New(serviceEngine{}, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), musicScanner)
	if err != nil {
		t.Fatal(err)
	}
	flac := service.ListTracks(TrackFilter{Format: domain.FormatFLAC})
	if len(flac) != 1 || flac[0].Title != "Beta" {
		t.Fatalf("flac filter = %#v", flac)
	}
	search := service.ListTracks(TrackFilter{Query: "alpha"})
	if len(search) != 1 || search[0].Title != "Alpha" {
		t.Fatalf("search = %#v", search)
	}

	all := service.ListTracks(TrackFilter{})
	all[0].Artists[0] = "mutated"
	again := service.ListTracks(TrackFilter{})
	if again[0].Artists[0] != "测试歌手" {
		t.Fatalf("service leaked mutable state: %#v", again[0])
	}
	if _, err := service.Track("missing"); err != ErrTrackNotFound {
		t.Fatalf("missing error = %v", err)
	}
}

func TestNormalizeTrackQueryAcceptsOgg(t *testing.T) {
	query, err := NormalizeTrackQuery(TrackQuery{Format: domain.FormatOGG})
	if err != nil || query.Format != domain.FormatOGG {
		t.Fatalf("normalize OGG query = %#v, %v", query, err)
	}
	if _, err := NormalizeTrackQuery(TrackQuery{Format: "aac"}); !errors.Is(err, ErrInvalidTrackQuery) {
		t.Fatalf("unsupported format error = %v", err)
	}
}

func TestServiceLoadsPersistedIndexAndRescansOnDemand(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Persisted.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	engine := &countingServiceEngine{}
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	dataStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })

	first, err := New(context.Background(), musicScanner, dataStore)
	if err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 1 || first.Library().TrackCount != 1 {
		t.Fatalf("initial scan reads=%d tracks=%d", engine.reads.Load(), first.Library().TrackCount)
	}
	if first.Library().RootPath != root {
		t.Fatalf("initial root path=%q, want %q", first.Library().RootPath, root)
	}
	second, err := New(context.Background(), musicScanner, dataStore)
	if err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 1 || second.Library().TrackCount != 1 {
		t.Fatalf("persisted load unexpectedly rescanned: reads=%d tracks=%d", engine.reads.Load(), second.Library().TrackCount)
	}
	if second.Library().RootPath != root {
		t.Fatalf("persisted root path=%q, want %q", second.Library().RootPath, root)
	}
	if err := second.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 2 {
		t.Fatalf("explicit rescan reads=%d, want 2", engine.reads.Load())
	}
}

func TestServiceRecoversPersistedDraftProjectionWithUntargetedQuickScan(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Recovered.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	engine := &countingServiceEngine{}
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryServiceRepository{scans: map[string]scanner.Result{
		root: {
			Library: domain.LibrarySummary{ID: "persisted-library", Name: "Persisted", RootPath: root, TrackCount: 1},
			Tracks: []domain.Track{{
				ID: "trk-persisted", FileName: "Recovered.mp3", RelativePath: "Recovered.mp3", Format: domain.FormatMP3,
				Title: "stale projection", SyncState: domain.SyncDraft,
			}},
		},
	}}
	service, err := New(context.Background(), musicScanner, repo)
	if err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 0 || len(service.PendingPaths()) != 1 {
		t.Fatalf("loaded draft reads=%d pending=%v", engine.reads.Load(), service.PendingPaths())
	}
	if err := service.QuickScan(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	tracks := service.ListTracks(TrackFilter{})
	if engine.reads.Load() != 1 || len(tracks) != 1 || tracks[0].Title != "Recovered" || tracks[0].SyncState != domain.SyncIndexed || len(service.PendingPaths()) != 0 {
		t.Fatalf("recovered tracks=%#v reads=%d pending=%v", tracks, engine.reads.Load(), service.PendingPaths())
	}
}

func TestServiceRescanTrackOnlyReadsRequestedFile(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"First.mp3", "Second.flac"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	engine := &countingServiceEngine{}
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), musicScanner)
	if err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 2 {
		t.Fatalf("initial reads = %d, want 2", engine.reads.Load())
	}
	tracks := service.ListTracks(TrackFilter{})
	if len(tracks) != 2 {
		t.Fatalf("tracks = %#v", tracks)
	}
	updated, err := service.RescanTrack(context.Background(), tracks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != tracks[0].ID || engine.reads.Load() != 3 {
		t.Fatalf("single scan updated=%#v reads=%d, want one additional read", updated, engine.reads.Load())
	}
}

func TestServiceQuickScanSkipsUnchangedFilesAndMarksMissing(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "First.mp3")
	secondPath := filepath.Join(root, "Second.flac")
	if err := os.WriteFile(firstPath, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := &countingServiceEngine{}
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), musicScanner)
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.reads.Load(); got != 2 {
		t.Fatalf("initial reads=%d, want 2", got)
	}
	if err := service.QuickScan(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := engine.reads.Load(); got != 2 {
		t.Fatalf("unchanged quick scan reads=%d, want 2", got)
	}
	if err := os.WriteFile(firstPath, []byte("first changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.QuickScan(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := engine.reads.Load(); got != 3 {
		t.Fatalf("changed quick scan reads=%d, want 3", got)
	}
	if err := os.Remove(secondPath); err != nil {
		t.Fatal(err)
	}
	if err := service.QuickScan(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	missing := service.ListTracks(TrackFilter{Health: domain.HealthMissing})
	if len(missing) != 1 || missing[0].RelativePath != "Second.flac" {
		t.Fatalf("missing tracks=%#v", missing)
	}
}

func TestReconcileDirectoryExposesDraftBeforeMetadataScan(t *testing.T) {
	root := t.TempDir()
	engine := &countingServiceEngine{}
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), musicScanner)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := service.SubscribeEvents()
	defer unsubscribe()
	<-events // initial recoverable snapshot
	album := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(album, "New.flac")
	if err := os.WriteFile(path, []byte("new audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := service.ReconcileDirectory(context.Background(), "Artist")
	if err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 0 {
		t.Fatalf("filesystem reconcile read tags %d times", engine.reads.Load())
	}
	if !result.Changed || len(result.Pending) != 1 || result.Pending[0] != "Artist/Album/New.flac" {
		t.Fatalf("reconcile result=%#v", result)
	}
	select {
	case event := <-events:
		if event.Kind != EventInventory || event.Generation != result.Generation {
			t.Fatalf("inventory event=%#v result=%#v", event, result)
		}
	default:
		t.Fatal("inventory event was not published")
	}
	page, err := service.ListTrackPage(TrackQuery{}, "", 100)
	if err != nil || len(page.Tracks) != 1 || page.Tracks[0].SyncState != domain.SyncDraft {
		t.Fatalf("draft page=%#v err=%v", page, err)
	}
	if _, err := service.FileRef(page.Tracks[0].ID); err != ErrTrackNotIndexed {
		t.Fatalf("draft file ref error=%v", err)
	}
	if err := service.QuickScan(context.Background(), result.Pending); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Kind != EventMetadata {
			t.Fatalf("metadata event=%#v", event)
		}
	default:
		t.Fatal("metadata event was not published")
	}
	indexed, err := service.Track(page.Tracks[0].ID)
	if err != nil || indexed.SyncState != domain.SyncIndexed || engine.reads.Load() != 1 {
		t.Fatalf("indexed track=%#v reads=%d err=%v", indexed, engine.reads.Load(), err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReconcileDirectory(context.Background(), "Artist/Album"); err != nil {
		t.Fatal(err)
	}
	visible, _ := service.ListTrackPage(TrackQuery{}, "", 100)
	missing, _ := service.ListTrackPage(TrackQuery{Health: domain.HealthMissing}, "", 100)
	if visible.Total != 0 || missing.Total != 1 {
		t.Fatalf("visible=%#v missing=%#v", visible, missing)
	}
}

func TestServiceTrackPagesAreStableAndCursorBecomesStaleAfterRefresh(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"01.mp3", "02.mp3", "03.mp3", "04.mp3", "05.mp3"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	musicScanner, err := scanner.New(serviceEngine{}, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), musicScanner)
	if err != nil {
		t.Fatal(err)
	}
	query := TrackQuery{Sort: TrackSortTitle}
	first, err := service.ListTrackPage(query, "", 2)
	if err != nil || first.Total != 5 || len(first.Tracks) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	second, err := service.ListTrackPage(query, first.NextCursor, 2)
	if err != nil || len(second.Tracks) != 2 || !second.HasMore {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
	third, err := service.ListTrackPage(query, second.NextCursor, 2)
	if err != nil || len(third.Tracks) != 1 || third.HasMore {
		t.Fatalf("third page=%#v err=%v", third, err)
	}
	seen := make(map[string]struct{}, 5)
	for _, page := range [][]domain.Track{first.Tracks, second.Tracks, third.Tracks} {
		for _, track := range page {
			if _, found := seen[track.ID]; found {
				t.Fatalf("duplicate track across pages: %s", track.ID)
			}
			seen[track.ID] = struct{}{}
		}
	}
	if len(seen) != 5 {
		t.Fatalf("seen=%d, want 5", len(seen))
	}
	if err := service.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListTrackPage(query, first.NextCursor, 2); err != ErrStaleTrackCursor {
		t.Fatalf("stale cursor error=%v, want %v", err, ErrStaleTrackCursor)
	}
}

func TestServiceTrackPagesCombineFolderHealthFormatAndSearchFilters(t *testing.T) {
	trackNumber := 1
	tracks := []domain.Track{
		{ID: "album-1", FileName: "one.flac", RelativePath: "Artist/Album/one.flac", FolderID: "folder-album", Format: domain.FormatFLAC, Title: "One", Album: "Album", Health: domain.HealthMissingLyrics, TrackNumber: &trackNumber},
		{ID: "album-2", FileName: "two.mp3", RelativePath: "Artist/Album/two.mp3", FolderID: "folder-album", Format: domain.FormatMP3, Title: "Two", Album: "Album", Health: domain.HealthComplete, TrackNumber: &trackNumber},
		{ID: "disc-2", FileName: "three.flac", RelativePath: "Artist/Album/Disc 2/three.flac", FolderID: "folder-disc", Format: domain.FormatFLAC, Title: "Three", Album: "Album", Health: domain.HealthComplete, TrackNumber: &trackNumber},
		{ID: "other", FileName: "four.wav", RelativePath: "Other/four.wav", FolderID: "folder-other", Format: domain.FormatWAV, Title: "Four", Album: "Other", Health: domain.HealthComplete, TrackNumber: &trackNumber},
	}
	service := &Service{ordered: make(map[TrackSort][]domain.Track), orderVersion: 1}
	service.apply(scanner.Result{Library: domain.LibrarySummary{ID: "library"}, Tracks: tracks})

	assertPageTotal := func(query TrackQuery, want int) {
		t.Helper()
		page, err := service.ListTrackPage(query, "", 20)
		if err != nil || page.Total != want || len(page.Tracks) != want {
			t.Fatalf("query %#v page=%#v err=%v, want %d", query, page, err, want)
		}
	}
	assertPageTotal(TrackQuery{FolderID: "folder-album"}, 2)
	assertPageTotal(TrackQuery{FolderPath: "Artist · Album", IncludeSubfolders: true}, 3)
	assertPageTotal(TrackQuery{FolderID: "folder-album", Health: domain.HealthMissingLyrics}, 1)
	assertPageTotal(TrackQuery{Format: domain.FormatFLAC, Query: "three"}, 1)
}

func TestServiceResolveTracksByQueryEnforcesBatchLimit(t *testing.T) {
	const limit = 2000
	root := t.TempDir()
	for index := 0; index < limit+1; index++ {
		name := filepath.Join(root, fmt.Sprintf("%04d.mp3", index))
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	musicScanner, err := scanner.New(serviceEngine{}, scanner.Options{Root: root, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), musicScanner)
	if err != nil {
		t.Fatal(err)
	}
	_, total, err := service.ResolveTracksByQuery(TrackQuery{Sort: TrackSortTitle}, limit)
	if err != ErrTrackSelectionLarge || total != limit+1 {
		t.Fatalf("resolve total=%d err=%v, want limit error at %d", total, err, limit+1)
	}
}

func TestServiceSwitchRootScansBeforeReplacingActiveIndex(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "First.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "Next.flac"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dataStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	firstScanner, err := scanner.New(serviceEngine{}, scanner.Options{Root: first, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), firstScanner, dataStore)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SwitchRoot(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if service.Root() != second || service.Library().TrackCount != 1 {
		t.Fatalf("switched root=%q library=%#v", service.Root(), service.Library())
	}
	tracks := service.ListTracks(TrackFilter{})
	if len(tracks) != 1 || tracks[0].FileName != "Next.flac" {
		t.Fatalf("switched tracks = %#v", tracks)
	}
	persisted, found, err := dataStore.LibraryRoot(context.Background())
	if err != nil || !found || persisted != second {
		t.Fatalf("persisted root=%q found=%v err=%v", persisted, found, err)
	}
}

func TestServiceSwitchRootRebuildsCachedDraftProjectionBeforeActivation(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "First.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "Next.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	engine := &countingServiceEngine{}
	firstScanner, err := scanner.New(engine, scanner.Options{Root: first, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryServiceRepository{scans: make(map[string]scanner.Result)}
	service, err := New(context.Background(), firstScanner, repo)
	if err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 1 {
		t.Fatalf("initial reads=%d, want 1", engine.reads.Load())
	}
	repo.scans[second] = scanner.Result{
		Library: domain.LibrarySummary{ID: "cached-library", Name: "Cached", RootPath: second, TrackCount: 1},
		Tracks: []domain.Track{{
			ID: "trk-cached", FileName: "Next.mp3", RelativePath: "Next.mp3", Format: domain.FormatMP3,
			Title: "stale projection", SyncState: domain.SyncDraft,
		}},
	}

	if err := service.SwitchRoot(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	tracks := service.ListTracks(TrackFilter{})
	if engine.reads.Load() != 2 || len(tracks) != 1 || tracks[0].Title != "Next" || tracks[0].SyncState != domain.SyncIndexed {
		t.Fatalf("rebuilt tracks=%#v reads=%d", tracks, engine.reads.Load())
	}
	persisted := repo.scans[second]
	if len(persisted.Tracks) != 1 || persisted.Tracks[0].SyncState != domain.SyncIndexed || persisted.Tracks[0].Title != "Next" {
		t.Fatalf("persisted rebuild=%#v", persisted.Tracks)
	}
	if repo.activeRoot != second {
		t.Fatalf("active root=%q, want %q", repo.activeRoot, second)
	}
}
