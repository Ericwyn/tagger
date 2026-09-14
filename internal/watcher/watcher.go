package watcher

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/fsnotify/fsnotify"
)

type Callback func(context.Context, []string) error

// ErrBusy tells the watcher that a callback's work is temporarily blocked by
// another durable task. The affected directory batch is retained and retried
// after a short delay instead of being silently dropped.
var ErrBusy = errors.New("watcher callback busy")

type Manager struct {
	wait     time.Duration
	callback Callback

	mu       sync.Mutex
	cancel   context.CancelFunc
	fsw      *fsnotify.Watcher
	root     string
	watchErr chan error
}

func New(wait time.Duration, callback Callback) *Manager {
	if wait < 0 {
		wait = 5 * time.Second
	}
	return &Manager{wait: wait, callback: callback, watchErr: make(chan error, 1)}
}

func (m *Manager) Start(ctx context.Context, root string) error {
	m.Stop()
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create library watcher: %w", err)
	}
	root, err = filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		_ = fsw.Close()
		return err
	}
	if err := addDirectories(fsw, root); err != nil {
		_ = fsw.Close()
		return fmt.Errorf("watch library directories: %w", err)
	}
	watchCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.fsw = fsw
	m.root = root
	m.mu.Unlock()
	go m.loop(watchCtx, fsw, root)
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	fsw := m.fsw
	m.cancel = nil
	m.fsw = nil
	m.root = ""
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if fsw != nil {
		_ = fsw.Close()
	}
}

func (m *Manager) Errors() <-chan error { return m.watchErr }

func (m *Manager) loop(ctx context.Context, fsw *fsnotify.Watcher, root string) {
	var timer *time.Timer
	var timerC <-chan time.Time
	targets := make(map[string]struct{})
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case event, ok := <-fsw.Events:
			if !ok {
				return
			}
			if isIgnored(event.Name, root) {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := filepath.Abs(event.Name); err == nil {
					if stat, statErr := fsStat(info); statErr == nil && stat.IsDir() {
						if addErr := addDirectories(fsw, info); addErr != nil {
							select {
							case m.watchErr <- fmt.Errorf("watch new library directory: %w", addErr):
							default:
							}
						}
					}
				}
			}
			if info, statErr := fsStat(event.Name); statErr == nil && !info.IsDir() && !isWatchedFile(event.Name) {
				continue
			}
			if _, statErr := fsStat(event.Name); statErr != nil && event.Op&(fsnotify.Remove|fsnotify.Rename) == 0 && !isWatchedFile(event.Name) {
				continue
			}
			target := eventFolder(event.Name, root)
			if target == "" {
				continue
			}
			targets[target] = struct{}{}
			if timer == nil {
				timer = time.NewTimer(m.wait)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(m.wait)
			}
			timerC = timer.C
		case <-timerC:
			batch := make([]string, 0, len(targets))
			for target := range targets {
				batch = append(batch, target)
			}
			targets = make(map[string]struct{})
			timerC = nil
			if len(batch) > 0 && m.callback != nil {
				if err := m.callback(ctx, batch); err != nil {
					if errors.Is(err, ErrBusy) {
						for _, target := range batch {
							targets[target] = struct{}{}
						}
						if timer == nil {
							timer = time.NewTimer(retryDelay(m.wait))
						} else {
							timer.Reset(retryDelay(m.wait))
						}
						timerC = timer.C
					} else {
						select {
						case m.watchErr <- err:
						default:
						}
					}
				}
			}
		case err, ok := <-fsw.Errors:
			if ok && err != nil {
				select {
				case m.watchErr <- err:
				default:
				}
			}
		}
	}
}

func retryDelay(wait time.Duration) time.Duration {
	const minimum = 100 * time.Millisecond
	if wait < minimum {
		return minimum
	}
	return wait
}

func addDirectories(fsw *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != root && ignoredDirectory(entry.Name()) {
				return fs.SkipDir
			}
			return fsw.Add(path)
		}
		return nil
	})
}

func fsStat(path string) (fs.FileInfo, error) { return os.Stat(path) }

func eventFolder(path, root string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "."
	}
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(filepath.Dir(relative))
}

func isIgnored(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return true
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if ignoredDirectory(part) || strings.HasPrefix(part, ".") {
			return true
		}
	}
	base := filepath.Base(path)
	return base == ".DS_Store" || strings.HasPrefix(base, ".")
}

func ignoredDirectory(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "@eadir", "$recycle.bin", "system volume information":
		return true
	default:
		return false
	}
}

func isWatchedFile(path string) bool {
	extension := filepath.Ext(path)
	if strings.EqualFold(extension, ".lrc") {
		return true
	}
	_, supported := domain.TrackFormatFromExtension(extension)
	return supported
}
