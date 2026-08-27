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
		{"lrcapi", "https://api.lrc.cx/cover?title=Song"},
		{"lrcapi", "https://is1-ssl.mzstatic.com/image/thumb/cover.jpg"},
		{"lrcapi", "https://p1.music.126.net/cover.jpg"},
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
		{"lrcapi", "https://images.example.com/cover.jpg"},
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
		if request.Header.Get("Accept") == "" || request.Header.Get("User-Agent") != BrowserUserAgent {
			t.Fatalf("headers = %#v", request.Header)
		}
		return &http.Response{
			// Deliberately lie about the type: this mirrors CDN responses where a
			// .jpg URL or stale header contains a valid PNG payload.
			StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}},
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

func TestDownloadArtworkUsesConfiguredArchiveMirror(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatal(err)
	}
	requests := make([]string, 0, 3)
	client := &http.Client{Transport: artworkRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.String())
		switch request.URL.Hostname() {
		case "coverartarchive.org":
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header: http.Header{"Location": {
					"https://archive.org/download/mbid-release-1/mbid-release-1-123_thumb500.jpg",
				}},
				Body: io.NopCloser(bytes.NewReader(nil)), Request: request,
			}, nil
		case "vercel-proxy.example.test":
			if strings.HasPrefix(request.URL.Path, "/https/archive.org/download/") {
				return &http.Response{
					StatusCode: http.StatusFound,
					Header: http.Header{"Location": {
						"/https/dn721809.ca.archive.org/0/items/mbid-release-1/mbid-release-1-123_thumb500.jpg",
					}},
					Body: io.NopCloser(bytes.NewReader(nil)), Request: request,
				}, nil
			}
			if strings.HasPrefix(request.URL.Path, "/https/dn721809.ca.archive.org/0/items/") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"image/jpeg"}},
					Body:       io.NopCloser(bytes.NewReader(imageData.Bytes())), Request: request,
				}, nil
			}
			t.Fatalf("unexpected proxy path %q", request.URL.Path)
			return nil, nil
		default:
			t.Fatalf("unexpected artwork host %q", request.URL.Hostname())
			return nil, nil
		}
	})}

	asset, err := DownloadArtworkWithOptions(context.Background(), ArtworkReference{
		ProviderID: "musicbrainz", URL: "https://coverartarchive.org/release/release-1/front-500",
	}, client, ArtworkDownloadOptions{ArchiveDownloadBaseURL: "https://vercel-proxy.example.test/https/archive.org"})
	if err != nil {
		t.Fatal(err)
	}
	if asset.Width != 8 || asset.Height != 6 || len(requests) != 3 {
		t.Fatalf("asset=%#v requests=%#v", asset, requests)
	}
	want := "https://vercel-proxy.example.test/https/archive.org/download/mbid-release-1/mbid-release-1-123_thumb500.jpg"
	if requests[1] != want {
		t.Fatalf("mirror request = %q, want %q", requests[1], want)
	}
	if !strings.Contains(requests[2], "/https/dn721809.ca.archive.org/0/items/") {
		t.Fatalf("relative storage redirect = %q", requests[2])
	}
}

func TestValidateArtworkDownloadBaseURLSupportsHTTPSPathPrefix(t *testing.T) {
	valid, err := ValidateArtworkDownloadBaseURL(" https://mirror.example.test/ ", "archiveDownloadBaseUrl")
	if err != nil || valid != "https://mirror.example.test" {
		t.Fatalf("valid base = %q err=%v", valid, err)
	}
	prefix, err := ValidateArtworkDownloadBaseURL("https://vercel-proxy.example.test/https/archive.org/", "archiveDownloadBaseUrl")
	if err != nil || prefix != "https://vercel-proxy.example.test/https/archive.org" {
		t.Fatalf("valid path prefix = %q err=%v", prefix, err)
	}
	for _, value := range []string{
		"http://mirror.example.test",
		"https://user@mirror.example.test",
		"https://mirror.example.test?upstream=archive.org",
		"https://mirror.example.test#archive",
	} {
		if _, err := ValidateArtworkDownloadBaseURL(value, "archiveDownloadBaseUrl"); err == nil {
			t.Errorf("base %q unexpectedly accepted", value)
		}
	}
}

func TestArtworkRedirectPolicyDoesNotRewriteDynamicArchiveNodes(t *testing.T) {
	config, err := parseArtworkDownloadConfig("musicbrainz", ArtworkDownloadOptions{ArchiveDownloadBaseURL: "https://mirror.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	client := withArtworkRedirectPolicy(&http.Client{}, "musicbrainz", config)
	request, err := http.NewRequest(http.MethodGet, "https://dn710000.ca.archive.org/0/items/mbid-release-1/cover.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(request, nil); err != nil {
		t.Fatal(err)
	}
	if got := request.URL.Hostname(); got != "dn710000.ca.archive.org" {
		t.Fatalf("dynamic archive node rewritten to %q", got)
	}
}
