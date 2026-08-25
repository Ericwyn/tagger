package library

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/scanner"
)

type ReconcileResult struct {
	Generation uint64   `json:"generation"`
	Changed    bool     `json:"changed"`
	Pending    []string `json:"pending,omitempty"`
	Missing    int      `json:"missing"`
	Warnings   []string `json:"warnings,omitempty"`
}

// ReconcileDirectory refreshes the current directory and its direct children.
// It never reads embedded tags. New or changed files are exposed immediately as
// drafts and returned in Pending for asynchronous targeted metadata scanning.
func (s *Service) ReconcileDirectory(ctx context.Context, folderPath string) (ReconcileResult, error) {
	return s.reconcileInventory(ctx, []string{normalizeFolderPath(folderPath)}, 1)
}

// ReconcileTargets is the fsnotify path: event targets are already narrow, so
// walking their complete subtree safely handles pre-populated directory moves.
func (s *Service) ReconcileTargets(ctx context.Context, targets []string) (ReconcileResult, error) {
	if len(targets) == 0 {
		targets = []string{"."}
	}
	for index := range targets {
		targets[index] = normalizeFolderPath(targets[index])
	}
	return s.reconcileInventory(ctx, targets, -1)
}

func (s *Service) reconcileInventory(ctx context.Context, targets []string, maxDepth int) (ReconcileResult, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	currentScanner := s.currentScanner()
	files, warnings, err := currentScanner.DiscoverFiles(ctx, targets, maxDepth)
	if err != nil {
		return ReconcileResult{Warnings: warnings}, err
	}
	s.mu.RLock()
	existing := cloneTracks(s.tracks)
	librarySummary := cloneLibrary(s.library)
	report := s.report
	s.mu.RUnlock()

	byPath := make(map[string]int, len(existing))
	for index, track := range existing {
		byPath[track.RelativePath] = index
	}
	seen := make(map[string]struct{}, len(files))
	changed := make([]domain.Track, 0)
	pendingSet := make(map[string]struct{})
	for _, file := range files {
		seen[file.RelativePath] = struct{}{}
		index, found := byPath[file.RelativePath]
		if !found {
			track := currentScanner.DraftTrack(file)
			existing = append(existing, track)
			byPath[file.RelativePath] = len(existing) - 1
			changed = append(changed, track)
			pendingSet[file.RelativePath] = struct{}{}
			continue
		}
		track := existing[index]
		fingerprintChanged := track.FileFingerprint != file.FileFingerprint
		filesystemChanged := fingerprintChanged || track.Missing || track.SizeBytes != file.SizeBytes || track.Writable != file.Writable
		if filesystemChanged {
			track.FileName = file.FileName
			track.FolderID = file.FolderID
			track.Format = file.Format
			track.SizeBytes = file.SizeBytes
			track.ModifiedAt = file.ModifiedAt
			track.Writable = file.Writable
			track.FileFingerprint = file.FileFingerprint
			track.Missing = false
			track.MissingSince = ""
			track.SyncState = domain.SyncDraft
			track.ParseError = ""
			existing[index] = track
			changed = append(changed, track)
		}
		if track.SyncState == domain.SyncDraft {
			pendingSet[file.RelativePath] = struct{}{}
		}
	}

	missing := 0
	// A partial read must never convert cached files into missing records.
	if len(warnings) == 0 {
		for index, track := range existing {
			if track.Missing || !inInventoryScope(track.RelativePath, targets, maxDepth) {
				continue
			}
			if _, found := seen[track.RelativePath]; found {
				continue
			}
			track.Missing = true
			track.MissingSince = time.Now().UTC().Format(time.RFC3339)
			track.Health = domain.HealthMissing
			existing[index] = track
			changed = append(changed, track)
			missing++
		}
	}

	pending := make([]string, 0, len(pendingSet))
	for path := range pendingSet {
		pending = append(pending, path)
	}
	sort.Strings(pending)
	if len(changed) == 0 {
		return ReconcileResult{Generation: s.EventGeneration(), Pending: pending, Missing: missing, Warnings: warnings}, nil
	}
	sort.Slice(existing, func(i, j int) bool { return existing[i].RelativePath < existing[j].RelativePath })
	librarySummary.TrackCount = presentTrackCount(existing)
	librarySummary.Folders = buildFoldersForTracks(existing)
	librarySummary.FolderCount = len(librarySummary.Folders)
	result := scanner.Result{Library: librarySummary, Tracks: existing, Report: report}
	if s.repo != nil {
		if deltaRepo, ok := s.repo.(DeltaRepository); ok {
			if err := deltaRepo.SaveTrackUpdates(ctx, currentScanner.Root(), result, changed); err != nil {
				return ReconcileResult{}, fmt.Errorf("persist filesystem inventory: %w", err)
			}
		} else if err := s.repo.SaveScan(ctx, currentScanner.Root(), result); err != nil {
			return ReconcileResult{}, fmt.Errorf("persist filesystem inventory: %w", err)
		}
	}
	s.apply(result)
	paths := make([]string, 0, len(changed))
	for _, track := range changed {
		paths = append(paths, track.RelativePath)
	}
	s.publishEvent(Event{Kind: EventInventory, Paths: paths})
	return ReconcileResult{Generation: s.EventGeneration(), Changed: true, Pending: pending, Missing: missing, Warnings: warnings}, nil
}

func normalizeFolderPath(value string) string {
	value = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(value))))
	if value == "" || value == "/" {
		return "."
	}
	return value
}

func inInventoryScope(relativePath string, targets []string, maxDepth int) bool {
	path := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relativePath)))
	for _, target := range targets {
		target = normalizeFolderPath(target)
		if target != "." && path != target && !strings.HasPrefix(path, target+"/") {
			continue
		}
		if maxDepth < 0 || path == target {
			return true
		}
		relative := path
		if target != "." {
			relative = strings.TrimPrefix(path, target+"/")
		}
		directory := filepath.ToSlash(filepath.Dir(relative))
		depth := 0
		if directory != "." && directory != "" {
			depth = len(strings.Split(directory, "/"))
		}
		if depth <= maxDepth {
			return true
		}
	}
	return false
}

func presentTrackCount(tracks []domain.Track) int {
	count := 0
	for _, track := range tracks {
		if !track.Missing {
			count++
		}
	}
	return count
}

func (s *Service) PendingPaths() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	paths := make([]string, 0)
	for _, track := range s.tracks {
		if !track.Missing && track.SyncState == domain.SyncDraft {
			paths = append(paths, track.RelativePath)
		}
	}
	sort.Strings(paths)
	return paths
}
