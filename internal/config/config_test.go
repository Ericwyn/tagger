package config

import (
	"strings"
	"testing"
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

func TestParseRejectsMissingLibrary(t *testing.T) {
	_, err := Parse(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "music directory is required") {
		t.Fatalf("error = %v, want missing music directory", err)
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
