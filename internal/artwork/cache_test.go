package artwork

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 25, G: 80, B: 140, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buffer.Bytes()
}

func TestNormalizeURLAndKeyIgnoreQueryAndFragment(t *testing.T) {
	first, err := NormalizeURL("HTTPS://IS1-SSL.MZSTATIC.COM/image/cover.jpg?param=1000y#preview")
	if err != nil {
		t.Fatalf("normalize artwork URL: %v", err)
	}
	if first != "https://is1-ssl.mzstatic.com/image/cover.jpg" {
		t.Fatalf("unexpected normalized URL %q", first)
	}
	firstKey, err := Key("https://cdn.example/cover.jpg?width=500")
	if err != nil {
		t.Fatalf("first key: %v", err)
	}
	secondKey, err := Key("https://cdn.example/cover.jpg?width=1000")
	if err != nil {
		t.Fatalf("second key: %v", err)
	}
	if firstKey != secondKey {
		t.Fatalf("query parameters must not change cache key: %s != %s", firstKey, secondKey)
	}
}

func TestCacheReusesValidatedArtworkAcrossQueryVariants(t *testing.T) {
	cache, err := NewCache(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	data := testPNG(t)
	var calls atomic.Int32
	fetch := func() (Asset, error) {
		calls.Add(1)
		return Validate(data, "")
	}
	first, err := cache.Get(context.Background(), "https://cdn.example/cover.jpg?size=500", fetch)
	if err != nil {
		t.Fatalf("first cache get: %v", err)
	}
	second, err := cache.Get(context.Background(), "https://cdn.example/cover.jpg?size=1000", fetch)
	if err != nil {
		t.Fatalf("second cache get: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one remote fetch, got %d", calls.Load())
	}
	if first.Hash != second.Hash || first.Width != second.Width || first.Height != second.Height {
		t.Fatalf("cache returned different asset metadata: %#v %#v", first, second)
	}
}

func TestCacheExpiresAndCleansFiles(t *testing.T) {
	cache, err := NewCache(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	data := testPNG(t)
	asset, err := cache.Get(context.Background(), "https://cdn.example/expired.jpg", func() (Asset, error) {
		return Validate(data, "")
	})
	if err != nil {
		t.Fatalf("cache get: %v", err)
	}
	key, err := Key("https://cdn.example/expired.jpg")
	if err != nil {
		t.Fatalf("cache key: %v", err)
	}
	path := filepath.Join(cache.dir, key+cacheFileSuffix)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("age cache file: %v", err)
	}
	cache.now = func() time.Time { return time.Now() }
	if _, ok := cache.readFresh(key); ok {
		t.Fatal("expired artwork should not be a cache hit")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired cache file should be removed, stat error=%v", err)
	}
	if asset.Size == 0 {
		t.Fatal("expected validated asset")
	}

	temporary := filepath.Join(cache.dir, ".artwork-old.tmp")
	if err := os.WriteFile(temporary, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale temporary: %v", err)
	}
	if err := os.Chtimes(temporary, old, old); err != nil {
		t.Fatalf("age stale temporary: %v", err)
	}
	if err := cache.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("stale temporary should be removed, stat error=%v", err)
	}
}
