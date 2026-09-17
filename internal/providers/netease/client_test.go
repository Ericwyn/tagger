package netease

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

func TestSearchMapsMetadataLyricsAndArtwork(t *testing.T) {
	client := New(Config{
		Endpoint:      "https://example.test/search",
		LyricEndpoint: "https://example.test/lyric",
		Cookie:        "MUSIC_U=test-cookie",
		RateInterval:  0,
		Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/search" {
				if !strings.Contains(r.URL.Query().Get("s"), "Song") || r.Header.Get("Origin") != "https://music.163.com" || r.Header.Get("Cookie") != "MUSIC_U=test-cookie" {
					t.Fatalf("search request = %v headers=%v", r.URL, r.Header)
				}
				return response(r, `{"result":{"songs":[{"id":42,"name":"Song","alia":["Song (Live)"],"dt":210000,"ar":[{"name":"Artist"}],"al":{"name":"Album","picUrl":"https://img.music.126.net/a.jpg"},"no":3}]}}`), nil
			}
			if r.URL.Path == "/lyric" {
				return response(r, `{"lrc":{"lyric":"\ufeff[00:01.00] hello\r\n"},"tlyric":{"lyric":"[00:01.00] translated"}}`), nil
			}
			t.Fatalf("unexpected path %s", r.URL.Path)
			return nil, nil
		})},
	})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song", Artists: []string{"Artist"}}, 3)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	item := items[0]
	if item.ExternalID != "42" || item.TrackNumber != 3 || item.DurationSeconds != 210 || item.Album != "Album" {
		t.Fatalf("metadata = %#v", item)
	}
	if len(item.AlternateTitles) != 1 || item.AlternateTitles[0] != "Song (Live)" {
		t.Fatalf("aliases = %#v", item.AlternateTitles)
	}
	if item.SyncedLyrics != "[00:01.00] hello\n[00:01.00] translated" || item.Lyrics != item.SyncedLyrics {
		t.Fatalf("lyrics = %#v", item)
	}
	if item.ArtworkURL != "https://img.music.126.net/a.jpg?param=500y" {
		t.Fatalf("artwork = %q", item.ArtworkURL)
	}
}

func TestFetchLyricsUsesTranslationWhenOriginalIsMissing(t *testing.T) {
	client := New(Config{LyricEndpoint: "https://example.test/lyric", RateInterval: -1, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(r, `{"tlyric":{"lyric":"[00:01.00] 中文译文"}}`), nil
	})}})
	lyrics, err := client.fetchLyrics(context.Background(), 42)
	if err != nil || lyrics != "[00:01.00] 中文译文" {
		t.Fatalf("lyrics=%q err=%v", lyrics, err)
	}
}

func TestMergeTimedLyricsInterleavesAndPreservesMetadata(t *testing.T) {
	original := "[ar:Adele]\n[00:05.00]Second\n[00:01.00]First"
	translated := "[ar:Adele]\n[00:01.00]第一句\n[00:05.00]第二句"
	want := "[ar:Adele]\n[00:01.00]First\n[00:01.00]第一句\n[00:05.00]Second\n[00:05.00]第二句"
	if actual := mergeTimedLyrics(original, translated); actual != want {
		t.Fatalf("merged lyrics = %q, want %q", actual, want)
	}
}

func TestSearchSurfacesNetEaseBusinessAuthError(t *testing.T) {
	client := New(Config{Endpoint: "https://example.test/search", RateInterval: -1, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(r, `{"code":401,"msg":"需要登录"}`), nil
	})}})
	_, err := client.Search(context.Background(), providers.Query{Title: "Song"}, 1)
	var businessErr *providers.BusinessError
	if !errors.As(err, &businessErr) || businessErr.Code != "401" || !strings.Contains(err.Error(), "需要登录") {
		t.Fatalf("error=%T %v", err, err)
	}
}

func TestSearchUsesAlbumEndpointWhenSearchResultHasNoCover(t *testing.T) {
	client := New(Config{
		Endpoint:      "https://example.test/search",
		LyricEndpoint: "https://example.test/lyric",
		AlbumEndpoint: "https://example.test/album",
		RateInterval:  0,
		Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/search":
				return response(r, `{"result":{"songs":[{"id":42,"name":"Song","dt":210000,"ar":[{"name":"Artist"}],"al":{"id":7,"name":"Album"}}]}}`), nil
			case "/album/7":
				return response(r, `{"album":{"picUrl":"http://p1.music.126.net/fallback.jpg"}}`), nil
			case "/lyric":
				return response(r, `{"lrc":{"lyric":"[00:01.00] hello"}}`), nil
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
				return nil, nil
			}
		})},
	})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song", Artists: []string{"Artist"}}, 1)
	if err != nil || len(items) != 1 || items[0].ArtworkURL != "https://p1.music.126.net/fallback.jpg?param=500y" {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestSearchOnlyFetchesLyricsForReturnedCandidates(t *testing.T) {
	lyricCalls := 0
	client := New(Config{
		Endpoint: "https://example.test/search", LyricEndpoint: "https://example.test/lyric", RateInterval: -1,
		Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/search" {
				return response(r, `{"result":{"songs":[
					{"id":1,"name":"Song","dt":210000,"ar":[{"name":"Artist"}],"al":{"name":"Album"}},
					{"id":2,"name":"Song Live","dt":211000,"ar":[{"name":"Artist"}],"al":{"name":"Album"}},
					{"id":3,"name":"Song Remix","dt":212000,"ar":[{"name":"Artist"}],"al":{"name":"Album"}},
					{"id":4,"name":"Song Demo","dt":213000,"ar":[{"name":"Artist"}],"al":{"name":"Album"}}
				]}}`), nil
			}
			if r.URL.Path == "/lyric" {
				lyricCalls++
				return response(r, `{"lrc":{"lyric":"[00:01.00] line"}}`), nil
			}
			t.Fatalf("unexpected path %s", r.URL.Path)
			return nil, nil
		})},
	})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song", Artists: []string{"Artist"}}, 2)
	if err != nil || len(items) != 2 || lyricCalls != 2 {
		t.Fatalf("items=%#v lyricCalls=%d err=%v", items, lyricCalls, err)
	}
}

func response(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}, Request: request}
}
