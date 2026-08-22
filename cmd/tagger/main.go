package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ericwyn/tagger/internal/config"
	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/jobs"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/providers/itunes"
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
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return err
		}
		type completedWrite struct {
			trackID   string
			candidate providers.MatchCandidate
			result    filewrite.Result
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
			if err == nil {
				ref, refErr := libraryService.FileRef(item.TrackID)
				if refErr != nil {
					err = refErr
				} else {
					baseRevision := item.BaseRevision
					if baseRevision == "" {
						baseRevision = track.Revision
					}
					result, writeErr := tagWriter.Write(ctx, ref, baseRevision, patchFromCandidate(candidate, item.Fields), false)
					if writeErr != nil {
						err = writeErr
					} else {
						completed = append(completed, completedWrite{trackID: item.TrackID, candidate: candidate, result: result})
					}
				}
			}
			if err != nil {
				failed++
				_ = dataStore.UpsertMatchItem(ctx, store.MatchItem{ID: matchItem.ID, JobID: payload.MatchJobID, TrackID: item.TrackID, State: "write_failed", Candidates: matchItem.Candidates, SelectedCandidateID: item.CandidateID, Error: err.Error()})
			} else {
				_ = dataStore.UpsertMatchItem(ctx, store.MatchItem{ID: matchItem.ID, JobID: payload.MatchJobID, TrackID: item.TrackID, State: "written", Candidates: matchItem.Candidates, SelectedCandidateID: item.CandidateID})
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
				_, _ = dataStore.CreateRevision(ctx, domain.Revision{LibraryID: libraryService.Library().ID, TrackID: track.ID, TrackTitle: track.Title, FileName: track.FileName, Action: "批量采用候选标签", Source: descriptor.Name, BaseRevision: item.result.BaseRevision, ResultRevision: track.Revision, Diff: item.result.Diff, CoverTone: track.CoverTone, BeforeTags: item.result.BeforeTags, AfterTags: item.result.AfterTags})
			}
		}
		return nil
	})
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

func patchFromCandidate(candidate providers.MatchCandidate, fields []string) domain.TagPatch {
	selected := make(map[string]bool, len(fields))
	if len(fields) == 0 {
		for _, field := range []string{"title", "artists", "album", "albumArtists", "trackNumber", "trackTotal", "discNumber", "year", "genres", "lyrics"} {
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
	return patch
}
