package musicbrainz

import (
	"context"
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
	BaseURL                string
	ArchiveDownloadBaseURL string
	UserAgent              string
	Client                 *http.Client
	RateInterval           time.Duration
}

type Client struct {
	mu                     sync.RWMutex
	baseURL                string
	archiveDownloadBaseURL string
	userAgent              string
	http                   *http.Client
	gate                   *providers.Gate
}

func New(config Config) *Client {
	if config.BaseURL == "" {
		config.BaseURL = "https://musicbrainz.org/ws/2/recording/"
	}
	if config.ArchiveDownloadBaseURL == "" {
		config.ArchiveDownloadBaseURL = providers.DefaultArchiveDownloadBaseURL
	}
	if config.UserAgent == "" {
		config.UserAgent = providers.DefaultUserAgent("musicbrainz")
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if config.RateInterval == 0 {
		config.RateInterval = time.Second
	}
	return &Client{
		baseURL: config.BaseURL, archiveDownloadBaseURL: config.ArchiveDownloadBaseURL,
		userAgent: config.UserAgent, http: config.Client, gate: providers.NewGate(config.RateInterval),
	}
}

// ResetConfig restores the provider's built-in endpoints and transport
// defaults while keeping the injected HTTP client intact.
func (c *Client) ResetConfig() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = "https://musicbrainz.org/ws/2/recording/"
	c.archiveDownloadBaseURL = providers.DefaultArchiveDownloadBaseURL
	c.userAgent = providers.DefaultUserAgent("musicbrainz")
	c.gate.SetInterval(time.Second)
	return nil
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{
		ID: "musicbrainz", Name: "MusicBrainz", ShortName: "MB",
		Description:  "开放、结构化的发行与艺人资料",
		Capabilities: []string{"歌曲", "专辑", "音轨", "封面", "外部 ID"},
		Health:       providers.HealthReady, Enabled: true, Accent: "#e84b2c", QuotaLabel: "1 req/s · 官方 API",
	}
}

func (c *Client) ConfigFields() []providers.ConfigField {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return []providers.ConfigField{
		{Key: "baseUrl", Label: "API Base URL", Type: "url", Value: c.baseURL, Required: true, Description: "MusicBrainz recording 查询地址"},
		{Key: "archiveDownloadBaseUrl", Label: "Internet Archive 下载基址", Type: "url", Value: c.archiveDownloadBaseURL, Required: true, Description: "支持镜像 origin 或带路径的代理前缀；末尾会拼接 archive.org 的 /download/ 路径"},
		{Key: "userAgent", Label: "User-Agent", Type: "text", Value: c.userAgent, Required: true, Description: "请保留可联系的应用标识"},
		{Key: "rateIntervalMs", Label: "请求间隔（毫秒）", Type: "number", Value: strconv.FormatInt(c.gate.Interval().Milliseconds(), 10), Description: "避免触发官方 API 限流"},
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
			c.baseURL = strings.TrimRight(endpoint, "/") + "/"
		case "archiveDownloadBaseUrl":
			endpoint, err := providers.ValidateArtworkDownloadBaseURL(value, "archiveDownloadBaseUrl")
			if err != nil {
				return err
			}
			c.archiveDownloadBaseURL = endpoint
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

func (c *Client) ArtworkDownloadOptions() providers.ArtworkDownloadOptions {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return providers.ArtworkDownloadOptions{ArchiveDownloadBaseURL: c.archiveDownloadBaseURL}
}

func (c *Client) Search(ctx context.Context, query providers.Query, limit int) ([]providers.Candidate, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
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
		artistIDs := make([]string, 0, len(recording.ArtistCredit))
		for _, credit := range recording.ArtistCredit {
			if credit.Artist.ID != "" {
				artistIDs = append(artistIDs, credit.Artist.ID)
			}
		}
		releaseID := ""
		if len(recording.Releases) > 0 {
			releaseID = recording.Releases[0].ID
		}
		result = append(result, providers.Candidate{
			ProviderID: "musicbrainz", ExternalID: recording.ID, Title: recording.Title,
			Artists: artists, Album: album, AlbumArtists: artists, Year: year(recording.FirstReleaseDate),
			MusicBrainzTrackID: recording.ID, MusicBrainzReleaseID: releaseID, MusicBrainzArtistIDs: artistIDs,
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
				ID   string `json:"id"`
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
