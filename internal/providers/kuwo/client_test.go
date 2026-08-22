package kuwo

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

func TestSearchMapsKuwoResponse(t *testing.T) {
	body := `{"abslist":[{"MUSICRID":"MUSIC_123","SONGNAME":"Song","ARTIST":"Artist&Guest","ALBUM":"Album","ALBUMARTIST":"Artist","SONG_DURATION":"03:21","TRACKNUM":4,"ALBUMPIC":"https://img.kuwo.cn/a.jpg"}]}`
	client := New(Config{Endpoint: "https://example.test/search", Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
	})}})
	items, err := client.Search(context.Background(), providers.Query{Title: "Song"}, 3)
	if err != nil || len(items) != 1 || items[0].ExternalID != "123" || len(items[0].Artists) != 2 || items[0].DurationSeconds != 201 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}
