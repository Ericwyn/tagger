package musicbrainz

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericwyn/tagger/internal/providers"
	"github.com/ericwyn/tagger/internal/store"
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
		body := `{"recordings":[{"id":"mbid-1","title":"再回首","length":255000,"first-release-date":"1989-01-01","artist-credit":[{"name":"姜育恒","artist":{"id":"artist-1","name":"姜育恒"}}],"releases":[{"id":"release-1","title":"多年以后・再回首"}],"tags":[{"name":"mandopop"}]}]}`
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
	if candidates[0].MusicBrainzTrackID != "mbid-1" || candidates[0].MusicBrainzReleaseID != "release-1" || len(candidates[0].MusicBrainzArtistIDs) != 1 || candidates[0].MusicBrainzArtistIDs[0] != "artist-1" {
		t.Fatalf("musicbrainz ids = %#v", candidates[0])
	}
}

func TestSearchCanSimplifyChineseOutput(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"recordings":[{"id":"mbid-trad","title":"想見你","artist-credit":[{"name":"許嵩","artist":{"name":"許嵩"}}],"releases":[{"title":"專輯名"}],"tags":[{"name":"國語流行音樂"}]}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client := New(Config{BaseURL: "https://musicbrainz.test/recording", SimplifyChinese: true, Client: httpClient, RateInterval: -1})
	candidates, err := client.Search(context.Background(), providers.Query{Title: "想見你"}, 1)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
	if candidates[0].Title != "想见你" || candidates[0].Artists[0] != "许嵩" || candidates[0].Album != "专辑名" || candidates[0].Genres[0] != "国语流行音乐" {
		t.Fatalf("candidate was not simplified: %#v", candidates[0])
	}
	if client.CacheVariant() != "simplifyChinese=true" {
		t.Fatalf("cache variant = %q", client.CacheVariant())
	}
}

func TestArchiveDownloadBaseURLConfiguration(t *testing.T) {
	client := New(Config{})
	if got := client.ArtworkDownloadOptions().ArchiveDownloadBaseURL; got != providers.DefaultArchiveDownloadBaseURL {
		t.Fatalf("default archive download base = %q", got)
	}
	var field providers.ConfigField
	for _, candidate := range client.ConfigFields() {
		if candidate.Key == "archiveDownloadBaseUrl" {
			field = candidate
			break
		}
	}
	if field.Value != providers.DefaultArchiveDownloadBaseURL || !field.Required || field.Type != "url" {
		t.Fatalf("archive download config field = %#v", field)
	}
	if err := client.Configure(map[string]string{"archiveDownloadBaseUrl": "https://vercel-proxy.example.test/https/archive.org/"}); err != nil {
		t.Fatal(err)
	}
	if got := client.ArtworkDownloadOptions().ArchiveDownloadBaseURL; got != "https://vercel-proxy.example.test/https/archive.org" {
		t.Fatalf("configured archive download base = %q", got)
	}
	if err := client.Configure(map[string]string{"archiveDownloadBaseUrl": "http://mirror.example.test"}); err == nil {
		t.Fatal("insecure archive mirror was accepted")
	}
	if got := client.ArtworkDownloadOptions().ArchiveDownloadBaseURL; got != "https://vercel-proxy.example.test/https/archive.org" {
		t.Fatalf("invalid update changed archive download base to %q", got)
	}
	if err := client.ResetConfig(); err != nil {
		t.Fatal(err)
	}
	if got := client.ArtworkDownloadOptions().ArchiveDownloadBaseURL; got != providers.DefaultArchiveDownloadBaseURL {
		t.Fatalf("reset archive download base = %q", got)
	}
}

func TestProxyConfigurationAppliesToSearchAndArtworkAndResets(t *testing.T) {
	client := New(Config{})
	options := client.ArtworkDownloadOptions()
	if options.ProxyURL != "" || options.Gate == nil {
		t.Fatalf("default artwork transport options = %#v", options)
	}
	var proxyField providers.ConfigField
	for _, field := range client.ConfigFields() {
		if field.Key == "proxyUrl" {
			proxyField = field
			break
		}
	}
	if proxyField.Type != "url" || proxyField.Required || proxyField.Value != "" {
		t.Fatalf("proxy field = %#v", proxyField)
	}
	if err := client.Configure(map[string]string{"proxyUrl": "http://127.0.0.1:7890/"}); err != nil {
		t.Fatal(err)
	}
	if got := client.ArtworkDownloadOptions(); got.ProxyURL != "http://127.0.0.1:7890" || got.Gate != options.Gate {
		t.Fatalf("configured artwork transport options = %#v", got)
	}
	if err := client.Configure(map[string]string{"proxyUrl": "http://user:password@127.0.0.1:7890"}); err == nil {
		t.Fatal("authenticated proxy unexpectedly accepted")
	}
	if got := client.ArtworkDownloadOptions().ProxyURL; got != "http://127.0.0.1:7890" {
		t.Fatalf("invalid proxy update changed value to %q", got)
	}
	if err := client.ResetConfig(); err != nil {
		t.Fatal(err)
	}
	if got := client.ArtworkDownloadOptions().ProxyURL; got != "" {
		t.Fatalf("reset proxy = %q", got)
	}
}

func TestProxyConfigurationPersistsAcrossRegistryRestart(t *testing.T) {
	repository, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tagger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	client := New(Config{})
	registry := providers.NewRegistry(client)
	if err := registry.SetPersistence(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetConfig(context.Background(), "musicbrainz", map[string]string{"proxyUrl": "http://127.0.0.1:7890"}); err != nil {
		t.Fatal(err)
	}

	restarted := New(Config{})
	restartedRegistry := providers.NewRegistry(restarted)
	if err := restartedRegistry.SetPersistence(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	if got := restarted.ArtworkDownloadOptions().ProxyURL; got != "http://127.0.0.1:7890" {
		t.Fatalf("restored proxy = %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
