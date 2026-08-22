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
	searchBody := `{"abslist":[{"MUSICRID":"MUSIC_123","SONGNAME":"Song","ARTIST":"Artist&Guest","ALBUM":"Album","ALBUMARTIST":"Artist","SONG_DURATION":"03:21","TRACKNUM":4,"web_albumpic_short":"120/54/7/152082279.jpg"}]}`
	lyricsBody := `{"data":{"lrclist":[{"time":"1.25","lineLyric":"第一行"},{"time": "65.5", "lineLyric":"第二行"}]}}`
	client := New(Config{Endpoint: "https://example.test/search", LyricsEndpoint: "https://example.test/lyrics", LyricsRIDEndpoint: "https://example.test/rid", LyricsFileEndpoint: "https://example.test/file", RateInterval: -1, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body := searchBody
		if r.URL.Path == "/lyrics" {
			body = lyricsBody
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
	})}})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song"}, 3)
	if err != nil || len(items) != 1 || items[0].ExternalID != "123" || len(items[0].Artists) != 2 || items[0].DurationSeconds != 201 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if items[0].ArtworkURL != "https://img1.kwcdn.kuwo.cn/star/albumcover/500/54/7/152082279.jpg" {
		t.Fatalf("artwork = %q", items[0].ArtworkURL)
	}
	if items[0].Lyrics != "[00:01.25]第一行\n[01:05.50]第二行" || items[0].SyncedLyrics != items[0].Lyrics {
		t.Fatalf("lyrics = %q synced=%q", items[0].Lyrics, items[0].SyncedLyrics)
	}
}

func TestKuwoLyricsFallsBackToRIDAndLyricKey(t *testing.T) {
	client := New(Config{Endpoint: "https://example.test/search", LyricsEndpoint: "https://example.test/lyrics", LyricsRIDEndpoint: "https://example.test/rid", LyricsFileEndpoint: "https://example.test/file", RateInterval: -1, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body := `{"data":{}}`
		contentType := "application/json"
		switch r.URL.Path {
		case "/rid":
			body = `<root><music lyric="id=7&amp;token=abc"/></root>`
			contentType = "application/xml"
		case "/file":
			body = "[00:02.00]旧接口歌词"
			contentType = "text/plain"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {contentType}}, Request: r}, nil
	})}})
	lyrics, err := client.fetchLyrics(context.Background(), "123")
	if err != nil || lyrics != "[00:02.00]旧接口歌词" {
		t.Fatalf("lyrics=%q err=%v", lyrics, err)
	}
}

func TestParseLyricTimeSupportsSecondsMillisecondsAndClock(t *testing.T) {
	tests := map[string]float64{
		`1.25`:       1.25,
		`65000`:      65,
		`"01:05.50"`: 65.5,
	}
	for raw, expected := range tests {
		actual, ok := parseLyricTime([]byte(raw))
		if !ok || actual != expected {
			t.Errorf("parseLyricTime(%s) = %v, %v; want %v, true", raw, actual, ok, expected)
		}
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
