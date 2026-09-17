package config

import (
	"flag"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
)

type Config struct {
	Listen            string
	MusicDir          string
	DataDir           string
	LibraryName       string
	AuthToken         string
	ScanWorkers       int
	WatchMode         domain.WatchMode
	WatcherWait       time.Duration
	ReconcileInterval time.Duration
	TestProviders     bool
	TestTitle         string
	TestArtists       string
	TestAlbum         string
	TestDuration      int64
	TestLimit         int
	TestArtwork       bool
	TestJSON          bool
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
		WatchMode:         domain.WatchMode(valueOr(getenv("TAGGER_WATCH_MODE"), string(domain.WatchModeAuto))),
		WatcherWait:       watcherWait,
		ReconcileInterval: reconcileInterval,
		TestTitle:         "最佳歌手",
		TestArtists:       "许嵩",
		TestLimit:         1,
		TestArtwork:       true,
	}
	flags := flag.NewFlagSet("tagger", flag.ContinueOnError)
	flags.StringVar(&cfg.Listen, "listen", cfg.Listen, "HTTP listen address")
	flags.StringVar(&cfg.MusicDir, "music-dir", cfg.MusicDir, "music library root directory")
	flags.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "persistent application data directory")
	flags.StringVar(&cfg.LibraryName, "library-name", cfg.LibraryName, "display name for the music library")
	flags.StringVar(&cfg.AuthToken, "auth-token", cfg.AuthToken, "optional bearer token for API access")
	flags.IntVar(&cfg.ScanWorkers, "scan-workers", cfg.ScanWorkers, "parallel metadata readers (1-32)")
	flags.Var((*watchModeValue)(&cfg.WatchMode), "watch-mode", "filesystem update mode: auto, events, or poll")
	flags.DurationVar(&cfg.WatcherWait, "watcher-wait", cfg.WatcherWait, "debounce delay for filesystem changes")
	flags.DurationVar(&cfg.ReconcileInterval, "reconcile-interval", cfg.ReconcileInterval, "optional periodic incremental reconciliation (0 disables)")
	flags.BoolVar(&cfg.TestProviders, "test-providers", false, "test every built-in metadata provider and exit")
	flags.StringVar(&cfg.TestTitle, "test-title", cfg.TestTitle, "provider test song title")
	flags.StringVar(&cfg.TestArtists, "test-artists", cfg.TestArtists, "comma-separated provider test artists")
	flags.StringVar(&cfg.TestAlbum, "test-album", "", "optional provider test album")
	flags.Int64Var(&cfg.TestDuration, "test-duration", 0, "optional provider test duration in seconds")
	flags.IntVar(&cfg.TestLimit, "test-limit", cfg.TestLimit, "candidate limit per provider (1-10)")
	flags.BoolVar(&cfg.TestArtwork, "test-artwork", cfg.TestArtwork, "download and validate candidate artwork")
	flags.BoolVar(&cfg.TestJSON, "test-json", false, "write provider test results as JSON")
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
	cfg.TestTitle = strings.TrimSpace(cfg.TestTitle)
	cfg.TestArtists = strings.TrimSpace(cfg.TestArtists)
	cfg.TestAlbum = strings.TrimSpace(cfg.TestAlbum)
	if cfg.Listen == "" {
		return Config{}, fmt.Errorf("listen address cannot be empty")
	}
	if cfg.DataDir == "" {
		return Config{}, fmt.Errorf("data directory cannot be empty")
	}
	if cfg.ScanWorkers < 1 || cfg.ScanWorkers > 32 {
		return Config{}, fmt.Errorf("scan workers must be between 1 and 32")
	}
	switch cfg.WatchMode {
	case domain.WatchModeAuto, domain.WatchModeEvents, domain.WatchModePoll:
	default:
		return Config{}, fmt.Errorf("watch mode must be auto, events, or poll")
	}
	if cfg.WatcherWait < 0 || cfg.WatcherWait > 10*time.Minute {
		return Config{}, fmt.Errorf("watcher wait must be between 0 and 10 minutes")
	}
	if cfg.ReconcileInterval < 0 || cfg.ReconcileInterval > 30*24*time.Hour {
		return Config{}, fmt.Errorf("reconcile interval must be between 0 and 720 hours")
	}
	if cfg.TestProviders {
		if cfg.TestTitle == "" {
			return Config{}, fmt.Errorf("provider test title cannot be empty")
		}
		if cfg.TestDuration < 0 {
			return Config{}, fmt.Errorf("provider test duration cannot be negative")
		}
		if cfg.TestLimit < 1 || cfg.TestLimit > 10 {
			return Config{}, fmt.Errorf("provider test limit must be between 1 and 10")
		}
	}
	return cfg, nil
}

type watchModeValue domain.WatchMode

func (value *watchModeValue) String() string { return string(*value) }

func (value *watchModeValue) Set(raw string) error {
	*value = watchModeValue(strings.TrimSpace(raw))
	return nil
}

func valueOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
