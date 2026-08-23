package config

import (
	"flag"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen            string
	MusicDir          string
	DataDir           string
	LibraryName       string
	AuthToken         string
	ScanWorkers       int
	WatcherWait       time.Duration
	ReconcileInterval time.Duration
}

func Parse(args []string, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	workers := min(runtime.NumCPU(), 8)
	if value := strings.TrimSpace(getenv("TAGGER_SCAN_WORKERS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			workers = parsed
		}
	}
	watcherWait := 5 * time.Second
	if value := strings.TrimSpace(getenv("TAGGER_WATCHER_WAIT")); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("invalid TAGGER_WATCHER_WAIT: %w", err)
		}
		watcherWait = parsed
	}
	reconcileInterval := time.Duration(0)
	if value := strings.TrimSpace(getenv("TAGGER_RECONCILE_INTERVAL")); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("invalid TAGGER_RECONCILE_INTERVAL: %w", err)
		}
		reconcileInterval = parsed
	}

	cfg := Config{
		Listen:            valueOr(getenv("TAGGER_LISTEN"), "127.0.0.1:8080"),
		MusicDir:          strings.TrimSpace(getenv("TAGGER_MUSIC_DIR")),
		DataDir:           valueOr(getenv("TAGGER_DATA_DIR"), "./data"),
		LibraryName:       strings.TrimSpace(getenv("TAGGER_LIBRARY_NAME")),
		AuthToken:         strings.TrimSpace(getenv("TAGGER_AUTH_TOKEN")),
		ScanWorkers:       workers,
		WatcherWait:       watcherWait,
		ReconcileInterval: reconcileInterval,
	}
	flags := flag.NewFlagSet("tagger", flag.ContinueOnError)
	flags.StringVar(&cfg.Listen, "listen", cfg.Listen, "HTTP listen address")
	flags.StringVar(&cfg.MusicDir, "music-dir", cfg.MusicDir, "music library root directory")
	flags.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "persistent application data directory")
	flags.StringVar(&cfg.LibraryName, "library-name", cfg.LibraryName, "display name for the music library")
	flags.StringVar(&cfg.AuthToken, "auth-token", cfg.AuthToken, "optional bearer token for API access")
	flags.IntVar(&cfg.ScanWorkers, "scan-workers", cfg.ScanWorkers, "parallel metadata readers (1-32)")
	flags.DurationVar(&cfg.WatcherWait, "watcher-wait", cfg.WatcherWait, "debounce delay for filesystem changes")
	flags.DurationVar(&cfg.ReconcileInterval, "reconcile-interval", cfg.ReconcileInterval, "optional periodic incremental reconciliation (0 disables)")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	cfg.MusicDir = strings.TrimSpace(cfg.MusicDir)
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	cfg.AuthToken = strings.TrimSpace(cfg.AuthToken)
	if cfg.Listen == "" {
		return Config{}, fmt.Errorf("listen address cannot be empty")
	}
	if cfg.DataDir == "" {
		return Config{}, fmt.Errorf("data directory cannot be empty")
	}
	if cfg.ScanWorkers < 1 || cfg.ScanWorkers > 32 {
		return Config{}, fmt.Errorf("scan workers must be between 1 and 32")
	}
	if cfg.WatcherWait < 0 || cfg.WatcherWait > 10*time.Minute {
		return Config{}, fmt.Errorf("watcher wait must be between 0 and 10 minutes")
	}
	if cfg.ReconcileInterval < 0 || cfg.ReconcileInterval > 30*24*time.Hour {
		return Config{}, fmt.Errorf("reconcile interval must be between 0 and 720 hours")
	}
	return cfg, nil
}

func valueOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
