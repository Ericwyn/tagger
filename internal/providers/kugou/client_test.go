package kugou

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ericwyn/tagger/internal/providers"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSearchMapsLyricsAndArtwork(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("\ufeff[00:01.00] hello\r\n"))
	client := New(Config{
		SearchEndpoint:    "https://example.test/search",
		LyricsSearchURL:   "https://example.test/lyrics/search",
		LyricsDownloadURL: "https://example.test/lyrics/download",
		ArtworkEndpoint:   "https://example.test/artwork",
		Cookie:            "kg_mid=test-cookie",
		RateInterval:      0,
		Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/search":
				if r.URL.Query().Get("keyword") != "Song Artist" || r.Header.Get("Cookie") != "kg_mid=test-cookie" || r.Header.Get("Referer") == "" {
					t.Fatalf("search request = %v headers=%v", r.URL, r.Header)
				}
				return response(r, `{"data":{"info":[{"hash":"ABC","songname":"Song","singername":"Artist&Guest","album_id":55,"album_name":"Album","duration":"03:21","tracknum":4}]}}`), nil
			case "/lyrics/search":
				if r.URL.Query().Get("hash") != "ABC" {
					t.Fatalf("lyrics search query = %v", r.URL.Query())
				}
				return response(r, `{"candidates":[{"id":7,"accesskey":"key"}]}`), nil
			case "/lyrics/download":
				return response(r, `{"content":"`+encoded+`"}`), nil
			case "/artwork":
				return response(r, `{"data":{"img":"https://imge.kugou.com/stdmusic/cover.jpg"}}`), nil
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
				return nil, nil
			}
		})},
	})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song", Artists: []string{"Artist"}}, 3)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	item := items[0]
	if item.ExternalID != "ABC" || len(item.Artists) != 2 || item.DurationSeconds != 201 || item.TrackNumber != 4 {
		t.Fatalf("metadata = %#v", item)
	}
	if item.SyncedLyrics != "[00:01.00] hello" || item.Lyrics != item.SyncedLyrics {
		t.Fatalf("lyrics = %#v", item)
	}
	if item.ArtworkURL != "https://imge.kugou.com/stdmusic/cover.jpg" {
		t.Fatalf("artwork = %q", item.ArtworkURL)
	}
}

func TestSearchUsesCurrentEndpointAndResponseShape(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("[00:01.00] 歌词"))
	client := New(Config{
		// Simulate an endpoint restored from settings written by an older build.
		SearchEndpoint:    "https://mobilecdn.kugou.com/api/v3/search/song",
		LyricsSearchURL:   "https://example.test/lyrics/search",
		LyricsDownloadURL: "https://example.test/lyrics/download",
		RateInterval:      -1,
		Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/song_search_v2":
				if r.URL.Hostname() != "songsearch.kugou.com" || r.URL.Query().Get("platform") != "WebFilter" {
					t.Fatalf("search request = %s", r.URL)
				}
				return response(r, `{"status":1,"data":{"lists":[{"FileHash":"ABC","SongName":"幻听","SingerName":"<em>许嵩</em>","AlbumID":"973046","AlbumName":"梦游计","Duration":273,"Image":"http://imge.kugou.com/stdmusic/{size}/cover.jpg"}]}}`), nil
			case "/lyrics/search":
				return response(r, `{"status":200,"candidates":[{"id":7,"accesskey":"key"}]}`), nil
			case "/lyrics/download":
				return response(r, `{"status":200,"content":"`+encoded+`"}`), nil
			default:
				t.Fatalf("unexpected request %s", r.URL)
				return nil, nil
			}
		})},
	})
	items, err := client.Search(context.Background(), providers.Query{Title: "幻听", Artists: []string{"许嵩"}}, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	item := items[0]
	if item.ExternalID != "ABC" || item.Album != "梦游计" || item.DurationSeconds != 273 || len(item.Artists) != 1 || item.Artists[0] != "许嵩" {
		t.Fatalf("metadata = %#v", item)
	}
	if item.ArtworkURL != "https://imge.kugou.com/stdmusic/500/cover.jpg" || item.Lyrics != "[00:01.00] 歌词" {
		t.Fatalf("assets = %#v", item)
	}
}

func TestSearchSurfacesKuGouBusinessAuthError(t *testing.T) {
	client := New(Config{SearchEndpoint: "https://example.test/search", RateInterval: -1, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(r, `{"status":401,"error":"需要登录"}`), nil
	})}})
	_, err := client.Search(context.Background(), providers.Query{Title: "Song"}, 1)
	var businessErr *providers.BusinessError
	if !errors.As(err, &businessErr) || !strings.Contains(err.Error(), "需要登录") {
		t.Fatalf("error=%T %v", err, err)
	}
}

func response(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}, Request: request}
}
