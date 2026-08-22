package library

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/ericwyn/tagger/internal/store"
	"github.com/ericwyn/tagger/internal/tags"
)

type serviceEngine struct{}

type countingServiceEngine struct{ reads atomic.Int32 }

func (engine *countingServiceEngine) Read(ctx context.Context, path string) (tags.Snapshot, error) {
	engine.reads.Add(1)
	return serviceEngine{}.Read(ctx, path)
}

func (*countingServiceEngine) Write(context.Context, string, map[string][]string) error { return nil }

func (*countingServiceEngine) Version() string { return "counting-test" }

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

func TestServiceLoadsPersistedIndexAndRescansOnDemand(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Persisted.mp3"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	engine := &countingServiceEngine{}
	musicScanner, err := scanner.New(engine, scanner.Options{Root: root, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	dataStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })

	first, err := New(context.Background(), musicScanner, dataStore)
	if err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 1 || first.Library().TrackCount != 1 {
		t.Fatalf("initial scan reads=%d tracks=%d", engine.reads.Load(), first.Library().TrackCount)
	}
	second, err := New(context.Background(), musicScanner, dataStore)
	if err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 1 || second.Library().TrackCount != 1 {
		t.Fatalf("persisted load unexpectedly rescanned: reads=%d tracks=%d", engine.reads.Load(), second.Library().TrackCount)
	}
	if err := second.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if engine.reads.Load() != 2 {
		t.Fatalf("explicit rescan reads=%d, want 2", engine.reads.Load())
	}
}
