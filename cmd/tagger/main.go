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
	"github.com/ericwyn/tagger/internal/providers/lrclib"
	"github.com/ericwyn/tagger/internal/providers/musicbrainz"
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
		providers.NewPlaceholder(providers.Descriptor{
			ID: "netease", Name: "网易云音乐", ShortName: "NE", Description: "中文曲库、歌词与发行信息",
			Capabilities: []string{"歌曲", "专辑", "歌词", "封面"}, Health: providers.HealthDisabled,
			Enabled: false, Experimental: true, Accent: "#d62d20", QuotaLabel: "实验性适配器 · 尚未启用",
		}),
		providers.NewPlaceholder(providers.Descriptor{
			ID: "kuwo", Name: "酷我音乐", ShortName: "KW", Description: "中文曲库补充来源",
			Capabilities: []string{"歌曲", "歌词", "封面"}, Health: providers.HealthDisabled,
			Enabled: false, Experimental: true, Accent: "#d69e2e", QuotaLabel: "实验性适配器 · 尚未启用",
		}),
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
