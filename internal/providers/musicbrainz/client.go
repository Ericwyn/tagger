package musicbrainz

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
	UserAgent    string
	Client       *http.Client
	RateInterval time.Duration
}

type Client struct {
	baseURL   string
	userAgent string
	http      *http.Client
	gate      *providers.Gate
}

func New(config Config) *Client {
	if config.BaseURL == "" {
		config.BaseURL = "https://musicbrainz.org/ws/2/recording/"
	}
	if config.UserAgent == "" {
		config.UserAgent = "Tagger/dev (https://github.com/ericwyn/tagger)"
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if config.RateInterval == 0 {
		config.RateInterval = time.Second
	}
	return &Client{baseURL: config.BaseURL, userAgent: config.UserAgent, http: config.Client, gate: providers.NewGate(config.RateInterval)}
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{
		ID: "musicbrainz", Name: "MusicBrainz", ShortName: "MB",
		Description:  "开放、结构化的发行与艺人资料",
		Capabilities: []string{"歌曲", "专辑", "音轨", "外部 ID"},
		Health:       providers.HealthReady, Enabled: true, Accent: "#e84b2c", QuotaLabel: "1 req/s · 官方 API",
	}
}

func (c *Client) Search(ctx context.Context, query providers.Query, limit int) ([]providers.Candidate, error) {
	if err := c.gate.Wait(ctx); err != nil {
		return nil, err
	}
	parts := []string{`recording:"` + escapeLucene(query.Title) + `"`}
	if len(query.Artists) > 0 {
		parts = append(parts, `artist:"`+escapeLucene(strings.Join(query.Artists, " "))+`"`)
	}
	values := url.Values{}
	values.Set("query", strings.Join(parts, " AND "))
	values.Set("fmt", "json")
	values.Set("limit", strconv.Itoa(limit))
	endpoint := c.baseURL + "?" + values.Encode()
	var response searchResponse
	if err := providers.GetJSON(ctx, c.http, endpoint, c.userAgent, &response); err != nil {
		return nil, err
	}
	result := make([]providers.Candidate, 0, len(response.Recordings))
	for _, recording := range response.Recordings {
		artists := make([]string, 0, len(recording.ArtistCredit))
		for _, credit := range recording.ArtistCredit {
			name := strings.TrimSpace(credit.Name)
			if name == "" {
				name = strings.TrimSpace(credit.Artist.Name)
			}
			if name != "" {
				artists = append(artists, name)
			}
		}
		album, artworkURL := "", ""
		if len(recording.Releases) > 0 {
			album = recording.Releases[0].Title
			if recording.Releases[0].ID != "" {
				artworkURL = "https://coverartarchive.org/release/" + recording.Releases[0].ID + "/front-500"
			}
		}
		genres := make([]string, 0, len(recording.Tags))
		for _, tag := range recording.Tags {
			if tag.Name != "" {
				genres = append(genres, tag.Name)
			}
		}
		result = append(result, providers.Candidate{
			ProviderID: "musicbrainz", ExternalID: recording.ID, Title: recording.Title,
			Artists: artists, Album: album, AlbumArtists: artists, Year: year(recording.FirstReleaseDate),
			DurationSeconds: int64(recording.Length / 1000), Genres: genres, ArtworkURL: artworkURL,
		})
	}
	return result, nil
}

func escapeLucene(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func year(value string) int {
	if len(value) < 4 {
		return 0
	}
	parsed, _ := strconv.Atoi(value[:4])
	return parsed
}

type searchResponse struct {
	Recordings []struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		Length           int64  `json:"length"`
		FirstReleaseDate string `json:"first-release-date"`
		ArtistCredit     []struct {
			Name   string `json:"name"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"artist-credit"`
		Releases []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"releases"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	} `json:"recordings"`
}
