package netease

import (
	"context"
	"github.com/ericwyn/tagger/internal/providers"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSearchMapsNeteaseResponse(t *testing.T) {
	client := New(Config{Endpoint: "https://example.test/search", Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Query().Get("s"), "Song") {
			t.Fatal(r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"result":{"songs":[{"id":42,"name":"Song","dt":210000,"ar":[{"name":"Artist"}],"al":{"name":"Album","picUrl":"https://img.music.126.net/a.jpg"},"no":3}]}}`)), Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
	})}})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song", Artists: []string{"Artist"}}, 3)
	if err != nil || len(items) != 1 || items[0].ExternalID != "42" || items[0].TrackNumber != 3 || items[0].DurationSeconds != 210 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}
