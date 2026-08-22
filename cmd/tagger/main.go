package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/ericwyn/tagger/internal/config"
	"github.com/ericwyn/tagger/internal/filewrite"
	"github.com/ericwyn/tagger/internal/library"
	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/providers/itunes"
	"github.com/ericwyn/tagger/internal/providers/lrclib"
	"github.com/ericwyn/tagger/internal/providers/musicbrainz"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/server"
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
	libraryService, err := library.New(ctx, musicScanner)
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

	srv := server.New(cfg.Listen, libraryService, tagWriter, providerRegistry, web.Dist(), version.Version, engine.Version())
	logger.Info("tagger started",
		"listen", cfg.Listen,
		"library", cfg.MusicDir,
		"tracks", libraryService.Library().TrackCount,
		"version", version.Version,
	)
	srv.Spin()
}
