package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerDebouncesFilesystemEvents(t *testing.T) {
	root := t.TempDir()
	changes := make(chan []string, 2)
	manager := New(40*time.Millisecond, func(_ context.Context, targets []string) error {
		changes <- targets
		return nil
	})
	if err := manager.Start(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	path := filepath.Join(root, "song.mp3")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case targets := <-changes:
		if len(targets) != 1 || targets[0] != "." {
			t.Fatalf("targets=%v, want root target", targets)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not emit a debounced event")
	}
	select {
	case extra := <-changes:
		t.Fatalf("unexpected second debounced batch: %v", extra)
	case <-time.After(120 * time.Millisecond):
	}
}

func TestIgnoredPaths(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "music")
	for _, path := range []string{filepath.Join(root, ".hidden", "song.mp3"), filepath.Join(root, "@eaDir", "song.mp3"), filepath.Join(root, ".DS_Store")} {
		if !isIgnored(path, root) {
			t.Fatalf("path %q was not ignored", path)
		}
	}
	if isIgnored(filepath.Join(root, "Artist", "song.mp3"), root) {
		t.Fatal("regular audio path was ignored")
	}
}

func TestManagerRetriesBusyBatch(t *testing.T) {
	root := t.TempDir()
	changes := make(chan []string, 1)
	attempts := 0
	manager := New(10*time.Millisecond, func(_ context.Context, targets []string) error {
		attempts++
		if attempts == 1 {
			return ErrBusy
		}
		changes <- targets
		return nil
	})
	if err := manager.Start(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case targets := <-changes:
		if len(targets) != 1 || targets[0] != "." {
			t.Fatalf("targets=%v, want retried root target", targets)
		}
		if attempts != 2 {
			t.Fatalf("callback attempts=%d, want 2", attempts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("busy watcher batch was not retried")
	}
}
