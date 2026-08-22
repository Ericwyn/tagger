// Package lrcapi adapts the self-hosted/public LrcApi aggregator into the
// normal Tagger candidate contract. LrcApi is deliberately opt-in because
// its public instance aggregates unofficial sources and may be unavailable.
package lrcapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	CoverURL     string
	Auth         string
	UserAgent    string
	Client       *http.Client
	RateInterval time.Duration
}

type Client struct {
	mu                                 sync.RWMutex
	baseURL, coverURL, auth, userAgent string
	http                               *http.Client
	gate                               *providers.Gate
}

func New(config Config) *Client {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.lrc.cx/jsonapi"
	}
	if config.CoverURL == "" {
		config.CoverURL = "https://api.lrc.cx/cover"
	}
	if config.UserAgent == "" {
		config.UserAgent = providers.DefaultUserAgent("lrcapi")
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 12 * time.Second}
	}
	if config.RateInterval == 0 {
		config.RateInterval = 500 * time.Millisecond
	}
	return &Client{
		baseURL: config.BaseURL, coverURL: config.CoverURL, auth: strings.TrimSpace(config.Auth),
		userAgent: config.UserAgent, http: config.Client, gate: providers.NewGate(config.RateInterval),
	}
}

// ResetConfig restores the public LrcApi defaults and clears its optional
// authorization value while keeping the injected HTTP client.
func (c *Client) ResetConfig() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = "https://api.lrc.cx/jsonapi"
	c.coverURL = "https://api.lrc.cx/cover"
	c.auth = ""
	c.userAgent = providers.DefaultUserAgent("lrcapi")
	c.gate.SetInterval(500 * time.Millisecond)
	return nil
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{
		ID: "lrcapi", Name: "LrcApi 聚合", ShortName: "LA",
		Description:  "可自托管的歌词与封面聚合接口",
		Capabilities: []string{"歌词", "同步歌词", "封面"},
		Health:       providers.HealthDegraded, Enabled: false, Experimental: true,
		Accent: "#8067d8", QuotaLabel: "实验性 · 可配置自托管地址",
	}
}

func (c *Client) ConfigFields() []providers.ConfigField {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return []providers.ConfigField{
		{Key: "baseUrl", Label: "歌词 JSON API URL", Type: "url", Value: c.baseURL, Required: true},
		{Key: "coverUrl", Label: "封面 API URL", Type: "url", Value: c.coverURL, Required: true},
		{Key: "auth", Label: "Authorization", Type: "password", Value: c.auth, Secret: true, Placeholder: "可选鉴权令牌"},
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
		case "coverUrl":
			endpoint, err := providers.ValidateHTTPURL(value, "coverUrl")
			if err != nil {
				return err
			}
			c.coverURL = endpoint
		case "auth":
			c.auth = strings.TrimSpace(value)
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
	if err := c.gate.Wait(ctx); err != nil {
		return nil, err
	}
	endpoint, err := withQuery(c.baseURL, query)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	if c.auth != "" {
		headers["Authorization"] = c.auth
	}
	var raw json.RawMessage
	if err := providers.GetJSONWithHeaders(ctx, c.http, endpoint, c.userAgent, headers, &raw); err != nil {
		return nil, err
	}
	items, err := responseItems(raw)
	if err != nil {
		return nil, err
	}
	result := make([]providers.Candidate, 0, min(limit, len(items)))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		candidate := c.mapItem(item, query)
		if candidate.Title == "" {
			candidate.Title = query.Title
		}
		if candidate.ExternalID == "" {
			candidate.ExternalID = stableID(candidate.Title, candidate.Artists, candidate.Album, candidate.Lyrics)
		}
		if _, exists := seen[candidate.ExternalID]; exists {
			continue
		}
		seen[candidate.ExternalID] = struct{}{}
		result = append(result, candidate)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (c *Client) mapItem(item map[string]any, query providers.Query) providers.Candidate {
	title := firstString(item, "title", "track", "trackName", "song", "name")
	artists := stringList(item["artists"])
	if len(artists) == 0 {
		artists = splitArtists(firstString(item, "artist", "artists", "singer"))
	}
	album := firstString(item, "album", "albumName", "collection")
	lyrics := providers.NormalizeLyrics(firstString(item, "lyrics", "lyric", "lrc", "syncedLyrics"))
	cover := firstString(item, "cover", "artwork", "artworkUrl", "coverUrl", "image")
	if cover == "" && (title != "" || query.Title != "") {
		cover = c.coverURLFor(firstNonEmpty(title, query.Title), artists, album)
	}
	duration := parseSeconds(item["duration"])
	if duration == 0 {
		duration = parseSeconds(item["durationSeconds"])
	}
	externalID := firstString(item, "id", "externalId", "hash", "songId")
	return providers.Candidate{
		ProviderID: "lrcapi", ExternalID: externalID, Title: title, Artists: artists,
		Album: album, AlbumArtists: append([]string(nil), artists...), DurationSeconds: duration,
		Lyrics: lyrics, SyncedLyrics: lyrics, ArtworkURL: cover,
	}
}

func (c *Client) coverURLFor(title string, artists []string, album string) string {
	endpoint, err := url.Parse(c.coverURL)
	if err != nil {
		return ""
	}
	values := endpoint.Query()
	values.Set("title", title)
	if len(artists) > 0 {
		values.Set("artist", strings.Join(artists, ", "))
	}
	if album != "" {
		values.Set("album", album)
	}
	endpoint.RawQuery = values.Encode()
	return endpoint.String()
}

func withQuery(base string, query providers.Query) (string, error) {
	endpoint, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	values := endpoint.Query()
	values.Set("title", query.Title)
	if len(query.Artists) > 0 {
		values.Set("artist", strings.Join(query.Artists, ", "))
	}
	if query.Album != "" {
		values.Set("album", query.Album)
	}
	endpoint.RawQuery = values.Encode()
	return endpoint.String(), nil
}

func responseItems(raw json.RawMessage) ([]map[string]any, error) {
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err == nil {
		return items, nil
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	for _, key := range []string{"data", "results", "songs", "lyrics"} {
		if value, ok := envelope[key]; ok {
			payload, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				return nil, marshalErr
			}
			if json.Unmarshal(payload, &items) == nil {
				return items, nil
			}
			var item map[string]any
			if json.Unmarshal(payload, &item) == nil && len(item) > 0 {
				return []map[string]any{item}, nil
			}
		}
	}
	return []map[string]any{envelope}, nil
}

func firstString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			switch typed := value.(type) {
			case string:
				if value := strings.TrimSpace(typed); value != "" {
					return value
				}
			case float64:
				if typed != 0 {
					return strconv.FormatInt(int64(typed), 10)
				}
			}
		}
	}
	return ""
}

func stringList(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func splitArtists(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == ',' || r == '，' || r == '&' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func parseSeconds(value any) int64 {
	switch typed := value.(type) {
	case float64:
		if typed > 10000 {
			typed /= 1000
		}
		return int64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			if parsed > 10000 {
				parsed /= 1000
			}
			return int64(parsed)
		}
	}
	return 0
}

func stableID(title string, artists []string, album, lyrics string) string {
	hash := sha256.Sum256([]byte(title + "\x00" + strings.Join(artists, ",") + "\x00" + album + "\x00" + lyrics))
	return hex.EncodeToString(hash[:8])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
