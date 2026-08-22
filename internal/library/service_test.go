package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/tags"
)

type serviceEngine struct{}

func (serviceEngine) Read(_ context.Context, path string) (tags.Snapshot, error) {
	name := filepath.Base(path)
	return tags.Snapshot{
		Raw: map[string][]string{
			"TITLE":  {name[:len(name)-len(filepath.Ext(name))]},
			"ARTIST": {"测试歌手"},
		},
		DurationSeconds: 120,
		ArtworkCount:    1,
	}, nil
}

func (serviceEngine) Write(context.Context, string, map[string][]string) error { return nil }

func (serviceEngine) Version() string { return "test" }

func TestServiceFiltersAndReturnsDefensiveCopies(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Alpha.mp3", "Beta.flac"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	musicScanner, err := scanner.New(serviceEngine{}, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), musicScanner)
	if err != nil {
		t.Fatal(err)
	}

	flac := service.ListTracks(TrackFilter{Format: domain.FormatFLAC})
	if len(flac) != 1 || flac[0].Title != "Beta" {
		t.Fatalf("flac filter = %#v", flac)
	}
	search := service.ListTracks(TrackFilter{Query: "alpha"})
	if len(search) != 1 || search[0].Title != "Alpha" {
		t.Fatalf("search = %#v", search)
	}

	all := service.ListTracks(TrackFilter{})
	all[0].Artists[0] = "mutated"
	again := service.ListTracks(TrackFilter{})
	if again[0].Artists[0] != "测试歌手" {
		t.Fatalf("service leaked mutable state: %#v", again[0])
	}
	if _, err := service.Track("missing"); err != ErrTrackNotFound {
		t.Fatalf("missing error = %v", err)
	}
}
