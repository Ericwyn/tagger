package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ericwyn/tagger/internal/artwork"
	"github.com/ericwyn/tagger/internal/config"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/jobs"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/providers/itunes"
	"github.com/ericwyn/tagger/internal/providers/kugou"
	"github.com/ericwyn/tagger/internal/providers/kuwo"
	"github.com/ericwyn/tagger/internal/providers/lrclib"
	"github.com/ericwyn/tagger/internal/providers/musicbrainz"
	"github.com/ericwyn/tagger/internal/providers/netease"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/server"
	"github.com/ericwyn/tagger/internal/store"
	"github.com/ericwyn/tagger/internal/tags/taglibwasm"
	"github.com/ericwyn/tagger/internal/version"
	"github.com/ericwyn/tagger/web"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	engine := taglibwasm.New()
	musicScanner, err := scanner.New(engine, scanner.Options{
		Root:        cfg.MusicDir,
		LibraryName: cfg.LibraryName,
		Workers:     cfg.ScanWorkers,
	})
	if err != nil {
		logger.Error("initialize scanner", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	dataStore, err := store.Open(ctx, filepath.Join(cfg.DataDir, "tagger.db"))
	if err != nil {
		cancel()
		logger.Error("initialize persistent store", "error", err)
		os.Exit(1)
	}
	defer dataStore.Close()
	libraryService, err := library.New(ctx, musicScanner, dataStore)
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
	providerRegistry := providers.NewRegistry(
		musicbrainz.New(musicbrainz.Config{}),
		lrclib.New(lrclib.Config{}),
		itunes.New(itunes.Config{}),
		netease.New(netease.Config{}),
		kugou.New(kugou.Config{}),
		kuwo.New(kuwo.Config{}),
	)
	if err := providerRegistry.SetPersistence(context.Background(), dataStore); err != nil {
		logger.Error("load provider persistence", "error", err)
		os.Exit(1)
	}
	jobManager := jobs.New(dataStore)
	jobManager.Register(domain.JobScan, func(ctx context.Context, _ domain.Job, progress jobs.Progress) error {
		before := libraryService.Library().TrackCount
		if err := progress(0, before, 0, 0, "正在发现并解析音乐文件"); err != nil {
			return err
		}
		if err := libraryService.Rescan(ctx); err != nil {
			return err
		}
		total := libraryService.Library().TrackCount
		return progress(total, total, total, 0, fmt.Sprintf("扫描完成，共索引 %d 首曲目", total))
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
			payload.Limit = 5
		}
		failed, succeeded := 0, 0
		for index, trackID := range payload.TrackIDs {
			track, err := libraryService.Track(trackID)
			if err != nil {
				failed++
				_ = dataStore.UpsertMatchItem(ctx, store.MatchItem{JobID: job.ID, TrackID: trackID, State: "failed", Error: err.Error()})
			} else {
				result, searchErr := providerRegistry.Search(ctx, providers.Query{Title: track.Title, Artists: track.Artists, Album: track.Album, DurationSeconds: track.DurationSeconds}, payload.ProviderIDs, payload.Limit)
				if searchErr != nil {
					failed++
					_ = dataStore.UpsertMatchItem(ctx, store.MatchItem{JobID: job.ID, TrackID: trackID, State: "failed", Error: searchErr.Error()})
				} else {
					state := "review"
					if len(result.Candidates) == 0 {
						state = "no_match"
						failed++
					} else {
						succeeded++
					}
					candidateJSON, _ := json.Marshal(result.Candidates)
					_ = dataStore.UpsertMatchItem(ctx, store.MatchItem{JobID: job.ID, TrackID: trackID, State: state, Candidates: candidateJSON})
				}
			}
			if err := progress(index+1, len(payload.TrackIDs), succeeded, failed, fmt.Sprintf("已分析 %d/%d 首曲目", index+1, len(payload.TrackIDs))); err != nil {
				return err
			}
		}
		if failed > 0 {
			return nil
		}
		return jobs.ErrNeedsReview
	})
	jobManager.Register(domain.JobWrite, func(ctx context.Context, job domain.Job, progress jobs.Progress) error {
		var payload struct {
			MatchJobID string `json:"matchJobId"`
			Items      []struct {
				TrackID      string   `json:"trackId"`
				CandidateID  string   `json:"candidateId"`
				BaseRevision string   `json:"baseRevision"`
				Fields       []string `json:"fields"`
				Artwork      bool     `json:"artwork"`
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
		failed := 0
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
				artworkTarget, err = prepareCandidateArtwork(ctx, providerRegistry, candidate, defaultArtworkDownloader)
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
						completed = append(completed, completedWrite{trackID: item.TrackID, candidate: candidate, tagResult: tagResult, artworkResult: artworkResult})
					}
				}
			}
			if err != nil {
				failed++
				state := "write_failed"
				if artworkFailedAfterTags {
					state = "artwork_failed"
				}
				if persistErr := dataStore.UpsertMatchItem(ctx, store.MatchItem{ID: matchItem.ID, JobID: payload.MatchJobID, TrackID: item.TrackID, State: state, Candidates: matchItem.Candidates, SelectedCandidateID: item.CandidateID, ReviewFields: append([]string(nil), item.Fields...), ReviewArtwork: item.Artwork, Error: err.Error()}); persistErr != nil {
					return fmt.Errorf("persist failed match item %s: %w", item.TrackID, persistErr)
				}
			} else {
				if persistErr := dataStore.UpsertMatchItem(ctx, store.MatchItem{ID: matchItem.ID, JobID: payload.MatchJobID, TrackID: item.TrackID, State: "written", Candidates: matchItem.Candidates, SelectedCandidateID: item.CandidateID, ReviewFields: append([]string(nil), item.Fields...), ReviewArtwork: item.Artwork}); persistErr != nil {
					return fmt.Errorf("persist written match item %s: %w", item.TrackID, persistErr)
				}
			}
			if err := progress(index+1, len(payload.Items), len(completed), failed, fmt.Sprintf("已写入 %d/%d 首曲目", index+1, len(payload.Items))); err != nil {
				return err
			}
		}
		if len(completed) > 0 {
			if err := libraryService.Rescan(ctx); err != nil {
				return err
			}
			for _, item := range completed {
				track, trackErr := libraryService.Track(item.trackID)
				if trackErr != nil {
					continue
				}
				descriptor, _ := providerRegistry.Descriptor(item.candidate.ProviderID)
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
				if _, historyErr := dataStore.CreateRevision(ctx, domain.Revision{LibraryID: libraryService.Library().ID, TrackID: track.ID, TrackTitle: track.Title, FileName: track.FileName, Action: action, Source: descriptor.Name, BaseRevision: baseRevision, ResultRevision: resultRevision, Diff: diff, CoverTone: track.CoverTone, BeforeTags: beforeTags, AfterTags: afterTags, BeforeArtwork: beforeArtwork, AfterArtwork: afterArtwork}); historyErr != nil {
					return fmt.Errorf("persist batch revision for %s: %w", item.trackID, historyErr)
				}
			}
		}
		return nil
	})
	jobManager.Register(domain.JobBatchEdit, newBatchEditHandler(libraryService, tagWriter, dataStore))
	if err := jobManager.Start(context.Background()); err != nil {
		logger.Error("start persistent job worker", "error", err)
		os.Exit(1)
	}
	defer jobManager.Close()

	srv := server.New(cfg.Listen, libraryService, tagWriter, providerRegistry, dataStore, web.Dist(), version.Version, engine.Version())
	srv.SetJobManager(jobManager)
	logger.Info("tagger started",
		"listen", cfg.Listen,
		"library", cfg.MusicDir,
		"data", dataStore.Path(),
		"tracks", libraryService.Library().TrackCount,
		"version", version.Version,
	)
	srv.Spin()
}

func newBatchEditHandler(libraryService *library.Service, tagWriter *filewrite.Writer, dataStore *store.Store) jobs.Handler {
	return func(ctx context.Context, job domain.Job, progress jobs.Progress) error {
		var payload domain.BatchEditPayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return err
		}
		completed := make([]struct {
			track  domain.Track
			result filewrite.Result
		}, 0, len(payload.Items))
		failed := 0
		for index, item := range payload.Items {
			track, err := libraryService.Track(item.TrackID)
			var result filewrite.Result
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
				}
			}
			state := "written"
			if err != nil {
				failed++
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
				completed = append(completed, struct {
					track  domain.Track
					result filewrite.Result
				}{track: track, result: result})
			}
			if progressErr := progress(index+1, len(payload.Items), len(completed), failed, fmt.Sprintf("已编辑 %d/%d 首曲目", index+1, len(payload.Items))); progressErr != nil {
				return progressErr
			}
		}
		if len(completed) == 0 {
			return nil
		}
		if err := libraryService.Rescan(ctx); err != nil {
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
			if _, err := dataStore.CreateRevision(ctx, domain.Revision{
				LibraryID: libraryService.Library().ID, TrackID: track.ID, TrackTitle: track.Title, FileName: track.FileName,
				Action: "批量编辑标签", Source: "批量编辑", BaseRevision: item.result.BaseRevision,
				ResultRevision: track.Revision, Diff: item.result.Diff, CoverTone: track.CoverTone,
				BeforeTags: item.result.BeforeTags, AfterTags: item.result.AfterTags,
			}); err != nil {
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
		case "album":
			patch.Album = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: strings.TrimSpace(operation.Value)}
		case "albumArtists":
			patch.AlbumArtists = &domain.StringsFieldPatch{Op: batchStringsOperation(operation.Mode), Value: batchListValue(operation.Mode, track.AlbumArtists, operation.Value)}
		case "genres":
			patch.Genres = &domain.StringsFieldPatch{Op: batchStringsOperation(operation.Mode), Value: batchListValue(operation.Mode, track.Genres, operation.Value)}
		case "comment":
			patch.Comment = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: strings.TrimSpace(operation.Value)}
		case "composers":
			patch.Composers = &domain.StringsFieldPatch{Op: batchStringsOperation(operation.Mode), Value: batchListValue(operation.Mode, track.Composers, operation.Value)}
		case "conductor":
			patch.Conductor = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: strings.TrimSpace(operation.Value)}
		case "lyricists":
			patch.Lyricists = &domain.StringsFieldPatch{Op: batchStringsOperation(operation.Mode), Value: batchListValue(operation.Mode, track.Lyricists, operation.Value)}
		case "copyright":
			patch.Copyright = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: strings.TrimSpace(operation.Value)}
		case "bpm":
			if operation.Mode == domain.BatchEditDelete {
				patch.BPM = &domain.IntFieldPatch{Op: domain.OperationDelete}
			} else if bpm, err := strconv.Atoi(strings.TrimSpace(operation.Value)); err == nil && bpm > 0 {
				patch.BPM = &domain.IntFieldPatch{Op: domain.OperationSet, Value: bpm}
			}
		case "isrc":
			patch.ISRC = &domain.StringFieldPatch{Op: batchStringOperation(operation.Mode), Value: strings.TrimSpace(operation.Value)}
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

func batchStringsOperation(mode domain.BatchEditMode) domain.Operation {
	if mode == domain.BatchEditDelete {
		return domain.OperationDelete
	}
	return domain.OperationSet
}

func batchListValue(mode domain.BatchEditMode, current []string, value string) []string {
	if mode == domain.BatchEditDelete {
		return nil
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

func defaultArtworkDownloader(ctx context.Context, reference providers.ArtworkReference) (artwork.Asset, error) {
	return providers.DownloadArtwork(ctx, reference, nil)
}

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
	reference, err := registry.ArtworkReference(candidate.ID)
	if err != nil {
		return nil, fmt.Errorf("候选封面不可用：%w", err)
	}
	asset, err := download(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("下载候选封面：%w", err)
	}
	return &asset, nil
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
