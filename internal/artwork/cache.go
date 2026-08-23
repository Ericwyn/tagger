package artwork

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// DefaultCacheTTL deliberately stays short. It prevents repeated provider
	// image requests during a work session without turning the cache into a
	// second, permanent music library.
	DefaultCacheTTL = 3 * time.Hour
	cacheFileSuffix = ".img"
)

// CacheStats describes the files owned by the runtime artwork cache. Only
// validated image files and temporary files created by this package are
// included; unrelated files in the data directory are left untouched.
type CacheStats struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// Cache stores validated provider artwork outside the SQLite database. The
// cache key is derived from the URL with query parameters and fragments
// removed, so size/format hints cannot cause a burst of duplicate downloads.
// File modification time is used as the creation/refresh timestamp because
// it is portable across the filesystems supported by Go.
type Cache struct {
	dir   string
	ttl   time.Duration
	now   func() time.Time
	group singleflight.Group
	mu    sync.Mutex
}

// NewCache creates an artwork cache directory and removes stale entries once
// at startup. A positive TTL is required so an accidental zero value cannot
// make every lookup immediately stale.
func NewCache(dir string, ttl time.Duration) (*Cache, error) {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("artwork cache directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create artwork cache directory: %w", err)
	}
	c := &Cache{dir: dir, ttl: ttl, now: time.Now}
	_ = c.Cleanup(context.Background())
	return c, nil
}

// Start periodically removes entries that have aged beyond the cache TTL.
// The caller owns ctx and can cancel it during server shutdown.
func (c *Cache) Start(ctx context.Context, interval time.Duration) {
	if c == nil {
		return
	}
	if interval <= 0 {
		interval = c.ttl / 2
		if interval < time.Minute {
			interval = time.Minute
		}
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = c.Cleanup(ctx)
			}
		}
	}()
}

// Dir returns the cache directory used by this instance.
func (c *Cache) Dir() string {
	if c == nil {
		return ""
	}
	return c.dir
}

// Stats returns the current size of cache-owned files.
func (c *Cache) Stats(ctx context.Context) (CacheStats, error) {
	return c.scan(ctx, false)
}

// Clear removes all cache-owned images and temporary files and returns the
// amount of data removed. It intentionally does not remove the directory so
// subsequent provider requests can continue to use the cache immediately.
func (c *Cache) Clear(ctx context.Context) (CacheStats, error) {
	if c == nil {
		return CacheStats{}, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.scanLocked(ctx, true)
}

// NormalizeURL removes query parameters and fragments while preserving the
// canonical scheme/host/path used by the provider CDN. It intentionally does
// not broaden URL safety; providers still validate the original URL before a
// network request is made.
func NormalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid artwork URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String(), nil
}

func Key(raw string) (string, error) {
	normalized, err := NormalizeURL(raw)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(digest[:]), nil
}

// Get returns a fresh cached asset or invokes fetch once for concurrent
// callers. The supplied fetch function remains responsible for provider URL
// allowlisting, redirects, rate limits and payload validation.
func (c *Cache) Get(ctx context.Context, sourceURL string, fetch func() (Asset, error)) (Asset, error) {
	if fetch == nil {
		return Asset{}, errors.New("artwork cache fetch function is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	if c == nil {
		return fetch()
	}
	key, err := Key(sourceURL)
	if err != nil {
		return fetch()
	}
	if asset, ok := c.readFresh(key); ok {
		return asset, nil
	}
	result, err, _ := c.group.Do(key, func() (any, error) {
		if asset, ok := c.readFresh(key); ok {
			return asset, nil
		}
		asset, err := fetch()
		if err != nil {
			return Asset{}, err
		}
		if err := c.write(key, asset.Data); err != nil {
			// A cache filesystem failure must not make an otherwise successful
			// artwork probe fail; the caller can still use the downloaded asset.
			return asset, nil
		}
		return asset, nil
	})
	if err != nil {
		return Asset{}, err
	}
	asset, ok := result.(Asset)
	if !ok {
		return Asset{}, errors.New("artwork cache returned an invalid asset")
	}
	return asset, nil
}

func (c *Cache) filePath(key string) string {
	return filepath.Join(c.dir, key+cacheFileSuffix)
}

func (c *Cache) readFresh(key string) (Asset, bool) {
	path := c.filePath(key)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return Asset{}, false
	}
	now := c.now()
	age := now.Sub(info.ModTime())
	if age < 0 || age > c.ttl {
		if age > c.ttl {
			_ = os.Remove(path)
		}
		return Asset{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Asset{}, false
	}
	asset, err := Validate(data, "")
	if err != nil {
		_ = os.Remove(path)
		return Asset{}, false
	}
	return asset, true
}

func (c *Cache) write(key string, data []byte) error {
	if len(data) == 0 {
		return errors.New("artwork cache cannot store empty data")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(c.dir, ".artwork-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, c.filePath(key))
}

// Cleanup removes expired images and abandoned temporary files. It is safe to
// call while lookups are in progress because writes are atomic renames.
func (c *Cache) Cleanup(ctx context.Context) error {
	if c == nil {
		return nil
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	now := c.now()
	var cleanupErr error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, cacheFileSuffix) && !strings.HasSuffix(name, ".tmp") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}
		if now.Sub(info.ModTime()) <= c.ttl {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func (c *Cache) scan(ctx context.Context, remove bool) (CacheStats, error) {
	if c == nil {
		return CacheStats{}, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.scanLocked(ctx, remove)
}

func (c *Cache) scanLocked(ctx context.Context, remove bool) (CacheStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CacheStats{}, nil
		}
		return CacheStats{}, err
	}
	var result CacheStats
	var scanErr error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, cacheFileSuffix) && !strings.HasSuffix(name, ".tmp") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			scanErr = errors.Join(scanErr, infoErr)
			continue
		}
		result.Files++
		result.Bytes += info.Size()
		if remove {
			if removeErr := os.Remove(filepath.Join(c.dir, name)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				scanErr = errors.Join(scanErr, removeErr)
			}
		}
	}
	return result, scanErr
}
