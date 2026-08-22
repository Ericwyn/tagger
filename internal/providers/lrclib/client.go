package lrclib

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ericwyn/tagger/internal/providers"
)

type Config struct {
	BaseURL      string
	SearchURL    string
	UserAgent    string
	Client       *http.Client
	RateInterval time.Duration
}

type Client struct {
	mu        sync.RWMutex
	baseURL   string
	searchURL string
	userAgent string
	http      *http.Client
	gate      *providers.Gate
}

func New(config Config) *Client {
	if config.BaseURL == "" {
		config.BaseURL = "https://lrclib.net/api/get"
	}
	if config.SearchURL == "" {
		config.SearchURL = deriveSearchURL(config.BaseURL)
	}
	if config.UserAgent == "" {
		config.UserAgent = providers.DefaultUserAgent("lrclib")
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if config.RateInterval == 0 {
		config.RateInterval = 300 * time.Millisecond
	}
	return &Client{baseURL: config.BaseURL, searchURL: config.SearchURL, userAgent: config.UserAgent, http: config.Client, gate: providers.NewGate(config.RateInterval)}
}

// ResetConfig restores both LRCLIB lookup endpoints and the polite default
// request interval without replacing the configured HTTP client.
func (c *Client) ResetConfig() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = "https://lrclib.net/api/get"
	c.searchURL = "https://lrclib.net/api/search"
	c.userAgent = providers.DefaultUserAgent("lrclib")
	c.gate.SetInterval(300 * time.Millisecond)
	return nil
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{
		ID: "lrclib", Name: "LRCLIB", ShortName: "LR",
		Description:  "纯文本与同步 LRC 歌词",
		Capabilities: []string{"歌词", "同步歌词"}, Health: providers.HealthReady, Enabled: true,
		Accent: "#27645b", QuotaLabel: "礼貌限流 · 官方 API",
	}
}

func (c *Client) ConfigFields() []providers.ConfigField {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return []providers.ConfigField{
		{Key: "baseUrl", Label: "精确查询 URL", Type: "url", Value: c.baseURL, Required: true},
		{Key: "searchUrl", Label: "宽搜索 URL", Type: "url", Value: c.searchURL, Required: true},
		{Key: "userAgent", Label: "User-Agent", Type: "text", Value: c.userAgent, Required: true},
		{Key: "rateIntervalMs", Label: "请求间隔（毫秒）", Type: "number", Value: strconv.FormatInt(c.gate.Interval().Milliseconds(), 10)},
	}
}

func (c *Client) Configure(values map[string]string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, value := range values {
		switch key {
		case "baseUrl":
			endpoint, err := providers.ValidateHTTPURL(value, "baseUrl")
			if err != nil {
				return err
			}
			c.baseURL = endpoint
		case "searchUrl":
			endpoint, err := providers.ValidateHTTPURL(value, "searchUrl")
			if err != nil {
				return err
			}
			c.searchURL = endpoint
		case "userAgent":
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("userAgent 不能为空")
			}
			c.userAgent = strings.TrimSpace(value)
		case "rateIntervalMs":
			interval, err := providers.ParseRateInterval(value)
			if err != nil {
				return err
			}
			c.gate.SetInterval(interval)
		default:
			return fmt.Errorf("未知配置项 %q", key)
		}
	}
	return nil
}

func (c *Client) Search(ctx context.Context, query providers.Query, limit int) ([]providers.Candidate, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if strings.TrimSpace(query.Title) == "" {
		return []providers.Candidate{}, nil
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	var firstErr error
	// /api/get is very precise and cheap when the artist/duration are correct,
	// so keep it as the fast path. Older files often have incomplete artists;
	// those continue into the broader /api/search fallback below.
	if len(query.Artists) > 0 {
		if err := c.gate.Wait(ctx); err != nil {
			return nil, err
		}
		values := metadataValues(query)
		var response lyricsResponse
		err := providers.GetJSON(ctx, c.http, c.baseURL+"?"+values.Encode(), c.userAgent, &response)
		if err == nil {
			if candidate, ok := responseCandidate(response); ok {
				return []providers.Candidate{candidate}, nil
			}
		} else if !isNotFound(err) {
			firstErr = err
		}
	}

	results, err := c.searchFallback(ctx, query, limit)
	if err == nil {
		return results, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, err
}

func (c *Client) searchFallback(ctx context.Context, query providers.Query, limit int) ([]providers.Candidate, error) {
	terms := searchTerms(query)
	var firstErr error
	seen := make(map[string]struct{}, limit)
	result := make([]providers.Candidate, 0, limit)
	for index, term := range terms {
		if index > 0 {
			if err := c.gate.Wait(ctx); err != nil {
				return nil, err
			}
		} else if len(query.Artists) == 0 {
			if err := c.gate.Wait(ctx); err != nil {
				return nil, err
			}
		}
		values := url.Values{}
		values.Set("q", term)
		values.Set("track_name", term)
		if len(query.Artists) > 0 {
			values.Set("artist_name", strings.Join(query.Artists, ", "))
		}
		if query.Album != "" {
			values.Set("album_name", query.Album)
		}
		var response []lyricsResponse
		if err := providers.GetJSON(ctx, c.http, c.searchURL+"?"+values.Encode(), c.userAgent, &response); err != nil {
			if isNotFound(err) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, item := range response {
			candidate, ok := responseCandidate(item)
			if !ok {
				continue
			}
			key := candidate.ExternalID + "\x00" + candidate.Title + "\x00" + strings.Join(candidate.Artists, ",")
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, candidate)
			if len(result) >= limit {
				return result, nil
			}
		}
	}
	if len(result) > 0 {
		return result, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return []providers.Candidate{}, nil
}

func responseCandidate(response lyricsResponse) (providers.Candidate, bool) {
	trackName := strings.TrimSpace(response.TrackName)
	if trackName == "" {
		trackName = strings.TrimSpace(response.Name)
	}
	synced := providers.NormalizeLyrics(response.SyncedLyrics)
	plain := providers.NormalizeLyrics(response.PlainLyrics)
	if synced == "" && plain == "" {
		return providers.Candidate{}, false
	}
	if synced == "" {
		synced = plain
	}
	id := strconv.FormatInt(response.ID, 10)
	if response.ID == 0 {
		id = strings.TrimSpace(trackName + "-" + response.ArtistName)
	}
	artists := []string{}
	if strings.TrimSpace(response.ArtistName) != "" {
		artists = []string{strings.TrimSpace(response.ArtistName)}
	}
	return providers.Candidate{
		ProviderID: "lrclib", ExternalID: id, Title: trackName, Artists: artists,
		Album: response.AlbumName, AlbumArtists: append([]string(nil), artists...),
		DurationSeconds: int64(response.Duration), Lyrics: plain, SyncedLyrics: synced,
	}, true
}

func metadataValues(query providers.Query) url.Values {
	values := url.Values{}
	values.Set("track_name", query.Title)
	values.Set("artist_name", strings.Join(query.Artists, ", "))
	if query.Album != "" {
		values.Set("album_name", query.Album)
	}
	if query.DurationSeconds > 0 {
		values.Set("duration", strconv.FormatInt(query.DurationSeconds, 10))
	}
	return values
}

func searchTerms(query providers.Query) []string {
	result := []string{}
	appendTerm := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range result {
			if existing == value {
				return
			}
		}
		result = append(result, value)
	}
	appendTerm(strings.Join(append([]string{query.Title}, query.Artists...), " "))
	appendTerm(query.Title)
	// Parenthesized edition/feature suffixes are common in filenames but are
	// not consistently indexed by lyric services.
	if index := strings.IndexAny(query.Title, "([〔【"); index > 0 {
		appendTerm(strings.TrimSpace(query.Title[:index]))
	}
	return result
}

func deriveSearchURL(base string) string {
	parsed, err := url.Parse(base)
	if err == nil && strings.HasSuffix(parsed.Path, "/get") {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/get") + "/search"
		return parsed.String()
	}
	return strings.TrimSuffix(base, "/") + "/search"
}

func isNotFound(err error) bool {
	var httpError *providers.HTTPError
	return errors.As(err, &httpError) && httpError.Status == http.StatusNotFound
}

type lyricsResponse struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}
