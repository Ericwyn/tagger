package itunes

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ericwyn/tagger/internal/providers"
)

type Config struct {
	BaseURL      string
	Country      string
	UserAgent    string
	Client       *http.Client
	RateInterval time.Duration
}

type Client struct {
	baseURL, country, userAgent string
	http                        *http.Client
	gate                        *providers.Gate
}

func New(config Config) *Client {
	if config.BaseURL == "" {
		config.BaseURL = "https://itunes.apple.com/search"
	}
	if config.Country == "" {
		config.Country = "CN"
	}
	if config.UserAgent == "" {
		config.UserAgent = "Tagger/dev (https://github.com/ericwyn/tagger)"
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if config.RateInterval == 0 {
		config.RateInterval = 3 * time.Second
	}
	return &Client{baseURL: config.BaseURL, country: config.Country, userAgent: config.UserAgent, http: config.Client, gate: providers.NewGate(config.RateInterval)}
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{
		ID: "apple", Name: "Apple / iTunes", ShortName: "AM",
		Description:  "Apple 全球目录、版本与高质量封面",
		Capabilities: []string{"歌曲", "专辑", "音轨", "封面"}, Health: providers.HealthReady, Enabled: true,
		Accent: "#1d4ed8", QuotaLabel: "约 20 req/min · 官方 Search API",
	}
}

func (c *Client) Search(ctx context.Context, query providers.Query, limit int) ([]providers.Candidate, error) {
	if err := c.gate.Wait(ctx); err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("term", strings.TrimSpace(query.Title+" "+strings.Join(query.Artists, " ")))
	values.Set("country", c.country)
	values.Set("media", "music")
	values.Set("entity", "song")
	values.Set("limit", strconv.Itoa(limit))
	var response searchResponse
	if err := providers.GetJSON(ctx, c.http, c.baseURL+"?"+values.Encode(), c.userAgent, &response); err != nil {
		return nil, err
	}
	result := make([]providers.Candidate, 0, len(response.Results))
	for _, item := range response.Results {
		result = append(result, providers.Candidate{
			ProviderID: "apple", ExternalID: strconv.FormatInt(item.TrackID, 10), Title: item.TrackName,
			Artists: []string{item.ArtistName}, Album: item.CollectionName,
			AlbumArtists: []string{firstNonEmpty(item.CollectionArtistName, item.ArtistName)},
			Year:         year(item.ReleaseDate), TrackNumber: item.TrackNumber, TrackTotal: item.TrackCount,
			DiscNumber: item.DiscNumber, DiscTotal: item.DiscCount, DurationSeconds: item.TrackTimeMillis / 1000,
			Genres: []string{item.PrimaryGenreName}, ArtworkURL: strings.Replace(item.ArtworkURL100, "100x100", "600x600", 1),
		})
	}
	return result, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func year(value string) int {
	if len(value) < 4 {
		return 0
	}
	parsed, _ := strconv.Atoi(value[:4])
	return parsed
}

type searchResponse struct {
	Results []struct {
		TrackID              int64  `json:"trackId"`
		TrackName            string `json:"trackName"`
		ArtistName           string `json:"artistName"`
		CollectionName       string `json:"collectionName"`
		CollectionArtistName string `json:"collectionArtistName"`
		TrackNumber          int    `json:"trackNumber"`
		TrackCount           int    `json:"trackCount"`
		DiscNumber           int    `json:"discNumber"`
		DiscCount            int    `json:"discCount"`
		ReleaseDate          string `json:"releaseDate"`
		PrimaryGenreName     string `json:"primaryGenreName"`
		TrackTimeMillis      int64  `json:"trackTimeMillis"`
		ArtworkURL100        string `json:"artworkUrl100"`
	} `json:"results"`
}
