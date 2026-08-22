package providers

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

type artworkRoundTripFunc func(*http.Request) (*http.Response, error)

func (function artworkRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestValidateArtworkURLUsesProviderAllowlist(t *testing.T) {
	allowed := []struct{ provider, url string }{
		{"musicbrainz", "https://coverartarchive.org/release/id/front-500"},
		{"musicbrainz", "https://ia801.test.archive.org/file.jpg"},
		{"apple", "https://is1-ssl.mzstatic.com/image/thumb.jpg"},
		{"netease", "https://p1.music.126.net/cover.jpg"},
		{"kuwo", "https://img1.kuwo.cn/cover.jpg"},
		{"kuwo", "https://img1.kwcdn.kuwo.cn/star/albumcover/500/1/2/3.jpg"},
		{"kugou", "https://imge.kugou.com/cover.jpg"},
	}
	for _, item := range allowed {
		if _, err := validateArtworkURL(item.provider, item.url); err != nil {
			t.Errorf("validate %s: %v", item.url, err)
		}
	}
	blocked := []struct{ provider, url string }{
		{"apple", "http://is1-ssl.mzstatic.com/image.jpg"},
		{"apple", "https://mzstatic.com.evil.test/image.jpg"},
		{"musicbrainz", "https://127.0.0.1/cover.jpg"},
		{"unknown", "https://coverartarchive.org/cover.jpg"},
	}
	for _, item := range blocked {
		if _, err := validateArtworkURL(item.provider, item.url); !errors.Is(err, ErrUnsafeArtworkURL) {
			t.Errorf("blocked URL %s error = %v", item.url, err)
		}
	}
}

func TestPublicIPRejectsLocalAndPrivateRanges(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.2", "169.254.169.254", "::1", "fc00::1"} {
		if publicIP(net.ParseIP(value)) {
			t.Errorf("%s unexpectedly considered public", value)
		}
	}
	if !publicIP(net.ParseIP("1.1.1.1")) || !publicIP(net.ParseIP("2606:4700:4700::1111")) {
		t.Fatal("public resolver addresses were rejected")
	}
}

func TestDownloadArtworkValidatesResponseBytes(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 5, 4))); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: artworkRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Accept") == "" || !strings.Contains(request.Header.Get("User-Agent"), "Tagger") {
			t.Fatalf("headers = %#v", request.Header)
		}
		return &http.Response{
			StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}},
			Body: io.NopCloser(bytes.NewReader(imageData.Bytes())), Request: request,
		}, nil
	})}
	asset, err := DownloadArtwork(context.Background(), ArtworkReference{
		ProviderID: "apple", URL: "https://is1-ssl.mzstatic.com/image.png",
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Width != 5 || asset.Height != 4 || asset.MIME != "image/png" {
		t.Fatalf("asset = %#v", asset)
	}
}
