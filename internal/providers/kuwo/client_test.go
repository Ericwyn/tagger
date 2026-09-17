package kuwo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ericwyn/tagger/internal/providers"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSearchMapsKuwoResponse(t *testing.T) {
	searchBody := `{"abslist":[{"MUSICRID":"MUSIC_123","SONGNAME":"Song","ARTIST":"Artist&Guest","ALBUM":"Album","ALBUMARTIST":"Artist","DURATION":"201","TRACKNUM":4,"web_albumpic_short":"120/54/7/152082279.jpg"}]}`
	lyricsBody := `{"data":{"lrclist":[{"time":"1.25","lineLyric":"第一行"},{"time": "65.5", "lineLyric":"第二行"}]}}`
	client := New(Config{Endpoint: "https://example.test/search", LyricsEndpoint: "https://example.test/lyrics", LyricsRIDEndpoint: "https://example.test/rid", LyricsFileEndpoint: "https://example.test/file", Auth: "Bearer test", Cookie: "kw_token=test-cookie", RateInterval: -1, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/search" && (r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("Cookie") != "kw_token=test-cookie" || r.Header.Get("Referer") == "" || r.Header.Get("Origin") == "") {
			t.Fatalf("search headers = %#v", r.Header)
		}
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
	if items[0].ArtworkURL != "https://img4.kuwo.cn/star/albumcover/500/54/7/152082279.jpg" {
		t.Fatalf("artwork = %q", items[0].ArtworkURL)
	}
	if items[0].Lyrics != "[00:01.25]第一行\n[01:05.50]第二行" || items[0].SyncedLyrics != items[0].Lyrics {
		t.Fatalf("lyrics = %q synced=%q", items[0].Lyrics, items[0].SyncedLyrics)
	}
}

func TestSearchSurfacesKuwoBusinessAuthError(t *testing.T) {
	client := New(Config{Endpoint: "https://example.test/search", RateInterval: -1, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":401,"msg":"登录已过期"}`)), Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
	})}})
	_, err := client.Search(context.Background(), providers.Query{Title: "Song"}, 1)
	var businessErr *providers.BusinessError
	if !errors.As(err, &businessErr) || !strings.Contains(err.Error(), "登录已过期") {
		t.Fatalf("error=%T %v", err, err)
	}
}

func TestSearchFallsBackToTitleWhenArtistKeywordHasNoResults(t *testing.T) {
	searches := 0
	client := New(Config{Endpoint: "https://example.test/search", LyricsEndpoint: "https://example.test/lyrics", LyricsRIDEndpoint: "https://example.test/rid", LyricsFileEndpoint: "https://example.test/file", RateInterval: -1, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/search" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{}}`)), Header: http.Header{}, Request: r}, nil
		}
		searches++
		if r.URL.Query().Get("all") != "Song" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"abslist":[]}`)), Header: http.Header{}, Request: r}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"abslist":[{"MUSICRID":"MUSIC_9","SONGNAME":"Song","ARTIST":"Artist"}]}`)), Header: http.Header{}, Request: r}, nil
	})}})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song", Artists: []string{"Wrong"}}, 1)
	if err != nil || len(items) != 1 || items[0].ExternalID != "9" || searches != 2 {
		t.Fatalf("items=%#v err=%v searches=%d", items, err, searches)
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

func TestKuwoLyricsRetriesCurrentEndpointForPersistedLegacyConfig(t *testing.T) {
	requests := 0
	client := New(Config{LyricsEndpoint: legacyLyricsEndpoint, LyricsRIDEndpoint: "https://example.test/rid", RateInterval: -1, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/newh5/singles/songinfoandlrc" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":301,"msg":"音乐查询失败"}`)), Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
		}
		if r.URL.Path == "/openapi/v1/www/lyric/getlyric" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":200,"data":{"lrclist":[{"time":"1.25","lineLyric":"当前接口歌词"}]}}`)), Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})}})
	lyrics, err := client.fetchLyrics(context.Background(), "236362975")
	if err != nil || lyrics != "[00:01.25]当前接口歌词" || requests != 2 {
		t.Fatalf("lyrics=%q err=%v requests=%d", lyrics, err, requests)
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
		"/star/albumcover/120/1/2/3.jpg":                     "https://img4.kuwo.cn/star/albumcover/500/1/2/3.jpg",
		"//img2.kwcdn.kuwo.cn/star/albumcover/500/1/2/3.jpg": "https://img4.kuwo.cn/star/albumcover/500/1/2/3.jpg",
	}
	for input, expected := range tests {
		if actual := normalizeArtworkURL(input); actual != expected {
			t.Errorf("normalizeArtworkURL(%q) = %q, want %q", input, actual, expected)
		}
	}
}
