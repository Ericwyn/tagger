package musicbrainz

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ericwyn/tagger/internal/providers"
)

func TestSearchBuildsOfficialRecordingQueryAndMapsResponse(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("User-Agent") != "Tagger/Test" {
			t.Errorf("user-agent = %q", request.Header.Get("User-Agent"))
		}
		query := request.URL.Query().Get("query")
		if !strings.Contains(query, `recording:"再回首"`) || !strings.Contains(query, `artist:"姜育恒"`) {
			t.Errorf("query = %q", query)
		}
		body := `{"recordings":[{"id":"mbid-1","title":"再回首","length":255000,"first-release-date":"1989-01-01","artist-credit":[{"name":"姜育恒"}],"releases":[{"id":"release-1","title":"多年以后・再回首"}],"tags":[{"name":"mandopop"}]}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	client := New(Config{BaseURL: "https://musicbrainz.test/recording", UserAgent: "Tagger/Test", Client: httpClient})
	candidates, err := client.Search(context.Background(), providers.Query{Title: "再回首", Artists: []string{"姜育恒"}}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ExternalID != "mbid-1" || candidates[0].Year != 1989 || candidates[0].DurationSeconds != 255 {
		t.Fatalf("candidate = %#v", candidates)
	}
	if candidates[0].ArtworkURL != "https://coverartarchive.org/release/release-1/front-500" {
		t.Fatalf("artwork = %q", candidates[0].ArtworkURL)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
