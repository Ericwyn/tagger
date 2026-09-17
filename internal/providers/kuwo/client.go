package kuwo

import (
	"context"
	"encoding/json"
	"encoding/xml"
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
	Client             *http.Client
	Endpoint           string
	LyricsEndpoint     string
	LyricsRIDEndpoint  string
	LyricsFileEndpoint string
	UserAgent          string
	Auth               string
	Cookie             string
	RateInterval       time.Duration
}
type Client struct {
	mu       sync.RWMutex
	config   Config
	proxyURL string
	baseHTTP *http.Client
	http     *http.Client
	gate     *providers.Gate
}

func New(config Config) *Client {
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if config.Endpoint == "" {
		config.Endpoint = "https://search.kuwo.cn/r.s"
	}
	if config.LyricsEndpoint == "" {
		config.LyricsEndpoint = "https://www.kuwo.cn/newh5/singles/songinfoandlrc"
	}
	if config.LyricsRIDEndpoint == "" {
		config.LyricsRIDEndpoint = "https://player.kuwo.cn/webmusic/st/getNewMuiseByRid"
	}
	if config.LyricsFileEndpoint == "" {
		config.LyricsFileEndpoint = "https://newlyric.kuwo.cn/newlyric.lrc"
	}
	if config.UserAgent == "" {
		config.UserAgent = providers.DefaultUserAgent("kuwo")
	}
	if config.RateInterval == 0 {
		config.RateInterval = 180 * time.Millisecond
	}
	gate := providers.NewGate(config.RateInterval)
	return &Client{config: config, baseHTTP: config.Client, http: providers.WrapHTTPClient(config.Client, gate), gate: gate}
}

// ResetConfig restores the web endpoints and clears optional credentials
// without replacing the HTTP client.
func (c *Client) ResetConfig() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Endpoint = "https://search.kuwo.cn/r.s"
	c.config.LyricsEndpoint = "https://www.kuwo.cn/newh5/singles/songinfoandlrc"
	c.config.LyricsRIDEndpoint = "https://player.kuwo.cn/webmusic/st/getNewMuiseByRid"
	c.config.LyricsFileEndpoint = "https://newlyric.kuwo.cn/newlyric.lrc"
	c.config.UserAgent = providers.DefaultUserAgent("kuwo")
	c.config.Auth = ""
	c.config.Cookie = ""
	c.gate.SetInterval(180 * time.Millisecond)
	return c.setProxyLocked("")
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{ID: "kuwo", Name: "酷我音乐", ShortName: "KW", Description: "中文曲库、同步歌词与封面实验性来源", Capabilities: []string{"歌曲", "专辑", "音轨", "歌词", "同步歌词", "封面"}, Health: providers.HealthDegraded, Enabled: false, Experimental: true, Accent: "#d69e2e", QuotaLabel: "实验性网页接口 · 默认关闭"}
}

func (c *Client) ConfigFields() []providers.ConfigField {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return []providers.ConfigField{
		{Key: "endpoint", Label: "搜索 API URL", Type: "url", Value: c.config.Endpoint, Required: true},
		{Key: "lyricsEndpoint", Label: "歌词 JSON URL", Type: "url", Value: c.config.LyricsEndpoint, Required: true},
		{Key: "lyricsRidEndpoint", Label: "歌词 RID URL", Type: "url", Value: c.config.LyricsRIDEndpoint, Required: true},
		{Key: "lyricsFileEndpoint", Label: "歌词文件 URL", Type: "url", Value: c.config.LyricsFileEndpoint, Required: true},
		{Key: "userAgent", Label: "User-Agent", Type: "text", Value: c.config.UserAgent, Required: true},
		{Key: "auth", Label: "鉴权头（可选）", Type: "password", Value: c.config.Auth, Secret: true, Placeholder: "Bearer …"},
		{Key: "cookie", Label: "Cookie（可选）", Type: "password", Value: c.config.Cookie, Secret: true, Placeholder: "kw_token=…"},
		providers.ProxyConfigField(c.proxyURL),
		{Key: "rateIntervalMs", Label: "请求间隔（毫秒）", Type: "number", Value: strconv.FormatInt(c.gate.Interval().Milliseconds(), 10)},
	}
}

func (c *Client) Configure(values map[string]string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, value := range values {
		switch key {
		case "endpoint":
			endpoint, err := providers.ValidateHTTPURL(value, "endpoint")
			if err != nil {
				return err
			}
			c.config.Endpoint = endpoint
		case "lyricsEndpoint":
			endpoint, err := providers.ValidateHTTPURL(value, "lyricsEndpoint")
			if err != nil {
				return err
			}
			c.config.LyricsEndpoint = endpoint
		case "lyricsRidEndpoint":
			endpoint, err := providers.ValidateHTTPURL(value, "lyricsRidEndpoint")
			if err != nil {
				return err
			}
			c.config.LyricsRIDEndpoint = endpoint
		case "lyricsFileEndpoint":
			endpoint, err := providers.ValidateHTTPURL(value, "lyricsFileEndpoint")
			if err != nil {
				return err
			}
			c.config.LyricsFileEndpoint = endpoint
		case "userAgent":
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("userAgent 不能为空")
			}
			c.config.UserAgent = strings.TrimSpace(value)
		case "auth":
			c.config.Auth = strings.TrimSpace(value)
		case "cookie":
			c.config.Cookie = strings.TrimSpace(value)
		case "proxyUrl":
			if err := c.setProxyLocked(value); err != nil {
				return err
			}
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

func (c *Client) setProxyLocked(value string) error {
	normalized, err := providers.ValidateProxyURL(value)
	if err != nil {
		return err
	}
	configured, err := providers.HTTPClientWithProxy(c.baseHTTP, c.gate, normalized)
	if err != nil {
		return err
	}
	previous := c.http
	c.http = configured
	c.proxyURL = normalized
	if previous != nil {
		previous.CloseIdleConnections()
	}
	return nil
}

func (c *Client) ArtworkDownloadOptions() providers.ArtworkDownloadOptions {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return providers.ArtworkDownloadOptions{ProxyURL: c.proxyURL, Gate: c.gate}
}

func (c *Client) Search(ctx context.Context, query providers.Query, limit int) ([]providers.Candidate, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	if strings.TrimSpace(query.Title) == "" {
		return []providers.Candidate{}, nil
	}
	keywords := searchKeywords(query)
	fetchLimit := limit * 4
	if fetchLimit < 20 {
		fetchLimit = 20
	}
	if fetchLimit > 100 {
		fetchLimit = 100
	}
	items := make([]kuwoItem, 0, fetchLimit)
	seen := make(map[string]struct{}, fetchLimit)
	var firstErr error
	for index, keyword := range keywords {
		values := url.Values{"client": {"kt"}, "ft": {"music"}, "cluster": {"0"}, "strategy": {"2012"}, "encoding": {"utf8"}, "rformat": {"json"}, "mobi": {"1"}, "issubtitle": {"1"}, "pn": {"0"}, "rn": {strconv.Itoa(fetchLimit)}, "all": {keyword}}
		var response struct {
			Items   []kuwoItem      `json:"abslist"`
			Code    json.RawMessage `json:"code"`
			Status  json.RawMessage `json:"status"`
			Message string          `json:"msg"`
		}
		if err := providers.GetJSONWithHeaders(ctx, c.http, c.config.Endpoint+"?"+values.Encode(), c.config.UserAgent, kuwoHeaders(c.config.Auth, c.config.Cookie), &response); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if index == 0 {
				return nil, err
			}
			continue
		}
		if err := kuwoBusinessError(response.Code, response.Status, response.Message); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if index == 0 {
				return nil, err
			}
			continue
		}
		for _, item := range response.Items {
			id := strings.TrimPrefix(item.MusicRID, "MUSIC_")
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			items = append(items, item)
		}
		if len(items) >= fetchLimit {
			break
		}
	}
	if len(items) == 0 && firstErr != nil {
		return nil, firstErr
	}
	result := make([]providers.Candidate, 0, min(limit, len(items)))
	for _, item := range items {
		id := strings.TrimPrefix(item.MusicRID, "MUSIC_")
		if id == "" {
			continue
		}
		candidate := providers.Candidate{ProviderID: "kuwo", ExternalID: id, Title: item.SongName, Artists: splitArtists(item.Artist), Album: item.Album, AlbumArtists: splitArtists(item.AlbumArtist), TrackNumber: item.TrackNumber, DurationSeconds: parseDuration(item.Duration), ArtworkURL: normalizeArtworkURL(item.AlbumPicture, item.WebAlbumPicture, item.WebAlbumPictureShort, item.AlbumPictureShort, item.Picture)}
		if lyrics, lyricsErr := c.fetchLyrics(ctx, id); lyricsErr == nil {
			candidate.Lyrics = lyrics
			candidate.SyncedLyrics = lyrics
		}
		result = append(result, candidate)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

type lyricLine struct {
	LineLyric string          `json:"lineLyric"`
	Lyric     string          `json:"lyric"`
	Time      json.RawMessage `json:"time"`
}

type kuwoItem struct {
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
}

type lyricPayload struct {
	Code    json.RawMessage `json:"code"`
	Status  json.RawMessage `json:"status"`
	Message string          `json:"msg"`
	Data    struct {
		LRCList []lyricLine `json:"lrclist"`
		Lyrics  string      `json:"lyrics"`
		LRC     struct {
			Lyric   string `json:"lyric"`
			Content string `json:"content"`
		} `json:"lrc"`
	} `json:"data"`
	LRCList []lyricLine `json:"lrclist"`
}

// fetchLyrics uses Kuwo's current JSON endpoint first and keeps the older RID
// + lyric-key flow as a compatibility fallback. Both endpoints are public web
// interfaces and may change independently, so lyric failures never make an
// otherwise valid song candidate disappear.
func (c *Client) fetchLyrics(ctx context.Context, id string) (string, error) {
	numericID := strings.TrimPrefix(strings.TrimSpace(id), "MUSIC_")
	values := url.Values{"musicId": {numericID}}
	body, primaryErr := c.getBody(ctx, c.config.LyricsEndpoint+"?"+values.Encode(), map[string]string{"Accept": "application/json"})
	if primaryErr == nil {
		var payload lyricPayload
		if err := json.Unmarshal(body, &payload); err == nil {
			if businessErr := kuwoBusinessError(payload.Code, payload.Status, payload.Message); businessErr != nil {
				primaryErr = businessErr
			} else {
				lyrics := firstLyrics(
					payload.Data.Lyrics,
					payload.Data.LRC.Lyric,
					payload.Data.LRC.Content,
					renderLyricLines(payload.Data.LRCList),
					renderLyricLines(payload.LRCList),
				)
				if providers.HasLyrics(lyrics) {
					return lyrics, nil
				}
			}
		}
	}

	ridValues := url.Values{"rid": {"MUSIC_" + numericID}}
	ridBody, ridErr := c.getBody(ctx, c.config.LyricsRIDEndpoint+"?"+ridValues.Encode(), map[string]string{"Accept": "application/xml, text/xml"})
	if ridErr == nil {
		if lyricKey := lyricKeyFromXML(ridBody); lyricKey != "" {
			lyricBody, lyricErr := c.getBody(ctx, c.config.LyricsFileEndpoint+"?"+lyricKey, map[string]string{"Accept": "text/plain, text/html"})
			if lyricErr == nil {
				lyrics := providers.NormalizeLyrics(string(lyricBody))
				if providers.HasLyrics(lyrics) {
					return lyrics, nil
				}
			}
		}
	}
	if primaryErr != nil {
		return "", primaryErr
	}
	return "", ridErr
}

func (c *Client) getBody(ctx context.Context, endpoint string, headers map[string]string) ([]byte, error) {
	requestHeaders := kuwoHeaders(c.config.Auth, c.config.Cookie)
	for name, value := range headers {
		requestHeaders[name] = value
	}
	return providers.GetBytesWithHeaders(ctx, c.http, endpoint, c.config.UserAgent, requestHeaders, 2<<20)
}

func firstLyrics(values ...string) string {
	for _, value := range values {
		if normalized := providers.NormalizeLyrics(value); providers.HasLyrics(normalized) {
			return normalized
		}
	}
	return ""
}

func lyricKeyFromXML(body []byte) string {
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attribute := range start.Attr {
			if strings.EqualFold(attribute.Name.Local, "lyric") && strings.TrimSpace(attribute.Value) != "" {
				return strings.TrimSpace(attribute.Value)
			}
		}
	}
}

func renderLyricLines(lines []lyricLine) string {
	if len(lines) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, line := range lines {
		text := strings.TrimSpace(line.LineLyric)
		if text == "" {
			text = strings.TrimSpace(line.Lyric)
		}
		if text == "" {
			continue
		}
		if strings.Contains(text, "[") {
			builder.WriteString(text)
		} else if seconds, ok := parseLyricTime(line.Time); ok {
			minutes := int(seconds) / 60
			remaining := seconds - float64(minutes*60)
			fmt.Fprintf(&builder, "[%02d:%05.2f]%s", minutes, remaining, text)
		} else {
			builder.WriteString(text)
		}
		builder.WriteByte('\n')
	}
	return providers.NormalizeLyrics(builder.String())
}

func parseLyricTime(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		if number > 10000 {
			number /= 1000
		}
		return number, number >= 0
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false
	}
	text = strings.TrimSpace(text)
	if strings.Contains(text, ":") {
		parts := strings.Split(text, ":")
		if len(parts) == 2 {
			minutes, minuteErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			seconds, secondErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if minuteErr == nil && secondErr == nil {
				return minutes*60 + seconds, true
			}
		}
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	if value > 10000 {
		value /= 1000
	}
	return value, value >= 0
}

func normalizeArtworkURL(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "//") {
			value = "https:" + value
		}
		if strings.HasPrefix(strings.ToLower(value), "http://") {
			value = "https://" + value[len("http://"):]
		}
		if strings.HasPrefix(strings.ToLower(value), "https://") {
			if parsed, err := url.Parse(value); err == nil && strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".kwcdn.kuwo.cn") {
				// The kwcdn hostname currently serves a certificate for an unrelated
				// Tencent CDN domain. Kuwo exposes the same image paths from img4,
				// whose certificate and current API examples are valid.
				parsed.Host = "img4.kuwo.cn"
				return parsed.String()
			}
			return value
		}
		value = strings.TrimPrefix(value, "/")
		value = strings.TrimPrefix(value, "star/albumcover/")
		// Kuwo's search API normally returns the 120px short path. The same
		// path can be requested at 500px without another metadata lookup.
		if strings.HasPrefix(value, "120/") {
			value = "500/" + strings.TrimPrefix(value, "120/")
		}
		return "https://img4.kuwo.cn/star/albumcover/" + value
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

func searchKeywords(query providers.Query) []string {
	title := strings.TrimSpace(query.Title)
	if title == "" {
		return nil
	}
	artists := strings.TrimSpace(strings.Join(query.Artists, " "))
	keywords := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		keywords = append(keywords, value)
	}
	if artists != "" {
		add(title + " " + artists)
	}
	if album := strings.TrimSpace(query.Album); album != "" {
		add(title + " " + album)
	}
	add(title)
	return keywords
}

func kuwoHeaders(auth, cookie string) map[string]string {
	result := map[string]string{"Referer": "https://www.kuwo.cn/", "Origin": "https://www.kuwo.cn"}
	if strings.TrimSpace(auth) != "" {
		result["Authorization"] = strings.TrimSpace(auth)
	}
	if strings.TrimSpace(cookie) != "" {
		result["Cookie"] = strings.TrimSpace(cookie)
	}
	return result
}

func kuwoBusinessError(code, status json.RawMessage, message string) error {
	for label, raw := range map[string]json.RawMessage{"code": code, "status": status} {
		value := strings.TrimSpace(string(raw))
		if value == "" || value == "null" {
			continue
		}
		value = strings.Trim(value, `"`)
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "error" || lower == "fail" || lower == "failed" || lower == "unauthorized" || lower == "forbidden" || lower == "ratelimit" || lower == "rate_limited" {
			return &providers.BusinessError{Provider: "kuwo", Code: label + "=" + value, Message: message}
		}
		number, err := strconv.Atoi(lower)
		if err == nil && (number < 0 || number == 401 || number == 403 || number == 429 || number >= 500) {
			return &providers.BusinessError{Provider: "kuwo", Code: label + "=" + value, Message: message, Retryable: number == 429 || number >= 500}
		}
	}
	return nil
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
