package lrclib

import (
	"context"
	"errors"
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
		config.BaseURL = "https://lrclib.net/api/get"
	}
	if config.UserAgent == "" {
		config.UserAgent = "Tagger/dev (https://github.com/ericwyn/tagger)"
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if config.RateInterval == 0 {
		config.RateInterval = 300 * time.Millisecond
	}
	return &Client{baseURL: config.BaseURL, userAgent: config.UserAgent, http: config.Client, gate: providers.NewGate(config.RateInterval)}
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{
		ID: "lrclib", Name: "LRCLIB", ShortName: "LR",
		Description:  "纯文本与同步 LRC 歌词",
		Capabilities: []string{"歌词", "同步歌词"}, Health: providers.HealthReady, Enabled: true,
		Accent: "#27645b", QuotaLabel: "礼貌限流 · 官方 API",
	}
}

func (c *Client) Search(ctx context.Context, query providers.Query, _ int) ([]providers.Candidate, error) {
	if query.Title == "" || len(query.Artists) == 0 {
		return []providers.Candidate{}, nil
	}
	if err := c.gate.Wait(ctx); err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("track_name", query.Title)
	values.Set("artist_name", strings.Join(query.Artists, ", "))
	if query.Album != "" {
		values.Set("album_name", query.Album)
	}
	if query.DurationSeconds > 0 {
		values.Set("duration", strconv.FormatInt(query.DurationSeconds, 10))
	}
	var response lyricsResponse
	if err := providers.GetJSON(ctx, c.http, c.baseURL+"?"+values.Encode(), c.userAgent, &response); err != nil {
		var httpError *providers.HTTPError
		if errors.As(err, &httpError) && httpError.Status == http.StatusNotFound {
			return []providers.Candidate{}, nil
		}
		return nil, err
	}
	return []providers.Candidate{{
		ProviderID: "lrclib", ExternalID: strconv.FormatInt(response.ID, 10), Title: response.TrackName,
		Artists: []string{response.ArtistName}, Album: response.AlbumName, AlbumArtists: []string{response.ArtistName},
		DurationSeconds: int64(response.Duration), Lyrics: response.PlainLyrics, SyncedLyrics: response.SyncedLyrics,
	}}, nil
}

type lyricsResponse struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}
