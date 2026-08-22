package netease

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
	Client    *http.Client
	Endpoint  string
	UserAgent string
}
type Client struct {
	config Config
	http   *http.Client
}

func New(config Config) *Client {
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if config.Endpoint == "" {
		config.Endpoint = "https://music.163.com/api/cloudsearch/pc"
	}
	if config.UserAgent == "" {
		config.UserAgent = "Tagger/0.1 (experimental netease adapter)"
	}
	return &Client{config: config, http: config.Client}
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{ID: "netease", Name: "网易云音乐", ShortName: "NE", Description: "中文曲库、发行与封面实验性来源", Capabilities: []string{"歌曲", "专辑", "音轨", "封面"}, Health: providers.HealthDegraded, Enabled: false, Experimental: true, Accent: "#d62d20", QuotaLabel: "实验性网页接口 · 默认关闭"}
}

func (c *Client) Search(ctx context.Context, query providers.Query, limit int) ([]providers.Candidate, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	values := url.Values{"s": {strings.TrimSpace(strings.Join(append([]string{query.Title}, query.Artists...), " "))}, "type": {"1"}, "limit": {strconv.Itoa(limit)}, "offset": {"0"}}
	var response struct {
		Result struct {
			Songs []struct {
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				Duration int64  `json:"dt"`
				Artists  []struct {
					Name string `json:"name"`
				} `json:"ar"`
				Album struct {
					Name       string `json:"name"`
					PictureURL string `json:"picUrl"`
				} `json:"al"`
				TrackNumber int `json:"no"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := providers.GetJSON(ctx, c.http, c.config.Endpoint+"?"+values.Encode(), c.config.UserAgent, &response); err != nil {
		return nil, err
	}
	result := make([]providers.Candidate, 0, len(response.Result.Songs))
	for _, song := range response.Result.Songs {
		artists := make([]string, 0, len(song.Artists))
		for _, artist := range song.Artists {
			artists = append(artists, artist.Name)
		}
		result = append(result, providers.Candidate{ProviderID: "netease", ExternalID: strconv.FormatInt(song.ID, 10), Title: song.Name, Artists: artists, Album: song.Album.Name, TrackNumber: song.TrackNumber, DurationSeconds: song.Duration / 1000, ArtworkURL: song.Album.PictureURL})
	}
	return result, nil
}
