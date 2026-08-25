package config

import (
	"strings"
	"testing"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
)

func TestParseUsesEnvironmentAndFlags(t *testing.T) {
	environment := map[string]string{
		"TAGGER_LISTEN":       "0.0.0.0:9000",
		"TAGGER_MUSIC_DIR":    "/music/from-env",
		"TAGGER_DATA_DIR":     "/var/lib/tagger",
		"TAGGER_SCAN_WORKERS": "3",
	}
	cfg, err := Parse([]string{"--music-dir", "/music/from-flag", "--library-name", "Archive", "--auth-token", "secret"}, func(key string) string {
		return environment[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0:9000" || cfg.MusicDir != "/music/from-flag" || cfg.DataDir != "/var/lib/tagger" || cfg.LibraryName != "Archive" || cfg.AuthToken != "secret" || cfg.ScanWorkers != 3 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if cfg.WatcherWait != 5*time.Second {
		t.Fatalf("watcher wait = %s, want 5s", cfg.WatcherWait)
	}
	if cfg.ReconcileInterval != 0 {
		t.Fatalf("reconcile interval = %s, want disabled by default", cfg.ReconcileInterval)
	}
	if cfg.WatchMode != domain.WatchModeAuto {
		t.Fatalf("watch mode=%q, want auto", cfg.WatchMode)
	}
}

func TestParseUsesDefaultDataDirectory(t *testing.T) {
	cfg, err := Parse([]string{"--music-dir", "/music"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "./data" {
		t.Fatalf("data directory = %q, want ./data", cfg.DataDir)
	}
}

func TestParseAllowsMissingLibraryForPersistedSelection(t *testing.T) {
	cfg, err := Parse(nil, nil)
	if err != nil || cfg.MusicDir != "" {
		t.Fatalf("config = %#v err=%v, want an empty directory for persisted selection", cfg, err)
	}
}

func TestParseRejectsUnsafeWorkerCount(t *testing.T) {
	_, err := Parse([]string{"--music-dir", "/music", "--scan-workers", "33"}, nil)
	if err == nil || !strings.Contains(err.Error(), "between 1 and 32") {
		t.Fatalf("error = %v, want worker limit", err)
	}
}

func TestParseRejectsEmptyDataDirectory(t *testing.T) {
	_, err := Parse([]string{"--music-dir", "/music", "--data-dir", "  "}, nil)
	if err == nil || !strings.Contains(err.Error(), "data directory cannot be empty") {
		t.Fatalf("error = %v, want empty data directory", err)
	}
}

func TestParseWatcherWaitFromEnvironmentAndRejectsInvalidValues(t *testing.T) {
	cfg, err := Parse(nil, func(key string) string {
		if key == "TAGGER_WATCHER_WAIT" {
			return "12s"
		}
		return ""
	})
	if err != nil || cfg.WatcherWait != 12*time.Second {
		t.Fatalf("config=%#v err=%v", cfg, err)
	}
	_, err = Parse(nil, func(key string) string {
		if key == "TAGGER_WATCHER_WAIT" {
			return "not-a-duration"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "TAGGER_WATCHER_WAIT") {
		t.Fatalf("invalid watcher wait error=%v", err)
	}
}

func TestParseReconcileInterval(t *testing.T) {
	cfg, err := Parse(nil, func(key string) string {
		if key == "TAGGER_RECONCILE_INTERVAL" {
			return "24h"
		}
		return ""
	})
	if err != nil || cfg.ReconcileInterval != 24*time.Hour {
		t.Fatalf("config=%#v err=%v", cfg, err)
	}
	_, err = Parse(nil, func(key string) string {
		if key == "TAGGER_RECONCILE_INTERVAL" {
			return "800h"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "reconcile interval") {
		t.Fatalf("out of range reconcile interval error=%v", err)
	}
}

func TestParseWatchMode(t *testing.T) {
	cfg, err := Parse([]string{"--watch-mode", "poll"}, nil)
	if err != nil || cfg.WatchMode != domain.WatchModePoll {
		t.Fatalf("config=%#v err=%v", cfg, err)
	}
	_, err = Parse([]string{"--watch-mode", "unknown"}, nil)
	if err == nil || !strings.Contains(err.Error(), "watch mode") {
		t.Fatalf("invalid watch mode error=%v", err)
	}
}
