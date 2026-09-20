package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/config"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/jobs"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/organizer"
	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/providers/itunes"
	"github.com/ericwyn/tagger/internal/providers/kugou"
	"github.com/ericwyn/tagger/internal/providers/kuwo"
	"github.com/ericwyn/tagger/internal/providers/lrcapi"
	"github.com/ericwyn/tagger/internal/providers/lrclib"
	"github.com/ericwyn/tagger/internal/providers/musicbrainz"
	"github.com/ericwyn/tagger/internal/providers/netease"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/server"
	"github.com/ericwyn/tagger/internal/store"
	"github.com/ericwyn/tagger/internal/tags/taglibwasm"
	"github.com/ericwyn/tagger/internal/version"
	"github.com/ericwyn/tagger/internal/watcher"
	"github.com/ericwyn/tagger/web"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	dataStore, err := store.Open(ctx, filepath.Join(cfg.DataDir, "tagger.db"))
	if err != nil {
		cancel()
		logger.Error("initialize persistent store", "error", err)
		os.Exit(1)
	}
	defer dataStore.Close()
	providerRegistry := providers.NewRegistry(
		musicbrainz.New(musicbrainz.Config{}),
		lrclib.New(lrclib.Config{}),
		itunes.New(itunes.Config{}),
		netease.New(netease.Config{}),
		kugou.New(kugou.Config{}),
		kuwo.New(kuwo.Config{}),
		lrcapi.New(lrcapi.Config{BaseURL: os.Getenv("TAGGER_LRCAPI_URL"), CoverURL: os.Getenv("TAGGER_LRCAPI_COVER_URL"), Auth: os.Getenv("TAGGER_LRCAPI_AUTH")}),
	)
	if err := providerRegistry.SetPersistence(ctx, dataStore); err != nil {
		cancel()
		logger.Error("load provider persistence", "error", err)
		os.Exit(1)
	}
	if cfg.TestProviders {
		report, diagnosticErr := executeProviderDiagnostics(ctx, providerRegistry, providers.Query{
			Title: cfg.TestTitle, Artists: parseProviderTestArtists(cfg.TestArtists), Album: cfg.TestAlbum, DurationSeconds: cfg.TestDuration,
		}, providerDiagnosticOptions{Limit: cfg.TestLimit, ProbeArtwork: cfg.TestArtwork}, artworkDownloader(providerRegistry.DownloadArtwork))
		var writeErr error
		if diagnosticErr == nil {
			writeErr = writeProviderDiagnostics(os.Stdout, report, cfg.TestJSON)
		}
		cancel()
		if diagnosticErr != nil || writeErr != nil {
			if diagnosticErr == nil {
				diagnosticErr = writeErr
			}
			logger.Error("provider diagnostics failed", "error", diagnosticErr)
			_ = dataStore.Close()
			os.Exit(1)
		}
		if report.Summary.Failed > 0 {
			_ = dataStore.Close()
			os.Exit(1)
		}
		return
	}
	musicDir := ""
	usingPersistedLibrary := false
	if persisted, found, rootErr := dataStore.LibraryRoot(ctx); rootErr != nil {
		cancel()
		logger.Error("load persisted library root", "error", rootErr)
		os.Exit(1)
	} else if found {
		if info, statErr := os.Stat(persisted); statErr == nil && info.IsDir() {
			musicDir = persisted
			usingPersistedLibrary = true
		} else {
			logger.Warn("persisted music directory is unavailable; trying bootstrap configuration", "path", persisted)
		}
	}
	// A configured path bootstraps a fresh installation. Once a runtime
	// selection exists, the persisted selection wins so an explicitly removed
	// library does not reappear after restart.
	if musicDir == "" {
		musicDir = strings.TrimSpace(cfg.MusicDir)
	}
	if musicDir == "" {
		// Older installations may already have indexed libraries but no
		// active-root setting. Prefer the most recently scanned valid one.
		first, firstFound, firstErr := dataStore.FirstLibraryRoot(ctx)
		if firstErr != nil {
			cancel()
			logger.Error("load indexed library root", "error", firstErr)
			os.Exit(1)
		}
		if firstFound {
			if info, statErr := os.Stat(first); statErr == nil && info.IsDir() {
				musicDir = first
				if err := dataStore.SetLibraryRoot(ctx, first); err != nil {
					cancel()
					logger.Error("persist recovered library root", "error", err)
					os.Exit(1)
				}
			}
		}
	}
	usingEmptyLibrary := false
	if musicDir == "" {
		// Do not leave a stale setting pointing at an unavailable directory:
		// the old indexed summary remains selectable, but no library is
		// active until the user chooses a valid root in Settings.
		if err := dataStore.ClearLibraryRoot(ctx); err != nil {
			cancel()
			logger.Error("clear unavailable library root", "error", err)
			os.Exit(1)
		}
		// Keep scanner and writer valid while the UI shows the empty state.
		// This private root is removed from the registry after the no-op scan.
		emptyRoot := filepath.Join(cfg.DataDir, ".tagger-empty-library")
		if err := os.MkdirAll(emptyRoot, 0o700); err != nil {
			cancel()
			logger.Error("create empty library root", "error", err)
			os.Exit(1)
		}
		musicDir = emptyRoot
		usingEmptyLibrary = true
	}
	engine := taglibwasm.New()
	musicScanner, err := scanner.New(engine, scanner.Options{
		Root:        musicDir,
		LibraryName: cfg.LibraryName,
		Workers:     cfg.ScanWorkers,
	})
	if err != nil {
		cancel()
		logger.Error("initialize scanner", "error", err)
		os.Exit(1)
	}
	if cfg.MusicDir != "" && !usingPersistedLibrary {
		if err := dataStore.SetLibraryRoot(ctx, musicScanner.Root()); err != nil {
			cancel()
			logger.Error("persist library root", "error", err)
			os.Exit(1)
		}
	}
	libraryService, err := library.New(ctx, musicScanner, dataStore)
	if usingEmptyLibrary {
		if cleanupErr := dataStore.DeleteLibraryByRoot(ctx, musicScanner.Root()); cleanupErr != nil {
			cancel()
			logger.Error("remove empty library placeholder", "error", cleanupErr)
			os.Exit(1)
		}
	}
	cancel()
	if err != nil {
		logger.Error("initial library scan failed", "error", err)
		os.Exit(1)
	}
	tagWriter, err := filewrite.New(musicScanner.Root(), engine)
	if err != nil {
		logger.Error("initialize safe tag writer", "error", err)
		os.Exit(1)
	}
	var artworkCache *artwork.Cache
	if cache, cacheErr := artwork.NewCache(filepath.Join(filepath.Dir(dataStore.Path()), "artwork-cache"), artwork.DefaultCacheTTL); cacheErr != nil {
		logger.Warn("initialize artwork cache; remote images will not be cached", "error", cacheErr)
	} else {
		artworkCache = cache
	}
	configuredArtworkDownloader := artworkDownloader(providerRegistry.DownloadArtwork)
	cachedArtworkDownloader := configuredArtworkDownloader
	if artworkCache != nil {
		cachedArtworkDownloader = func(ctx context.Context, reference providers.ArtworkReference) (artwork.Asset, error) {
			return artworkCache.Get(ctx, reference.URL, func() (artwork.Asset, error) {
				return configuredArtworkDownloader(ctx, reference)
			})
		}
	}
	jobManager := jobs.New(dataStore)
	var libraryWatcher *watcher.Manager
	watchContext, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	jobManager.Register(domain.JobScan, func(ctx context.Context, job domain.Job, progress jobs.Progress) error {
		var payload struct {
			Root    string           `json:"root"`
			Mode    scanner.ScanMode `json:"mode"`
			Targets []string         `json:"targets"`
		}
		if strings.TrimSpace(job.Payload) != "" {
			if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
				return fmt.Errorf("decode scan payload: %w", err)
			}
		}
		if strings.TrimSpace(payload.Root) != "" {
			if err := progress(0, job.Total, 0, 0, "正在切换并扫描新的音乐目录"); err != nil {
				return err
			}
			if err := libraryService.SwitchRoot(ctx, payload.Root); err != nil {
				return err
			}
			if err := tagWriter.SetRoot(payload.Root); err != nil {
				return err
			}
			if cfg.WatchMode == domain.WatchModePoll {
				libraryService.SetWatchStatus(cfg.WatchMode, domain.WatchStatePolling)
			} else if libraryWatcher != nil {
				if err := libraryWatcher.Start(watchContext, payload.Root); err != nil {
					logger.Warn("restart library watcher after switch", "error", err)
					libraryService.SetWatchStatus(cfg.WatchMode, domain.WatchStateDegraded)
				} else {
					libraryService.SetWatchStatus(cfg.WatchMode, domain.WatchStateHealthy)
				}
			}
			total := libraryService.Library().TrackCount
			return progress(total, total, total, 0, fmt.Sprintf("已切换曲库并索引 %d 首曲目", total))
		}
		before := libraryService.Library().TrackCount
		if err := progress(0, before, 0, 0, "正在发现并解析音乐文件"); err != nil {
			return err
		}
		var scanErr error
		if payload.Mode == scanner.ScanFull {
			scanErr = libraryService.Rescan(ctx)
		} else {
			scanErr = libraryService.QuickScan(ctx, payload.Targets)
		}
		if scanErr != nil {
			return scanErr
		}
		report := libraryService.LastReport()
		total := report.Discovered
		if total == 0 {
			total = before
		}
		succeeded := report.Parsed + report.Unchanged
		return progress(total, total, succeeded, report.Failed, fmt.Sprintf("%s扫描完成：变化 %d 首，未变化 %d 首，解析失败 %d 首，缺失 %d 首", scanModeLabel(payload.Mode), report.Changed, report.Unchanged, report.Failed, report.Missing))
	})
	jobManager.Register(domain.JobMatch, func(ctx context.Context, job domain.Job, progress jobs.Progress) error {
		var payload struct {
			TrackIDs    []string `json:"trackIds"`
			ProviderIDs []string `json:"providerIds"`
			Limit       int      `json:"limit"`
		}
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return err
		}
		if payload.Limit <= 0 {
			payload.Limit = providers.DefaultBatchCandidateLimit
		}
		processed, failed, succeeded := 0, 0, 0
		providerQueries, candidateCount := 0, 0
		var progressMu sync.Mutex
		// Provider gates still enforce one in-flight request per source. Two
		// tracks here only form a pipeline across different sources, hiding an
		// occasional slow provider without increasing per-source concurrency.
		if err := runMatchPipeline(ctx, payload.TrackIDs, func(matchCtx context.Context, trackID string) error {
			trackFailed, trackSucceeded := 0, 0
			trackProviderQueries, trackCandidateCount := 0, 0
			track, err := libraryService.Track(trackID)
			if err == nil && track.SyncState != "" && track.SyncState != domain.SyncIndexed {
				err = library.ErrTrackNotIndexed
			}
			if err != nil {
				trackFailed = 1
				if persistErr := dataStore.UpsertMatchItem(matchCtx, store.MatchItem{JobID: job.ID, TrackID: trackID, State: "failed", Error: err.Error()}); persistErr != nil {
					return fmt.Errorf("persist failed match item %s: %w", trackID, persistErr)
				}
			} else {
				result, searchErr := providerRegistry.SearchTrack(matchCtx, track, providers.Query{}, payload.ProviderIDs, payload.Limit)
				if searchErr != nil {
					trackFailed = 1
					if persistErr := dataStore.UpsertMatchItem(matchCtx, store.MatchItem{JobID: job.ID, TrackID: trackID, State: "failed", Error: searchErr.Error()}); persistErr != nil {
						return fmt.Errorf("persist failed match item %s: %w", trackID, persistErr)
					}
				} else {
					trackProviderQueries = len(result.Providers)
					trackCandidateCount = len(result.Candidates)
					state := "review"
					selectedCandidateID := ""
					if len(result.Candidates) == 0 {
						state = "no_match"
						trackFailed = 1
					} else {
						trackSucceeded = 1
						for _, candidate := range result.Candidates {
							if !candidate.Recommended {
								continue
							}
							selectedCandidateID = candidate.ID
							if candidate.AutoAccept {
								state = "accepted"
							}
							break
						}
					}
					candidateJSON, marshalErr := json.Marshal(result.Candidates)
					if marshalErr != nil {
						return fmt.Errorf("encode match candidates for %s: %w", trackID, marshalErr)
					}
					if persistErr := dataStore.UpsertMatchItem(matchCtx, store.MatchItem{JobID: job.ID, TrackID: trackID, State: state, Candidates: candidateJSON, SelectedCandidateID: selectedCandidateID}); persistErr != nil {
						return fmt.Errorf("persist match item %s: %w", trackID, persistErr)
					}
				}
			}

			progressMu.Lock()
			defer progressMu.Unlock()
			processed++
			failed += trackFailed
			succeeded += trackSucceeded
			providerQueries += trackProviderQueries
			candidateCount += trackCandidateCount
			return progress(processed, len(payload.TrackIDs), succeeded, failed, formatMatchProgress(processed, len(payload.TrackIDs), providerQueries, candidateCount, failed))
		}); err != nil {
			return err
		}
		if succeeded > 0 {
			return jobs.ErrNeedsReview
		}
		return nil
	})
	jobManager.Register(domain.JobWrite, func(ctx context.Context, job domain.Job, progress jobs.Progress) error {
		var payload struct {
			MatchJobID string `json:"matchJobId"`
			Items      []struct {
				TrackID        string   `json:"trackId"`
				CandidateID    string   `json:"candidateId"`
				BaseRevision   string   `json:"baseRevision"`
				Fields         []string `json:"fields"`
				Artwork        bool     `json:"artwork"`
				ArtworkMaxSize int      `json:"artworkMaxSize"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return err
		}
		type completedWrite struct {
			trackID       string
			candidate     providers.MatchCandidate
			tagResult     filewrite.Result
			artworkResult *filewrite.ArtworkResult
		}
		completed := make([]completedWrite, 0, len(payload.Items))
		succeeded, failed := 0, 0
		var lastFailure error
		for index, item := range payload.Items {
			matchItem, err := dataStore.MatchItem(ctx, payload.MatchJobID, item.TrackID)
			var candidate providers.MatchCandidate
			if err == nil {
				var candidates []providers.MatchCandidate
				err = json.Unmarshal(matchItem.Candidates, &candidates)
				for _, candidateItem := range candidates {
					if candidateItem.ID == item.CandidateID {
						candidate = candidateItem
						break
					}
				}
				if candidate.ID == "" {
					err = fmt.Errorf("候选不存在")
				}
			}
			track, trackErr := libraryService.Track(item.TrackID)
			if err == nil && trackErr != nil {
				err = trackErr
			}
			var artworkTarget *artwork.Asset
			if err == nil && item.Artwork {
				artworkTarget, err = prepareCandidateArtwork(ctx, providerRegistry, candidate, cachedArtworkDownloader)
				if err == nil && item.ArtworkMaxSize > 0 {
					resized, resizeErr := artwork.ResizeSquare(*artworkTarget, item.ArtworkMaxSize)
					if resizeErr != nil {
						err = fmt.Errorf("裁剪候选封面：%w", resizeErr)
					} else {
						artworkTarget = &resized
					}
				}
			}
			var artworkResult *filewrite.ArtworkResult
			artworkFailedAfterTags := false
			if err == nil {
				ref, refErr := libraryService.FileRef(item.TrackID)
				if refErr != nil {
					err = refErr
				} else {
					baseRevision := item.BaseRevision
					if baseRevision == "" {
						baseRevision = track.Revision
					}
					tagResult, writeErr := tagWriter.Write(ctx, ref, baseRevision, patchFromCandidate(candidate, item.Fields), false)
					if writeErr != nil {
						err = writeErr
					} else {
						if artworkTarget != nil {
							result, artworkErr := tagWriter.WriteArtwork(ctx, ref, tagResult.CurrentRevision, 0, artworkTarget, false)
							if artworkErr != nil {
								err = fmt.Errorf("写入候选封面：%w", artworkErr)
								artworkFailedAfterTags = true
							} else {
								artworkResult = &result
							}
						}
						// Keep every track whose tag phase completed so it can be
						// rescanned and recorded even when the requested artwork phase
						// failed. It is still one failed item in the job counters.
						completed = append(completed, completedWrite{trackID: item.TrackID, candidate: candidate, tagResult: tagResult, artworkResult: artworkResult})
					}
				}
			}
			if err != nil {
				failed++
				lastFailure = err
				state := "write_failed"
				if artworkFailedAfterTags {
					state = "artwork_failed"
				}
				if persistErr := dataStore.UpsertMatchItem(ctx, store.MatchItem{ID: matchItem.ID, JobID: payload.MatchJobID, TrackID: item.TrackID, State: state, Candidates: matchItem.Candidates, SelectedCandidateID: item.CandidateID, ReviewFields: append([]string(nil), item.Fields...), ReviewArtwork: item.Artwork, ReviewArtworkMaxSize: item.ArtworkMaxSize, Error: err.Error()}); persistErr != nil {
					return fmt.Errorf("persist failed match item %s: %w", item.TrackID, persistErr)
				}
			} else {
				if persistErr := dataStore.UpsertMatchItem(ctx, store.MatchItem{ID: matchItem.ID, JobID: payload.MatchJobID, TrackID: item.TrackID, State: "written", Candidates: matchItem.Candidates, SelectedCandidateID: item.CandidateID, ReviewFields: append([]string(nil), item.Fields...), ReviewArtwork: item.Artwork, ReviewArtworkMaxSize: item.ArtworkMaxSize}); persistErr != nil {
					return fmt.Errorf("persist written match item %s: %w", item.TrackID, persistErr)
				}
				succeeded++
			}
			if err := progress(index+1, len(payload.Items), succeeded, failed, formatItemProgress("写入", index+1, len(payload.Items), succeeded, failed, lastFailure)); err != nil {
				return err
			}
		}
		if len(completed) > 0 {
			ids := make([]string, 0, len(completed))
			for _, item := range completed {
				ids = append(ids, item.trackID)
			}
			if _, err := libraryService.RescanTracks(ctx, ids); err != nil {
				return err
			}
			for _, item := range completed {
				track, trackErr := libraryService.Track(item.trackID)
				if trackErr != nil {
					continue
				}
				source := candidateRevisionSource(providerRegistry, item.candidate)
				diff := append([]domain.RevisionDiff(nil), item.tagResult.Diff...)
				beforeTags, afterTags := item.tagResult.BeforeTags, item.tagResult.AfterTags
				baseRevision, resultRevision := item.tagResult.BaseRevision, item.tagResult.CurrentRevision
				action := "批量采用候选标签"
				if item.artworkResult != nil {
					diff = append(diff, item.artworkResult.Diff...)
					afterTags = item.artworkResult.AfterTags
					resultRevision = item.artworkResult.CurrentRevision
					action = "批量采用候选标签与封面"
				}
				if len(diff) == 0 {
					continue
				}
				var beforeArtwork, afterArtwork *domain.ArtworkSnapshot
				if item.artworkResult != nil {
					beforeArtwork = artworkRevisionSnapshot(item.artworkResult.Before)
					afterArtwork = artworkRevisionSnapshot(item.artworkResult.After)
				}
				if _, historyErr := dataStore.CreateRevision(ctx, domain.Revision{LibraryID: libraryService.Library().ID, TrackID: track.ID, TrackTitle: track.Title, FileName: track.FileName, Action: action, Source: source, BaseRevision: baseRevision, ResultRevision: resultRevision, Diff: diff, CoverTone: track.CoverTone, BeforeTags: beforeTags, AfterTags: afterTags, BeforeArtwork: beforeArtwork, AfterArtwork: afterArtwork}); historyErr != nil {
					return fmt.Errorf("persist batch revision for %s: %w", item.trackID, historyErr)
				}
			}
		}
		if err := finalizeMatchJobAfterWrite(ctx, jobManager, dataStore, payload.MatchJobID); err != nil {
			return fmt.Errorf("finalize match job after write: %w", err)
		}
		return nil
	})
	jobManager.Register(domain.JobBatchEdit, newBatchEditHandler(libraryService, tagWriter, dataStore))
	jobManager.Register(domain.JobOrganize, newOrganizeHandler(libraryService, dataStore))
	if err := jobManager.Start(context.Background()); err != nil {
		logger.Error("start persistent job worker", "error", err)
		os.Exit(1)
	}
	defer jobManager.Close()
	enqueueScan := func(ctx context.Context, mode scanner.ScanMode, targets []string, titleSuffix string) error {
		current := libraryService.Library()
		if current.ID == "" {
			return nil
		}
		active, err := jobManager.HasBlockingFileWork(ctx)
		if err != nil {
			return err
		}
		if active {
			return watcher.ErrBusy
		}
		payload, _ := json.Marshal(struct {
			Mode    scanner.ScanMode `json:"mode"`
			Targets []string         `json:"targets"`
		}{Mode: mode, Targets: targets})
		_, err = jobManager.Enqueue(ctx, domain.Job{
			Kind: domain.JobScan, LibraryID: current.ID, Title: current.Name + titleSuffix,
			Detail: "等待文件变化扫描 worker", Total: current.TrackCount, Payload: string(payload),
		})
		return err
	}
	if cfg.WatchMode == domain.WatchModePoll {
		libraryService.SetWatchStatus(cfg.WatchMode, domain.WatchStatePolling)
	} else {
		libraryWatcher = watcher.New(cfg.WatcherWait, func(ctx context.Context, targets []string) error {
			result, err := libraryService.ReconcileTargets(ctx, targets)
			if err != nil {
				return err
			}
			libraryService.SetWatchStatus(cfg.WatchMode, domain.WatchStateHealthy)
			if len(result.Pending) == 0 {
				return nil
			}
			return enqueueScan(ctx, scanner.ScanTarget, result.Pending, " 文件变化扫描")
		})
		if err := libraryWatcher.Start(watchContext, libraryService.Root()); err != nil {
			logger.Warn("initialize library watcher; directory polling fallback enabled", "error", err)
			libraryService.SetWatchStatus(cfg.WatchMode, domain.WatchStateDegraded)
		} else {
			libraryService.SetWatchStatus(cfg.WatchMode, domain.WatchStateHealthy)
		}
		go func() {
			for err := range libraryWatcher.Errors() {
				logger.Warn("library watcher error; directory polling fallback enabled", "error", err)
				libraryService.SetWatchStatus(cfg.WatchMode, domain.WatchStateDegraded)
			}
		}()
		defer libraryWatcher.Stop()
	}
	if pending := libraryService.PendingPaths(); len(pending) > 0 {
		payload, _ := json.Marshal(struct {
			Mode    scanner.ScanMode `json:"mode"`
			Targets []string         `json:"targets"`
		}{Mode: scanner.ScanQuick})
		if _, err := jobManager.Enqueue(context.Background(), domain.Job{
			Kind: domain.JobScan, LibraryID: libraryService.Library().ID,
			Title:  libraryService.Library().Name + " 恢复待索引文件",
			Detail: "等待标签投影重建 worker", Total: len(pending), Payload: string(payload),
		}); err != nil {
			logger.Warn("enqueue persisted draft metadata scan", "error", err)
		}
	}
	if cfg.ReconcileInterval > 0 {
		go func() {
			ticker := time.NewTicker(cfg.ReconcileInterval)
			defer ticker.Stop()
			for {
				select {
				case <-watchContext.Done():
					return
				case <-ticker.C:
					if err := enqueueScan(context.Background(), scanner.ScanQuick, nil, " 定时对账"); err != nil && !errors.Is(err, watcher.ErrBusy) {
						logger.Warn("enqueue periodic library reconciliation", "error", err)
					}
				}
			}
		}()
	}

	srv := server.New(cfg.Listen, libraryService, tagWriter, providerRegistry, dataStore, web.Dist(), version.Version, engine.Version())
	if artworkCache != nil {
		srv.SetArtworkCache(artworkCache)
	}
	srv.SetAuthToken(cfg.AuthToken)
	srv.SetJobManager(jobManager)
	logger.Info("tagger started",
		"listen", cfg.Listen,
		"library", musicScanner.Root(),
		"data", dataStore.Path(),
		"tracks", libraryService.Library().TrackCount,
		"version", version.Version,
	)
	srv.Spin()
}

func formatMatchProgress(processed, total, providerQueries, candidateCount, failed int) string {
	detail := fmt.Sprintf("已分析 %d/%d 首曲目 · 已查询 %d 次数据源 · 返回 %d 个候选", processed, total, providerQueries, candidateCount)
	if failed > 0 {
		detail += fmt.Sprintf(" · %d 首失败或无匹配", failed)
	}
	return detail
}

func formatItemProgress(action string, processed, total, succeeded, failed int, lastFailure error) string {
	detail := fmt.Sprintf("已处理 %d/%d 首曲目 · %s成功 %d 首 · 失败 %d 首", processed, total, action, succeeded, failed)
	if lastFailure == nil {
		return detail
	}
	message := strings.Join(strings.Fields(lastFailure.Error()), " ")
	characters := []rune(message)
	if len(characters) > 160 {
		message = string(characters[:157]) + "…"
	}
	return detail + " · 最近失败：" + message
}

func scanModeLabel(mode scanner.ScanMode) string {
	if mode == scanner.ScanFull {
		return "完整"
	}
	if mode == scanner.ScanTarget {
		return "定向"
	}
	return "快速"
}

// finalizeMatchJobAfterWrite closes the parent review workflow once its
// selected write items have reached terminal states. The write job remains the
// authoritative progress record for the actual file operations; this update
// only prevents the parent match job from staying in "待审核" forever after
// every item has been handled.
func finalizeMatchJobAfterWrite(ctx context.Context, manager *jobs.Manager, dataStore *store.Store, matchJobID string) error {
	if manager == nil || dataStore == nil || strings.TrimSpace(matchJobID) == "" {
		return nil
	}
	matchJob, err := manager.Get(ctx, matchJobID)
	if err != nil {
		return err
	}
	if !domain.IsMatchReviewable(matchJob) {
		return nil
	}
	items, err := dataStore.ListMatchItems(ctx, matchJobID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	pending := false
	written := 0
	skipped := 0
	failures := 0
	for _, item := range items {
		switch item.State {
		case "written":
			written++
		case "skipped":
			skipped++
		case "failed", "no_match", "write_failed", "artwork_failed":
			failures++
		case "review", "accepted", "write_pending":
			pending = true
		default:
			// Unknown future states are kept open rather than prematurely marking
			// the parent task complete.
			pending = true
		}
	}
	if pending {
		return nil
	}
	if matchJob.Total == 0 {
		matchJob.Total = len(items)
	}
	if len(items) > matchJob.Total {
		matchJob.Total = len(items)
	}
	// A legacy/incomplete snapshot can be missing item rows. Count only those
	// unrepresented tracks as failures; an item that was successfully rematched
	// must be allowed to clear its earlier failure count.
	failures += max(0, matchJob.Total-len(items))
	resolved := written + skipped
	matchJob.Processed = resolved + failures
	matchJob.Failed = failures
	matchJob.Succeeded = resolved
	matchJob.CompletedAt = time.Now().UTC()
	if failures > 0 && resolved > 0 {
		matchJob.State = domain.JobPartial
		matchJob.Detail = fmt.Sprintf("审核完成：已写入 %d 首，%d 首失败", written, failures)
	} else if failures > 0 {
		matchJob.State = domain.JobFailed
		matchJob.Detail = fmt.Sprintf("审核失败：已写入 %d 首，%d 首失败", written, failures)
	} else {
		matchJob.State = domain.JobSucceeded
		matchJob.Detail = fmt.Sprintf("审核完成：已写入 %d 首", written)
	}
	if skipped > 0 {
		matchJob.Detail += fmt.Sprintf("，跳过 %d 首", skipped)
	}
	return manager.Update(ctx, matchJob)
}

func newBatchEditHandler(libraryService *library.Service, tagWriter *filewrite.Writer, dataStore *store.Store) jobs.Handler {
	return func(ctx context.Context, job domain.Job, progress jobs.Progress) error {
		var payload domain.BatchEditPayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return err
		}
		var batchArtwork *artwork.Asset
		if payload.Artwork != nil && payload.Artwork.Action == domain.BatchArtworkReplace {
			data, err := base64.StdEncoding.DecodeString(payload.Artwork.Data)
			if err != nil {
				return fmt.Errorf("decode batch artwork: %w", err)
			}
			asset, err := artwork.Validate(data, payload.Artwork.MIME)
			if err != nil {
				return fmt.Errorf("validate batch artwork: %w", err)
			}
			if payload.Artwork.MaxSize > 0 {
				asset, err = artwork.ResizeSquare(asset, payload.Artwork.MaxSize)
				if err != nil {
					return fmt.Errorf("resize batch artwork: %w", err)
				}
			}
			batchArtwork = &asset
		}
		completed := make([]struct {
			track         domain.Track
			result        filewrite.Result
			artworkResult *filewrite.ArtworkResult
		}, 0, len(payload.Items))
		succeeded, failed := 0, 0
		var lastFailure error
		for index, item := range payload.Items {
			track, err := libraryService.Track(item.TrackID)
			var result filewrite.Result
			var completedArtworkResult *filewrite.ArtworkResult
			tagPhaseCompleted := false
			if err == nil {
				ref, refErr := libraryService.FileRef(item.TrackID)
				if refErr != nil {
					err = refErr
				} else {
					baseRevision := item.BaseRevision
					if baseRevision == "" {
						baseRevision = track.Revision
					}
					result, err = tagWriter.Write(ctx, ref, baseRevision, patchFromBatchEdit(track, payload.Operations, payload.SequenceTracks, index, len(payload.Items)), false)
					tagPhaseCompleted = err == nil
					if err == nil && payload.Artwork != nil {
						var target *artwork.Asset
						if payload.Artwork.Action == domain.BatchArtworkReplace {
							target = batchArtwork
						}
						artworkResult, artworkErr := tagWriter.WriteArtwork(ctx, ref, result.CurrentRevision, 0, target, false)
						if artworkErr != nil {
							err = artworkErr
						} else {
							result.Changed = result.Changed || artworkResult.Changed
							result.CurrentRevision = artworkResult.CurrentRevision
							result.Diff = append(result.Diff, artworkResult.Diff...)
							result.Warnings = append(result.Warnings, artworkResult.Warnings...)
							artworkResultCopy := artworkResult
							completedArtworkResult = &artworkResultCopy
						}
					}
				}
			}
			if tagPhaseCompleted {
				// Preserve successful tag mutations for rescan/history even when
				// a following artwork phase makes the overall item fail.
				completed = append(completed, struct {
					track         domain.Track
					result        filewrite.Result
					artworkResult *filewrite.ArtworkResult
				}{track: track, result: result, artworkResult: completedArtworkResult})
			}
			state := "written"
			if err != nil {
				failed++
				lastFailure = err
				state = "failed"
			}
			diff, marshalErr := json.Marshal(result.Diff)
			if marshalErr != nil {
				return marshalErr
			}
			if persistErr := dataStore.UpsertBatchEditItem(ctx, store.BatchEditItem{JobID: job.ID, TrackID: item.TrackID, State: state, Error: errorText(err), Diff: diff}); persistErr != nil {
				return fmt.Errorf("persist batch edit item %s: %w", item.TrackID, persistErr)
			}
			if err == nil {
				succeeded++
			}
			if progressErr := progress(index+1, len(payload.Items), succeeded, failed, formatItemProgress("编辑", index+1, len(payload.Items), succeeded, failed, lastFailure)); progressErr != nil {
				return progressErr
			}
		}
		if len(completed) == 0 {
			return nil
		}
		ids := make([]string, 0, len(completed))
		for _, item := range completed {
			ids = append(ids, item.track.ID)
		}
		if _, err := libraryService.RescanTracks(ctx, ids); err != nil {
			return err
		}
		for _, item := range completed {
			if !item.result.Changed {
				continue
			}
			track, err := libraryService.Track(item.track.ID)
			if err != nil {
				return err
			}
			action := "批量编辑标签"
			if item.artworkResult != nil {
				action = "批量编辑标签与封面"
			}
			var beforeArtwork, afterArtwork *domain.ArtworkSnapshot
			if item.artworkResult != nil {
				beforeArtwork = artworkRevisionSnapshot(item.artworkResult.Before)
				afterArtwork = artworkRevisionSnapshot(item.artworkResult.After)
			}
			if _, err := dataStore.CreateRevision(ctx, domain.Revision{
				LibraryID: libraryService.Library().ID, TrackID: track.ID, TrackTitle: track.Title, FileName: track.FileName,
				Action: action, Source: "批量编辑", BaseRevision: item.result.BaseRevision,
				ResultRevision: track.Revision, Diff: item.result.Diff, CoverTone: track.CoverTone,
				BeforeTags: item.result.BeforeTags, AfterTags: item.result.AfterTags,
				BeforeArtwork: beforeArtwork, AfterArtwork: afterArtwork,
			}); err != nil {
				return err
			}
		}
		return nil
	}
}

func newOrganizeHandler(libraryService *library.Service, dataStore *store.Store) jobs.Handler {
	return func(ctx context.Context, job domain.Job, progress jobs.Progress) error {
		var payload domain.OrganizePayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return fmt.Errorf("decode organize payload: %w", err)
		}
		payload.Mode = domain.NormalizeOrganizeMode(payload.Mode)
		if !payload.Mode.Valid() {
			return fmt.Errorf("unsupported organize mode: %q", payload.Mode)
		}
		if normalized, normalizeErr := organizer.NormalizeBasePath(payload.BasePath); normalizeErr != nil {
			return fmt.Errorf("normalize organize base path: %w", normalizeErr)
		} else {
			payload.BasePath = normalized
		}
		planner, err := organizer.NewPlanner(libraryService.Root())
		if err != nil {
			return err
		}
		succeeded, failed := 0, 0
		var lastFailure error
		for index, item := range payload.Items {
			current, itemErr := libraryService.Track(item.TrackID)
			var plan organizer.Plan
			if itemErr == nil {
				if item.BaseRevision != "" && current.Revision != item.BaseRevision {
					itemErr = fmt.Errorf("文件在预览后发生变化")
				} else {
					plan, itemErr = planner.PlanWithBasePath(ctx, current, payload.BasePath, payload.Mode, true)
				}
			}
			persist := func(state domain.OrganizeItemState, failure error) error {
				if dataStore == nil {
					return nil
				}
				itemResult := domain.OrganizeItem{
					JobID: job.ID, TrackID: item.TrackID, State: state,
					Error: errorText(failure), Warnings: append([]string(nil), plan.Warnings...),
				}
				if plan.TrackID != "" {
					itemResult.Source, itemResult.Target = plan.Source, plan.Target
					itemResult.PrimaryArtist, itemResult.Album = plan.PrimaryArtist, plan.Album
					itemResult.SidecarSource, itemResult.SidecarTarget = plan.SidecarSource, plan.SidecarTarget
					itemResult.SidecarExists = plan.SidecarExists
				}
				return dataStore.UpsertOrganizeItem(ctx, itemResult)
			}
			if itemErr == nil {
				switch plan.State {
				case domain.OrganizeNoop:
					if err := persist(domain.OrganizeNoop, nil); err != nil {
						return err
					}
					succeeded++
				case domain.OrganizeReady:
					if moveErr := organizer.Move(ctx, plan); moveErr != nil {
						itemErr = moveErr
					} else if _, relocateErr := libraryService.RelocateTrack(ctx, item.TrackID, plan.Target); relocateErr != nil {
						itemErr = relocateErr
						if rollbackErr := organizer.Rollback(context.Background(), plan); rollbackErr != nil {
							itemErr = fmt.Errorf("%w；回滚失败：%v", itemErr, rollbackErr)
						}
					} else {
						if err := persist(domain.OrganizeMoved, nil); err != nil {
							return err
						}
						succeeded++
					}
				default:
					itemErr = fmt.Errorf("无法整理：%s", strings.Join(plan.Warnings, "；"))
				}
			}
			if itemErr != nil {
				failed++
				lastFailure = itemErr
				if err := persist(domain.OrganizeFailed, itemErr); err != nil {
					return err
				}
			}
			if err := progress(index+1, len(payload.Items), succeeded, failed, formatItemProgress("整理", index+1, len(payload.Items), succeeded, failed, lastFailure)); err != nil {
				return err
			}
		}
		return nil
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func patchFromBatchEdit(track domain.Track, operations []domain.BatchEditOperation, sequence bool, index, total int) domain.TagPatch {
	patch := domain.TagPatch{}
	for _, operation := range operations {
		switch operation.Field {
		case "title":
			patch.Title = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: batchStringValue(operation.Mode, track.Title, operation.Value, operation.Find)}
		case "artists":
			patch.Artists = &domain.StringsFieldPatch{Op: batchStringsOperation(operation.Mode), Value: batchListValue(operation.Mode, track.Artists, operation.Value, operation.Find)}
		case "album":
			patch.Album = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: batchStringValue(operation.Mode, track.Album, operation.Value, operation.Find)}
		case "albumArtists":
			patch.AlbumArtists = &domain.StringsFieldPatch{Op: batchStringsOperation(operation.Mode), Value: batchListValue(operation.Mode, track.AlbumArtists, operation.Value, operation.Find)}
		case "genres":
			patch.Genres = &domain.StringsFieldPatch{Op: batchStringsOperation(operation.Mode), Value: batchListValue(operation.Mode, track.Genres, operation.Value, operation.Find)}
		case "comment":
			patch.Comment = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: batchStringValue(operation.Mode, track.Comment, operation.Value, operation.Find)}
		case "composers":
			patch.Composers = &domain.StringsFieldPatch{Op: batchStringsOperation(operation.Mode), Value: batchListValue(operation.Mode, track.Composers, operation.Value, operation.Find)}
		case "conductor":
			patch.Conductor = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: batchStringValue(operation.Mode, track.Conductor, operation.Value, operation.Find)}
		case "lyricists":
			patch.Lyricists = &domain.StringsFieldPatch{Op: batchStringsOperation(operation.Mode), Value: batchListValue(operation.Mode, track.Lyricists, operation.Value, operation.Find)}
		case "copyright":
			patch.Copyright = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: batchStringValue(operation.Mode, track.Copyright, operation.Value, operation.Find)}
		case "bpm":
			if operation.Mode == domain.BatchEditDelete {
				patch.BPM = &domain.IntFieldPatch{Op: domain.OperationDelete}
			} else if bpm, err := strconv.Atoi(strings.TrimSpace(operation.Value)); err == nil && bpm > 0 {
				patch.BPM = &domain.IntFieldPatch{Op: domain.OperationSet, Value: bpm}
			}
		case "isrc":
			patch.ISRC = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: batchStringValue(operation.Mode, track.ISRC, operation.Value, operation.Find)}
		case "year":
			if operation.Mode == domain.BatchEditDelete {
				patch.Year = &domain.IntFieldPatch{Op: domain.OperationDelete}
				continue
			}
			if year, err := strconv.Atoi(strings.TrimSpace(operation.Value)); err == nil && year > 0 {
				patch.Year = &domain.IntFieldPatch{Op: domain.OperationSet, Value: year}
			}
		}
	}
	if sequence {
		patch.TrackNumber = &domain.IntFieldPatch{Op: domain.OperationSet, Value: index + 1}
		patch.TrackTotal = &domain.IntFieldPatch{Op: domain.OperationSet, Value: total}
	}
	return patch
}

func batchStringOperation(mode domain.BatchEditMode) domain.Operation {
	if mode == domain.BatchEditDelete {
		return domain.OperationDelete
	}
	return domain.OperationSet
}

func batchStringValue(mode domain.BatchEditMode, current, value, find string) string {
	if mode == domain.BatchEditDelete {
		return ""
	}
	if mode == domain.BatchEditReplace {
		return strings.ReplaceAll(current, find, value)
	}
	return strings.TrimSpace(value)
}

func batchStringsOperation(mode domain.BatchEditMode) domain.Operation {
	if mode == domain.BatchEditDelete {
		return domain.OperationDelete
	}
	return domain.OperationSet
}

func batchListValue(mode domain.BatchEditMode, current []string, value, find string) []string {
	if mode == domain.BatchEditDelete {
		return nil
	}
	if mode == domain.BatchEditReplace {
		result := make([]string, 0, len(current))
		for _, item := range current {
			item = strings.TrimSpace(strings.ReplaceAll(item, find, value))
			if item != "" && !slices.Contains(result, item) {
				result = append(result, item)
			}
		}
		return result
	}
	next := splitBatchValues(value)
	if mode != domain.BatchEditAppend {
		return next
	}
	result := append([]string(nil), current...)
	for _, item := range next {
		if !slices.Contains(result, item) {
			result = append(result, item)
		}
	}
	return result
}

func splitBatchValues(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == '\n' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

type artworkDownloader func(context.Context, providers.ArtworkReference) (artwork.Asset, error)

func artworkRevisionSnapshot(asset *artwork.Asset) *domain.ArtworkSnapshot {
	if asset == nil {
		return nil
	}
	return &domain.ArtworkSnapshot{MIME: asset.MIME, Format: asset.Format, Width: asset.Width, Height: asset.Height, Size: asset.Size, Hash: asset.Hash, Data: append([]byte(nil), asset.Data...)}
}

func prepareCandidateArtwork(ctx context.Context, registry *providers.Registry, candidate providers.MatchCandidate, download artworkDownloader) (*artwork.Asset, error) {
	if registry == nil || download == nil {
		return nil, fmt.Errorf("候选封面服务未初始化")
	}
	referenceID := candidate.ArtworkReferenceID
	if referenceID == "" {
		referenceID = candidate.ID
	}
	reference, err := registry.ArtworkReference(referenceID)
	if err != nil {
		return nil, fmt.Errorf("候选封面不可用：%w", err)
	}
	asset, err := download(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("下载候选封面：%w", err)
	}
	return &asset, nil
}

func candidateRevisionSource(registry *providers.Registry, candidate providers.MatchCandidate) string {
	switch candidate.Kind {
	case providers.CandidateKindSmart:
		return "智能选择"
	case providers.CandidateKindAI:
		return "AI 筛选"
	}
	if registry != nil {
		if descriptor, found := registry.Descriptor(candidate.ProviderID); found {
			return descriptor.Name
		}
	}
	if candidate.ProviderName != "" {
		return candidate.ProviderName
	}
	return "数据源候选"
}

func patchFromCandidate(candidate providers.MatchCandidate, fields []string) domain.TagPatch {
	selected := make(map[string]bool, len(fields))
	// A nil field list is the backwards-compatible default (all fields). An
	// explicitly empty JSON array means the reviewer deselected every field and
	// must produce a no-op patch instead of silently re-enabling all fields.
	if fields == nil {
		for _, field := range []string{"title", "artists", "album", "albumArtists", "trackNumber", "trackTotal", "discNumber", "discTotal", "year", "genres", "lyrics", "comment", "composers", "conductor", "lyricists", "copyright", "bpm", "isrc", "musicbrainzTrackId", "musicbrainzReleaseId", "musicbrainzArtistIds", "acoustidId", "acoustidFingerprint"} {
			selected[field] = true
		}
	} else {
		for _, field := range fields {
			selected[field] = true
		}
	}
	patch := domain.TagPatch{}
	if selected["title"] && candidate.Title.Value != "" {
		patch.Title = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.Title.Value}
	}
	if selected["artists"] && len(candidate.Artists.Value) > 0 {
		patch.Artists = &domain.StringsFieldPatch{Op: domain.OperationSet, Value: candidate.Artists.Value}
	}
	if selected["album"] && candidate.Album.Value != "" {
		patch.Album = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.Album.Value}
	}
	if selected["albumArtists"] && len(candidate.AlbumArtists.Value) > 0 {
		patch.AlbumArtists = &domain.StringsFieldPatch{Op: domain.OperationSet, Value: candidate.AlbumArtists.Value}
	}
	if selected["trackNumber"] && candidate.TrackNumber.Value > 0 {
		patch.TrackNumber = &domain.IntFieldPatch{Op: domain.OperationSet, Value: candidate.TrackNumber.Value}
	}
	if selected["trackTotal"] && candidate.TrackTotal.Value > 0 {
		patch.TrackTotal = &domain.IntFieldPatch{Op: domain.OperationSet, Value: candidate.TrackTotal.Value}
	}
	if selected["discNumber"] && candidate.DiscNumber.Value > 0 {
		patch.DiscNumber = &domain.IntFieldPatch{Op: domain.OperationSet, Value: candidate.DiscNumber.Value}
	}
	if selected["discTotal"] && candidate.DiscTotal.Value > 0 {
		patch.DiscTotal = &domain.IntFieldPatch{Op: domain.OperationSet, Value: candidate.DiscTotal.Value}
	}
	if selected["year"] && candidate.Year.Value >= 1000 {
		patch.Year = &domain.IntFieldPatch{Op: domain.OperationSet, Value: candidate.Year.Value}
	}
	if selected["genres"] && len(candidate.Genres.Value) > 0 {
		patch.Genres = &domain.StringsFieldPatch{Op: domain.OperationSet, Value: candidate.Genres.Value}
	}
	if selected["lyrics"] && candidate.Lyrics != nil && candidate.Lyrics.Value != "" {
		patch.Lyrics = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.Lyrics.Value}
	}
	if selected["comment"] && candidate.Comment.Value != "" {
		patch.Comment = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.Comment.Value}
	}
	if selected["composers"] && len(candidate.Composers.Value) > 0 {
		patch.Composers = &domain.StringsFieldPatch{Op: domain.OperationSet, Value: candidate.Composers.Value}
	}
	if selected["conductor"] && candidate.Conductor.Value != "" {
		patch.Conductor = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.Conductor.Value}
	}
	if selected["lyricists"] && len(candidate.Lyricists.Value) > 0 {
		patch.Lyricists = &domain.StringsFieldPatch{Op: domain.OperationSet, Value: candidate.Lyricists.Value}
	}
	if selected["copyright"] && candidate.Copyright.Value != "" {
		patch.Copyright = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.Copyright.Value}
	}
	if selected["bpm"] && candidate.BPM.Value > 0 {
		patch.BPM = &domain.IntFieldPatch{Op: domain.OperationSet, Value: candidate.BPM.Value}
	}
	if selected["isrc"] && candidate.ISRC.Value != "" {
		patch.ISRC = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.ISRC.Value}
	}
	if selected["musicbrainzTrackId"] && candidate.MusicBrainzTrackID.Value != "" {
		patch.MusicBrainzTrackID = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.MusicBrainzTrackID.Value}
	}
	if selected["musicbrainzReleaseId"] && candidate.MusicBrainzReleaseID.Value != "" {
		patch.MusicBrainzReleaseID = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.MusicBrainzReleaseID.Value}
	}
	if selected["musicbrainzArtistIds"] && len(candidate.MusicBrainzArtistIDs.Value) > 0 {
		patch.MusicBrainzArtistIDs = &domain.StringsFieldPatch{Op: domain.OperationSet, Value: candidate.MusicBrainzArtistIDs.Value}
	}
	if selected["acoustidId"] && candidate.AcoustID.Value != "" {
		patch.AcoustID = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.AcoustID.Value}
	}
	if selected["acoustidFingerprint"] && candidate.AcoustIDFingerprint.Value != "" {
		patch.AcoustIDFingerprint = &domain.StringFieldPatch{Op: domain.OperationSet, Value: candidate.AcoustIDFingerprint.Value}
	}
	return patch
}
