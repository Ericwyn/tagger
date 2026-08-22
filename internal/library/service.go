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

type RootRepository interface {
	SetLibraryRoot(context.Context, string) error
}

type Service struct {
	scanner *scanner.Scanner
	repo    Repository

	scanMu  sync.Mutex
	mu      sync.RWMutex
	library domain.LibrarySummary
	tracks  []domain.Track
	byID    map[string]domain.Track
	report  domain.ScanReport
}

func New(ctx context.Context, scanner *scanner.Scanner, repositories ...Repository) (*Service, error) {
	service := &Service{scanner: scanner}
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
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	currentScanner := s.currentScanner()
	result, err := currentScanner.Scan(ctx)
	if err != nil {
		return err
	}
	if s.repo != nil {
		if err := s.repo.SaveScan(ctx, currentScanner.Root(), result); err != nil {
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
		if persistErr := s.repo.SaveScan(ctx, currentScanner.Root(), s.snapshotResult()); persistErr != nil {
			return track, fmt.Errorf("persist track scan: %w", persistErr)
		}
	}
	return track, scanErr
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
