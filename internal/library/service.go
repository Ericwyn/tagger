package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/scanner"
)

var ErrTrackNotFound = errors.New("track not found")

type FileRef struct {
	ID           string
	RelativePath string
	AbsolutePath string
	Revision     string
	Format       domain.TrackFormat
}

type TrackFilter struct {
	FolderID string
	Query    string
	Health   domain.TrackHealth
	Format   domain.TrackFormat
}

type Repository interface {
	SaveScan(ctx context.Context, root string, result scanner.Result) error
	LoadScan(ctx context.Context, root string) (scanner.Result, bool, error)
}

type DeltaRepository interface {
	SaveScanDelta(ctx context.Context, root string, result scanner.Result, changed []domain.Track) error
	SaveTrackUpdates(ctx context.Context, root string, result scanner.Result, tracks []domain.Track) error
}

type RootRepository interface {
	SetLibraryRoot(context.Context, string) error
}

type Service struct {
	scanner *scanner.Scanner
	repo    Repository

	scanMu sync.Mutex
	mu     sync.RWMutex

	library domain.LibrarySummary
	tracks  []domain.Track
	byID    map[string]domain.Track
	report  domain.ScanReport

	orderedMu    sync.RWMutex
	ordered      map[TrackSort][]domain.Track
	orderVersion uint64
}

func New(ctx context.Context, scanner *scanner.Scanner, repositories ...Repository) (*Service, error) {
	service := &Service{scanner: scanner, ordered: make(map[TrackSort][]domain.Track), orderVersion: 1}
	if len(repositories) > 0 {
		service.repo = repositories[0]
	}
	if service.repo != nil {
		result, found, err := service.repo.LoadScan(ctx, scanner.Root())
		if err != nil {
			return nil, fmt.Errorf("load persisted library: %w", err)
		}
		if found {
			if result.Library.RootPath == "" {
				result.Library.RootPath = scanner.Root()
			}
			service.apply(result)
			return service, nil
		}
	}
	if err := service.Rescan(ctx); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *Service) Rescan(ctx context.Context) error {
	return s.rescan(ctx, scanner.ScanFull, nil)
}

func (s *Service) QuickScan(ctx context.Context, targets []string) error {
	return s.rescan(ctx, scanner.ScanQuick, targets)
}

func (s *Service) rescan(ctx context.Context, mode scanner.ScanMode, targets []string) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	currentScanner := s.currentScanner()
	var result scanner.Result
	var err error
	s.mu.RLock()
	existing := cloneTracks(s.tracks)
	s.mu.RUnlock()
	if mode == scanner.ScanFull {
		result, err = currentScanner.Scan(ctx)
	} else {
		result, err = currentScanner.ScanIncremental(ctx, existing, targets)
	}
	if err != nil {
		return err
	}
	if s.repo != nil {
		if mode != scanner.ScanFull {
			if deltaRepo, ok := s.repo.(DeltaRepository); ok {
				if err := deltaRepo.SaveScanDelta(ctx, currentScanner.Root(), result, changedTracks(existing, result.Tracks)); err != nil {
					return fmt.Errorf("persist incremental scan: %w", err)
				}
			} else if err := s.repo.SaveScan(ctx, currentScanner.Root(), result); err != nil {
				return fmt.Errorf("persist scan: %w", err)
			}
		} else if err := s.repo.SaveScan(ctx, currentScanner.Root(), result); err != nil {
			return fmt.Errorf("persist scan: %w", err)
		}
	}
	s.apply(result)
	return nil
}

// RescanTrack refreshes one indexed file in place. It deliberately does not
// walk or parse the rest of the library, which keeps post-write refreshes and
// manual diagnostics responsive for large collections.
func (s *Service) RescanTrack(ctx context.Context, id string) (domain.Track, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	current, err := s.Track(id)
	if err != nil {
		return domain.Track{}, err
	}
	currentScanner := s.currentScanner()
	track, scanErr := currentScanner.ScanTrack(ctx, current.RelativePath)
	if track.ID == "" {
		return domain.Track{}, scanErr
	}
	s.applyTrack(track)
	if s.repo != nil {
		if deltaRepo, ok := s.repo.(DeltaRepository); ok {
			if persistErr := deltaRepo.SaveTrackUpdates(ctx, currentScanner.Root(), s.snapshotResult(), []domain.Track{track}); persistErr != nil {
				return track, fmt.Errorf("persist track scan: %w", persistErr)
			}
		} else if persistErr := s.repo.SaveScan(ctx, currentScanner.Root(), s.snapshotResult()); persistErr != nil {
			return track, fmt.Errorf("persist track scan: %w", persistErr)
		}
	}
	return track, scanErr
}

func (s *Service) RescanTracks(ctx context.Context, ids []string) ([]domain.Track, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	currentScanner := s.currentScanner()
	tracks := make([]domain.Track, 0, len(ids))
	for _, id := range ids {
		current, err := s.Track(id)
		if err != nil {
			return nil, err
		}
		track, scanErr := currentScanner.ScanTrack(ctx, current.RelativePath)
		if scanErr != nil && track.ID == "" {
			return nil, scanErr
		}
		tracks = append(tracks, track)
		s.applyTrack(track)
	}
	if s.repo != nil && len(tracks) > 0 {
		if deltaRepo, ok := s.repo.(DeltaRepository); ok {
			if err := deltaRepo.SaveTrackUpdates(ctx, currentScanner.Root(), s.snapshotResult(), tracks); err != nil {
				return nil, fmt.Errorf("persist track scans: %w", err)
			}
		} else if err := s.repo.SaveScan(ctx, currentScanner.Root(), s.snapshotResult()); err != nil {
			return nil, fmt.Errorf("persist track scans: %w", err)
		}
	}
	return tracks, nil
}

func (s *Service) RemoveMissing() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]domain.Track, 0, len(s.tracks))
	for _, track := range s.tracks {
		if !track.Missing {
			kept = append(kept, track)
		}
	}
	removed := len(s.tracks) - len(kept)
	if removed == 0 {
		return 0
	}
	s.tracks = kept
	s.byID = make(map[string]domain.Track, len(kept))
	for _, track := range kept {
		s.byID[track.ID] = cloneTrack(track)
	}
	s.library.TrackCount = len(kept)
	s.library.FolderCount = len(buildFoldersForTracks(kept))
	s.library.Folders = buildFoldersForTracks(kept)
	s.report.Missing = 0
	s.report.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	s.invalidateTrackOrder()
	return removed
}

// SwitchRoot scans or restores a candidate root completely before replacing
// the active scanner. A failed probe/scan leaves the current library intact.
func (s *Service) SwitchRoot(ctx context.Context, root string) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	currentScanner := s.currentScanner()
	nextScanner, err := currentScanner.WithRoot(root)
	if err != nil {
		return err
	}
	var result scanner.Result
	found := false
	if s.repo != nil {
		result, found, err = s.repo.LoadScan(ctx, nextScanner.Root())
		if err != nil {
			return fmt.Errorf("load switched library: %w", err)
		}
	}
	if !found {
		result, err = nextScanner.Scan(ctx)
		if err != nil {
			return err
		}
		if s.repo != nil {
			if err := s.repo.SaveScan(ctx, nextScanner.Root(), result); err != nil {
				return fmt.Errorf("persist switched library: %w", err)
			}
		}
	}
	if result.Library.RootPath == "" {
		result.Library.RootPath = nextScanner.Root()
	}
	if rootRepository, ok := s.repo.(RootRepository); ok {
		if err := rootRepository.SetLibraryRoot(ctx, nextScanner.Root()); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.scanner = nextScanner
	s.mu.Unlock()
	s.apply(result)
	return nil
}

func (s *Service) apply(result scanner.Result) {
	byID := make(map[string]domain.Track, len(result.Tracks))
	for _, track := range result.Tracks {
		byID[track.ID] = cloneTrack(track)
	}

	s.mu.Lock()
	s.library = cloneLibrary(result.Library)
	s.tracks = cloneTracks(result.Tracks)
	s.byID = byID
	s.report = result.Report
	s.mu.Unlock()
	s.invalidateTrackOrder()
}

func (s *Service) applyTrack(track domain.Track) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		s.byID = make(map[string]domain.Track)
	}
	clone := cloneTrack(track)
	s.byID[track.ID] = clone
	for index := range s.tracks {
		if s.tracks[index].ID == track.ID {
			s.tracks[index] = clone
			break
		}
	}
	now := time.Now().UTC()
	s.library.LastScanLabel = now.Format("2006-01-02 15:04")
	s.report.CompletedAt = now.Format(time.RFC3339)
	s.invalidateTrackOrder()
}

func (s *Service) snapshotResult() scanner.Result {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return scanner.Result{Library: cloneLibrary(s.library), Tracks: cloneTracks(s.tracks), Report: s.report}
}

func (s *Service) Library() domain.LibrarySummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneLibrary(s.library)
}

func (s *Service) LastReport() domain.ScanReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	report := s.report
	report.Warnings = append([]string(nil), report.Warnings...)
	return report
}

func (s *Service) ListTracks(filter TrackFilter) []domain.Track {
	s.mu.RLock()
	defer s.mu.RUnlock()
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	result := make([]domain.Track, 0, len(s.tracks))
	for _, track := range s.tracks {
		if filter.FolderID != "" && track.FolderID != filter.FolderID {
			continue
		}
		if filter.Health != "" && track.Health != filter.Health {
			continue
		}
		if filter.Format != "" && track.Format != filter.Format {
			continue
		}
		if query != "" && !trackMatches(track, query) {
			continue
		}
		result = append(result, cloneTrack(track))
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].RelativePath < result[j].RelativePath })
	return result
}

// ListTrackPage returns a stable, server-side filtered page. The cursor is
// intentionally opaque so callers cannot depend on the in-memory ordering
// implementation.
func (s *Service) ListTrackPage(query TrackQuery, cursor string, limit int) (TrackPage, error) {
	normalized, err := NormalizeTrackQuery(query)
	if err != nil {
		return TrackPage{}, err
	}
	pageSize, err := normalizePageSize(limit)
	if err != nil {
		return TrackPage{}, err
	}
	ordered, generation := s.orderedTracks(normalized.Sort)
	offset := 0
	if strings.TrimSpace(cursor) != "" {
		state, decodeErr := decodeTrackCursor(cursor)
		if decodeErr != nil {
			return TrackPage{}, decodeErr
		}
		if state.Generation != generation {
			return TrackPage{}, ErrStaleTrackCursor
		}
		if state.QueryHash != queryHash(normalized) {
			return TrackPage{}, fmt.Errorf("%w: query changed", ErrInvalidTrackCursor)
		}
		offset = state.Offset
	}

	page := make([]domain.Track, 0, pageSize)
	total, matched := 0, 0
	for _, track := range ordered {
		if !trackMatchesQuery(track, normalized) {
			continue
		}
		total++
		if matched < offset {
			matched++
			continue
		}
		if len(page) < pageSize {
			page = append(page, cloneTrack(track))
		}
		matched++
	}
	if offset > total {
		return TrackPage{}, fmt.Errorf("%w: offset exceeds result", ErrInvalidTrackCursor)
	}
	result := TrackPage{Tracks: page, Total: total}
	consumed := offset + len(page)
	if consumed < total {
		result.HasMore = true
		result.NextCursor = encodeTrackCursor(trackCursor{Generation: generation, QueryHash: queryHash(normalized), Offset: consumed})
	}
	return result, nil
}

func (s *Service) ResolveTracksByIDs(ids []string) ([]domain.Track, error) {
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, found := seen[id]; found {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return []domain.Track{}, nil
	}
	if len(unique) > MaxTrackResolveSize {
		return nil, ErrTrackSelectionLarge
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]domain.Track, 0, len(unique))
	for _, id := range unique {
		track, found := s.byID[id]
		if !found {
			return nil, fmt.Errorf("%w: %s", ErrTrackNotFound, id)
		}
		result = append(result, cloneTrack(track))
	}
	return result, nil
}

func (s *Service) ResolveTracksByQuery(query TrackQuery) ([]domain.Track, int, error) {
	normalized, err := NormalizeTrackQuery(query)
	if err != nil {
		return nil, 0, err
	}
	ordered, _ := s.orderedTracks(normalized.Sort)
	result := make([]domain.Track, 0)
	total := 0
	for _, track := range ordered {
		if !trackMatchesQuery(track, normalized) {
			continue
		}
		total++
		if total > MaxTrackResolveSize {
			return nil, total, ErrTrackSelectionLarge
		}
		result = append(result, cloneTrack(track))
	}
	return result, total, nil
}

func (s *Service) orderedTracks(mode TrackSort) ([]domain.Track, uint64) {
	s.orderedMu.RLock()
	if tracks, found := s.ordered[mode]; found {
		generation := s.orderVersion
		s.orderedMu.RUnlock()
		return tracks, generation
	}
	generation := s.orderVersion
	s.orderedMu.RUnlock()

	s.mu.RLock()
	snapshot := cloneTracks(s.tracks)
	s.mu.RUnlock()
	sortTracks(snapshot, mode)

	s.orderedMu.Lock()
	if generation == s.orderVersion {
		s.ordered[mode] = snapshot
	}
	s.orderedMu.Unlock()
	return snapshot, generation
}

func (s *Service) invalidateTrackOrder() {
	s.orderedMu.Lock()
	s.ordered = make(map[TrackSort][]domain.Track)
	s.orderVersion++
	if s.orderVersion == 0 {
		s.orderVersion = 1
	}
	s.orderedMu.Unlock()
}

func (s *Service) Track(id string) (domain.Track, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	track, ok := s.byID[id]
	if !ok {
		return domain.Track{}, ErrTrackNotFound
	}
	return cloneTrack(track), nil
}

func (s *Service) FileRef(id string) (FileRef, error) {
	track, err := s.Track(id)
	if err != nil {
		return FileRef{}, err
	}
	root := s.Root()
	return FileRef{
		ID:           track.ID,
		RelativePath: track.RelativePath,
		AbsolutePath: filepath.Join(root, filepath.FromSlash(track.RelativePath)),
		Revision:     track.Revision,
		Format:       track.Format,
	}, nil
}

func (s *Service) Root() string { return s.currentScanner().Root() }

func (s *Service) currentScanner() *scanner.Scanner {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanner
}

func trackMatches(track domain.Track, query string) bool {
	values := []string{track.Title, track.FileName, track.RelativePath, track.Album}
	values = append(values, track.Artists...)
	values = append(values, track.AlbumArtists...)
	values = append(values, track.Genres...)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func cloneTracks(tracks []domain.Track) []domain.Track {
	result := make([]domain.Track, len(tracks))
	for i, track := range tracks {
		result[i] = cloneTrack(track)
	}
	return result
}

func cloneTrack(track domain.Track) domain.Track {
	track.Artists = cloneStrings(track.Artists)
	track.AlbumArtists = cloneStrings(track.AlbumArtists)
	track.Genres = cloneStrings(track.Genres)
	if track.LyricsSidecar != nil {
		sidecar := *track.LyricsSidecar
		track.LyricsSidecar = &sidecar
	}
	return track
}

func cloneStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func cloneLibrary(library domain.LibrarySummary) domain.LibrarySummary {
	library.Folders = append([]domain.FolderNode(nil), library.Folders...)
	for i := range library.Folders {
		library.Folders[i].Children = append([]domain.FolderNode(nil), library.Folders[i].Children...)
	}
	return library
}

func changedTracks(previous []domain.Track, current []domain.Track) []domain.Track {
	old := make(map[string]domain.Track, len(previous))
	for _, track := range previous {
		old[track.RelativePath] = track
	}
	changed := make([]domain.Track, 0)
	for _, track := range current {
		prior, found := old[track.RelativePath]
		if !found || prior.Revision != track.Revision || prior.Missing != track.Missing || prior.Health != track.Health {
			changed = append(changed, track)
		}
	}
	return changed
}

func buildFoldersForTracks(tracks []domain.Track) []domain.FolderNode {
	// Scanner owns the folder normalization; this small helper preserves the
	// current in-memory summary after an explicit missing-index purge.
	counts := make(map[string]domain.FolderNode)
	for _, track := range tracks {
		folder := counts[track.FolderID]
		folder.ID = track.FolderID
		folder.Count++
		if folder.Name == "" {
			directory := filepath.ToSlash(filepath.Dir(track.RelativePath))
			folder.Name = "根目录单曲"
			if directory != "." {
				folder.Name = strings.ReplaceAll(directory, "/", " · ")
			}
		}
		counts[track.FolderID] = folder
	}
	result := make([]domain.FolderNode, 0, len(counts))
	for _, folder := range counts {
		result = append(result, folder)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
