package lrcapi

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

func TestSearchMapsLrcApiLyricsAndCover(t *testing.T) {
	client := New(Config{
		BaseURL: "https://example.test/jsonapi", CoverURL: "https://example.test/cover", Auth: "secret", RateInterval: 0,
		Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/jsonapi" || r.URL.Query().Get("title") != "Song" || r.URL.Query().Get("artist") != "Artist" || r.Header.Get("Authorization") != "secret" {
				t.Fatalf("request = %v headers=%v", r.URL, r.Header)
			}
			return response(r, `[{"id":"lrc-42","title":"Song","artist":"Artist / Guest","album":"Album","lyrics":"\ufeff[00:01.00] hello","duration":210}]`), nil
		})},
	})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song", Artists: []string{"Artist"}, Album: "Album"}, 2)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	item := items[0]
	if item.ExternalID != "lrc-42" || item.Title != "Song" || len(item.Artists) != 2 || item.DurationSeconds != 210 {
		t.Fatalf("metadata = %#v", item)
	}
	if item.Lyrics != "[00:01.00] hello" || item.SyncedLyrics != item.Lyrics {
		t.Fatalf("lyrics = %#v", item)
	}
	if item.ArtworkURL != "https://example.test/cover?album=Album&artist=Artist%2C+Guest&title=Song" {
		t.Fatalf("cover = %q", item.ArtworkURL)
	}
}

func TestSearchCanSimplifyChineseLyricsAndMetadata(t *testing.T) {
	client := New(Config{
		BaseURL: "https://example.test/jsonapi", SimplifyChinese: true, RateInterval: -1,
		Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			return response(r, `[{"id":"lrc-trad","title":"想見你","artist":"許嵩","album":"專輯名","lyrics":"[00:01.00] 想見你"}]`), nil
		})},
	})
	items, err := client.Search(context.Background(), providers.Query{Title: "想見你"}, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if items[0].Title != "想见你" || items[0].Artists[0] != "许嵩" || items[0].Album != "专辑名" || items[0].Lyrics != "[00:01.00] 想见你" {
		t.Fatalf("candidate was not simplified: %#v", items[0])
	}
}

func TestResponseEnvelopeAndFallbackID(t *testing.T) {
	client := New(Config{BaseURL: "https://example.test/jsonapi", CoverURL: "https://example.test/cover", RateInterval: 0, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(r, `{"data":{"title":"Wrapped","lyrics":"[00:02.00] hi"}}`), nil
	})}})
	items, err := client.Search(context.Background(), providers.Query{Title: "Wrapped"}, 1)
	if err != nil || len(items) != 1 || items[0].ExternalID == "" || items[0].Title != "Wrapped" || items[0].Lyrics != "[00:02.00] hi" {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func response(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}, Request: request}
}
