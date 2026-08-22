package kuwo

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
		config.Endpoint = "https://search.kuwo.cn/r.s"
	}
	if config.UserAgent == "" {
		config.UserAgent = "Tagger/0.1 (experimental kuwo adapter)"
	}
	return &Client{config: config, http: config.Client}
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{ID: "kuwo", Name: "酷我音乐", ShortName: "KW", Description: "中文曲库与封面实验性来源", Capabilities: []string{"歌曲", "专辑", "音轨", "封面"}, Health: providers.HealthDegraded, Enabled: false, Experimental: true, Accent: "#d69e2e", QuotaLabel: "实验性网页接口 · 默认关闭"}
}

func (c *Client) Search(ctx context.Context, query providers.Query, limit int) ([]providers.Candidate, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	keyword := strings.TrimSpace(strings.Join(append([]string{query.Title}, query.Artists...), " "))
	values := url.Values{"client": {"kt"}, "ft": {"music"}, "cluster": {"0"}, "strategy": {"2012"}, "encoding": {"utf8"}, "rformat": {"json"}, "mobi": {"1"}, "issubtitle": {"1"}, "pn": {"0"}, "rn": {strconv.Itoa(limit)}, "all": {keyword}}
	var response struct {
		Items []struct {
			MusicRID             string `json:"MUSICRID"`
			SongName             string `json:"SONGNAME"`
			Artist               string `json:"ARTIST"`
			Album                string `json:"ALBUM"`
			AlbumArtist          string `json:"ALBUMARTIST"`
			Duration             string `json:"SONG_DURATION"`
			TrackNumber          int    `json:"TRACKNUM"`
			AlbumPicture         string `json:"ALBUMPIC"`
			AlbumPictureShort    string `json:"ALBUMPIC_SHORT"`
			WebAlbumPicture      string `json:"web_albumpic"`
			WebAlbumPictureShort string `json:"web_albumpic_short"`
			Picture              string `json:"PIC"`
		} `json:"abslist"`
	}
	if err := providers.GetJSON(ctx, c.http, c.config.Endpoint+"?"+values.Encode(), c.config.UserAgent, &response); err != nil {
		return nil, err
	}
	result := make([]providers.Candidate, 0, len(response.Items))
	for _, item := range response.Items {
		id := strings.TrimPrefix(item.MusicRID, "MUSIC_")
		if id == "" {
			continue
		}
		result = append(result, providers.Candidate{ProviderID: "kuwo", ExternalID: id, Title: item.SongName, Artists: splitArtists(item.Artist), Album: item.Album, AlbumArtists: splitArtists(item.AlbumArtist), TrackNumber: item.TrackNumber, DurationSeconds: parseDuration(item.Duration), ArtworkURL: normalizeArtworkURL(item.AlbumPicture, item.WebAlbumPicture, item.WebAlbumPictureShort, item.AlbumPictureShort, item.Picture)})
	}
	return result, nil
}

func normalizeArtworkURL(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "//") {
			return "https:" + value
		}
		if strings.HasPrefix(strings.ToLower(value), "http://") {
			return "https://" + value[len("http://"):]
		}
		if strings.HasPrefix(strings.ToLower(value), "https://") {
			return value
		}
		value = strings.TrimPrefix(value, "/")
		value = strings.TrimPrefix(value, "star/albumcover/")
		// Kuwo's search API normally returns the 120px short path. The same
		// path can be requested at 500px without another metadata lookup.
		if strings.HasPrefix(value, "120/") {
			value = "500/" + strings.TrimPrefix(value, "120/")
		}
		return "https://img1.kwcdn.kuwo.cn/star/albumcover/" + value
	}
	return ""
}

func splitArtists(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '&' || r == ',' || r == '/' || r == '、' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, strings.TrimSpace(part))
		}
	}
	return result
}
func parseDuration(value string) int64 {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0
	}
	minutes, _ := strconv.ParseInt(parts[0], 10, 64)
	seconds, _ := strconv.ParseInt(parts[1], 10, 64)
	return minutes*60 + seconds
}
