package main

import (
	"context"
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
