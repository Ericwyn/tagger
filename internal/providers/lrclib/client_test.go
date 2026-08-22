package lrclib

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ericwyn/tagger/internal/providers"
)

func TestSearchUsesMetadataLookupAndMapsLyrics(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("track_name") != "一见如故" || query.Get("artist_name") != "许嵩" || query.Get("duration") != "262" {
			t.Errorf("query = %v", query)
		}
		if request.Header.Get("User-Agent") != "Tagger/Test" {
			t.Errorf("user-agent = %q", request.Header.Get("User-Agent"))
		}
		body := `{"id":42,"trackName":"一见如故","artistName":"许嵩","albumName":"安泊猜想","duration":262,"plainLyrics":"plain","syncedLyrics":"[00:01] synced"}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	client := New(Config{BaseURL: "https://lrclib.test/api/get", UserAgent: "Tagger/Test", Client: httpClient})
	candidates, err := client.Search(context.Background(), providers.Query{Title: "一见如故", Artists: []string{"许嵩"}, Album: "安泊猜想", DurationSeconds: 262}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ExternalID != "42" || candidates[0].Lyrics != "plain" || candidates[0].SyncedLyrics == "" {
		t.Fatalf("candidate = %#v", candidates)
	}
}

func TestSearchTreatsNotFoundAsEmpty(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not found"))}, nil
	})}
	client := New(Config{BaseURL: "https://lrclib.test/api/get", Client: httpClient})
	candidates, err := client.Search(context.Background(), providers.Query{Title: "missing", Artists: []string{"artist"}}, 5)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("candidates=%v err=%v", candidates, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
