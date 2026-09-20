package itunes

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ericwyn/tagger/internal/providers"
)

func TestSearchMapsAppleCatalogResult(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("country") != "CN" || query.Get("media") != "music" || query.Get("entity") != "song" || query.Get("limit") != "3" {
			t.Errorf("query = %v", query)
		}
		body := `{"resultCount":1,"results":[{"trackId":99,"trackName":"再回首","artistName":"姜育恒","collectionName":"多年以后","collectionArtistName":"姜育恒","trackNumber":1,"trackCount":10,"discNumber":1,"discCount":1,"releaseDate":"1989-01-01T00:00:00Z","primaryGenreName":"Mandopop","trackTimeMillis":255000,"artworkUrl100":"https://example.test/100x100bb.jpg"}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	client := New(Config{BaseURL: "https://itunes.test/search", Country: "CN", Client: httpClient})
	candidates, err := client.Search(context.Background(), providers.Query{Title: "再回首", Artists: []string{"姜育恒"}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ExternalID != "99" || candidates[0].TrackTotal != 10 || candidates[0].Year != 1989 {
		t.Fatalf("candidate = %#v", candidates)
	}
	if candidates[0].ArtworkURL != "https://example.test/600x600bb.jpg" {
		t.Fatalf("artwork = %q", candidates[0].ArtworkURL)
	}
}

func TestSearchFallsBackFromMainlandStorefrontToHongKong(t *testing.T) {
	countries := make([]string, 0, 2)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		country := request.URL.Query().Get("country")
		countries = append(countries, country)
		body := `{"resultCount":0,"results":[]}`
		if country == "HK" {
			body = `{"resultCount":1,"results":[{"trackId":1114698235,"trackName":"最佳歌手","artistName":"許嵩","collectionName":"最佳歌手 - Single","artworkUrl100":"https://is1-ssl.mzstatic.com/100x100bb.jpg"}]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client := New(Config{BaseURL: "https://itunes.test/search", Country: "CN", Client: httpClient, RateInterval: -1})
	candidates, err := client.Search(context.Background(), providers.Query{Title: "最佳歌手", Artists: []string{"许嵩"}}, 1)
	if err != nil || len(candidates) != 1 || candidates[0].ExternalID != "1114698235" {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
	if len(countries) != 2 || countries[0] != "CN" || countries[1] != "HK" {
		t.Fatalf("countries = %#v", countries)
	}
}

func TestDefaultStorefrontIsHongKong(t *testing.T) {
	client := New(Config{Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("country") != "HK" {
			t.Fatalf("country = %q", request.URL.Query().Get("country"))
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[]}`))}, nil
	})}, RateInterval: -1})
	if _, err := client.Search(context.Background(), providers.Query{Title: "Song"}, 1); err != nil {
		t.Fatal(err)
	}
}

func TestSearchCanSimplifyChineseOutput(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"resultCount":1,"results":[{"trackId":7,"trackName":"想見你","artistName":"許嵩","collectionName":"專輯名","collectionArtistName":"許嵩","primaryGenreName":"國語流行音樂"}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client := New(Config{BaseURL: "https://itunes.test/search", Country: "HK", SimplifyChinese: true, Client: httpClient, RateInterval: -1})
	candidates, err := client.Search(context.Background(), providers.Query{Title: "想見你"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.Title != "想见你" || candidate.Artists[0] != "许嵩" || candidate.Album != "专辑名" || candidate.Genres[0] != "国语流行音乐" {
		t.Fatalf("candidate was not simplified: %#v", candidate)
	}
	if got := client.CacheVariant(); got != "baseUrl=https://itunes.test/search;country=HK;simplifyChinese=true" {
		t.Fatalf("CacheVariant() = %q", got)
	}
}

func TestAppleSimplifyConfigRoundTrips(t *testing.T) {
	client := New(Config{Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[]}`))}, nil
	})}, RateInterval: -1})
	if err := client.Configure(map[string]string{"simplifyChinese": "true"}); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, field := range client.ConfigFields() {
		if field.Key == "simplifyChinese" {
			found = true
			if field.Type != "boolean" || field.Value != "true" {
				t.Fatalf("simplify field = %#v", field)
			}
		}
	}
	if !found {
		t.Fatal("simplifyChinese config field missing")
	}
	if err := client.ResetConfig(); err != nil {
		t.Fatal(err)
	}
	if client.CacheVariant() != "baseUrl=https://itunes.apple.com/search;country=HK;simplifyChinese=false" {
		t.Fatalf("reset CacheVariant() = %q", client.CacheVariant())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
