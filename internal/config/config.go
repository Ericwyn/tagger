package config

import (
	"flag"
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	Listen      string
	MusicDir    string
	LibraryName string
	ScanWorkers int
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

	cfg := Config{
		Listen:      valueOr(getenv("TAGGER_LISTEN"), "127.0.0.1:8080"),
		MusicDir:    strings.TrimSpace(getenv("TAGGER_MUSIC_DIR")),
		LibraryName: strings.TrimSpace(getenv("TAGGER_LIBRARY_NAME")),
		ScanWorkers: workers,
	}
	flags := flag.NewFlagSet("tagger", flag.ContinueOnError)
	flags.StringVar(&cfg.Listen, "listen", cfg.Listen, "HTTP listen address")
	flags.StringVar(&cfg.MusicDir, "music-dir", cfg.MusicDir, "music library root directory")
	flags.StringVar(&cfg.LibraryName, "library-name", cfg.LibraryName, "display name for the music library")
	flags.IntVar(&cfg.ScanWorkers, "scan-workers", cfg.ScanWorkers, "parallel metadata readers (1-32)")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	cfg.MusicDir = strings.TrimSpace(cfg.MusicDir)
	if cfg.Listen == "" {
		return Config{}, fmt.Errorf("listen address cannot be empty")
	}
	if cfg.MusicDir == "" {
		return Config{}, fmt.Errorf("music directory is required; pass --music-dir or TAGGER_MUSIC_DIR")
	}
	if cfg.ScanWorkers < 1 || cfg.ScanWorkers > 32 {
		return Config{}, fmt.Errorf("scan workers must be between 1 and 32")
	}
	return cfg, nil
}

func valueOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
