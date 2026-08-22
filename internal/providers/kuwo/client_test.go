package kuwo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ericwyn/tagger/internal/providers"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSearchMapsKuwoResponse(t *testing.T) {
	body := `{"abslist":[{"MUSICRID":"MUSIC_123","SONGNAME":"Song","ARTIST":"Artist&Guest","ALBUM":"Album","ALBUMARTIST":"Artist","SONG_DURATION":"03:21","TRACKNUM":4,"web_albumpic_short":"120/54/7/152082279.jpg"}]}`
	client := New(Config{Endpoint: "https://example.test/search", Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
	})}})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song"}, 3)
	if err != nil || len(items) != 1 || items[0].ExternalID != "123" || len(items[0].Artists) != 2 || items[0].DurationSeconds != 201 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if items[0].ArtworkURL != "https://img1.kwcdn.kuwo.cn/star/albumcover/500/54/7/152082279.jpg" {
		t.Fatalf("artwork = %q", items[0].ArtworkURL)
	}
}

func TestNormalizeArtworkURLSupportsLegacyKuwoValues(t *testing.T) {
	tests := map[string]string{
		"http://img1.kuwo.cn/star/albumcover/120/1/2/3.jpg":  "https://img1.kuwo.cn/star/albumcover/120/1/2/3.jpg",
		"/star/albumcover/120/1/2/3.jpg":                     "https://img1.kwcdn.kuwo.cn/star/albumcover/500/1/2/3.jpg",
		"//img2.kwcdn.kuwo.cn/star/albumcover/500/1/2/3.jpg": "https://img2.kwcdn.kuwo.cn/star/albumcover/500/1/2/3.jpg",
	}
	for input, expected := range tests {
		if actual := normalizeArtworkURL(input); actual != expected {
			t.Errorf("normalizeArtworkURL(%q) = %q, want %q", input, actual, expected)
		}
	}
}
