package library

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/scanner"
)

var ErrTrackNotFound = errors.New("track not found")

type TrackFilter struct {
	FolderID string
	Query    string
	Health   domain.TrackHealth
	Format   domain.TrackFormat
}

type Service struct {
	scanner *scanner.Scanner

	scanMu  sync.Mutex
	mu      sync.RWMutex
	library domain.LibrarySummary
	tracks  []domain.Track
	byID    map[string]domain.Track
	report  domain.ScanReport
}

func New(ctx context.Context, scanner *scanner.Scanner) (*Service, error) {
	service := &Service{scanner: scanner}
	if err := service.Rescan(ctx); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *Service) Rescan(ctx context.Context) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	result, err := s.scanner.Scan(ctx)
	if err != nil {
		return err
	}
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
	return nil
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
