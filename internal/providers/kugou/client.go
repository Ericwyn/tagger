package kugou

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ericwyn/tagger/internal/providers"
)

type Config struct {
	Client            *http.Client
	SearchEndpoint    string
	LyricsSearchURL   string
	LyricsDownloadURL string
	ArtworkEndpoint   string
	UserAgent         string
	Auth              string
	Cookie            string
	RateInterval      time.Duration
}

type Client struct {
	mu     sync.RWMutex
	config Config
	http   *http.Client
	gate   *providers.Gate
}

func New(config Config) *Client {
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if config.SearchEndpoint == "" {
		config.SearchEndpoint = "https://mobilecdn.kugou.com/api/v3/search/song"
	}
	if config.LyricsSearchURL == "" {
		config.LyricsSearchURL = "https://krcs.kugou.com/search"
	}
	if config.LyricsDownloadURL == "" {
		config.LyricsDownloadURL = "https://lyrics.kugou.com/download"
	}
	if config.ArtworkEndpoint == "" {
		config.ArtworkEndpoint = "https://wwwapi.kugou.com/yy/index.php"
	}
	if config.UserAgent == "" {
		config.UserAgent = "Tagger/0.1 (experimental kugou adapter)"
	}
	if config.RateInterval == 0 {
		config.RateInterval = 180 * time.Millisecond
	}
	return &Client{config: config, http: config.Client, gate: providers.NewGate(config.RateInterval)}
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{
		ID: "kugou", Name: "酷狗音乐", ShortName: "KG",
		Description:  "中文曲库、LRC 歌词与歌曲封面",
		Capabilities: []string{"歌曲", "专辑", "歌词", "同步歌词", "封面"},
		Health:       providers.HealthDegraded, Enabled: false, Experimental: true,
		Accent: "#14a86b", QuotaLabel: "实验性网页接口 · 设置中启用",
	}
}

func (c *Client) ConfigFields() []providers.ConfigField {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return []providers.ConfigField{
		{Key: "searchEndpoint", Label: "搜索 API URL", Type: "url", Value: c.config.SearchEndpoint, Required: true},
		{Key: "lyricsSearchUrl", Label: "歌词搜索 URL", Type: "url", Value: c.config.LyricsSearchURL, Required: true},
		{Key: "lyricsDownloadUrl", Label: "歌词下载 URL", Type: "url", Value: c.config.LyricsDownloadURL, Required: true},
		{Key: "artworkEndpoint", Label: "封面 API URL", Type: "url", Value: c.config.ArtworkEndpoint, Required: true},
		{Key: "userAgent", Label: "User-Agent", Type: "text", Value: c.config.UserAgent, Required: true},
		{Key: "auth", Label: "鉴权头（可选）", Type: "password", Value: c.config.Auth, Secret: true, Placeholder: "Bearer …"},
		{Key: "cookie", Label: "Cookie（可选）", Type: "password", Value: c.config.Cookie, Secret: true, Placeholder: "kg_mid=…"},
		{Key: "rateIntervalMs", Label: "请求间隔（毫秒）", Type: "number", Value: strconv.FormatInt(c.gate.Interval().Milliseconds(), 10)},
	}
}

func (c *Client) Configure(values map[string]string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, value := range values {
		switch key {
		case "searchEndpoint":
			endpoint, err := providers.ValidateHTTPURL(value, "searchEndpoint")
			if err != nil {
				return err
			}
			c.config.SearchEndpoint = endpoint
		case "lyricsSearchUrl":
			endpoint, err := providers.ValidateHTTPURL(value, "lyricsSearchUrl")
			if err != nil {
				return err
			}
			c.config.LyricsSearchURL = endpoint
		case "lyricsDownloadUrl":
			endpoint, err := providers.ValidateHTTPURL(value, "lyricsDownloadUrl")
			if err != nil {
				return err
			}
			c.config.LyricsDownloadURL = endpoint
		case "artworkEndpoint":
			endpoint, err := providers.ValidateHTTPURL(value, "artworkEndpoint")
			if err != nil {
				return err
			}
			c.config.ArtworkEndpoint = endpoint
		case "userAgent":
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("userAgent 不能为空")
			}
			c.config.UserAgent = strings.TrimSpace(value)
		case "auth":
			c.config.Auth = strings.TrimSpace(value)
		case "cookie":
			c.config.Cookie = strings.TrimSpace(value)
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
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	if strings.TrimSpace(query.Title) == "" {
		return []providers.Candidate{}, nil
	}
	if err := c.gate.Wait(ctx); err != nil {
		return nil, err
	}
	keywords := searchKeywords(query)
	var items []songItem
	var searchErr error
	for index, keyword := range keywords {
		if index > 0 {
			if err := c.gate.Wait(ctx); err != nil {
				return nil, err
			}
		}
		values := url.Values{
			"format":   {"json"},
			"keyword":  {keyword},
			"page":     {"1"},
			"pagesize": {strconv.Itoa(max(limit*4, 20))},
			"showtype": {"1"},
		}
		var response searchResponse
		if err := providers.GetJSONWithHeaders(ctx, c.http, c.config.SearchEndpoint+"?"+values.Encode(), c.config.UserAgent, kugouHeaders(c.config.Auth, c.config.Cookie), &response); err != nil {
			searchErr = err
			if index == 0 {
				return nil, err
			}
			continue
		}
		if err := response.businessError(); err != nil {
			searchErr = err
			if index == 0 {
				return nil, err
			}
			continue
		}
		items = response.Data.Info
		if len(items) > 0 {
			break
		}
	}
	if len(items) == 0 {
		if searchErr != nil {
			return nil, searchErr
		}
		return []providers.Candidate{}, nil
	}
	sort.SliceStable(items, func(left, right int) bool {
		return songScore(query, items[left]) > songScore(query, items[right])
	})

	result := make([]providers.Candidate, 0, min(limit, len(items)))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		hash := strings.TrimSpace(item.Hash)
		if hash == "" {
			continue
		}
		if _, exists := seen[strings.ToLower(hash)]; exists {
			continue
		}
		seen[strings.ToLower(hash)] = struct{}{}
		candidate := mapCandidate(item)
		lyrics, _ := c.fetchLyrics(ctx, hash)
		candidate.SyncedLyrics = lyrics
		candidate.Lyrics = lyrics
		if artwork, _ := c.fetchArtwork(ctx, hash, item.AlbumID.String()); artwork != "" {
			candidate.ArtworkURL = artwork
		}
		result = append(result, candidate)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (c *Client) fetchLyrics(ctx context.Context, hash string) (string, error) {
	if err := c.gate.Wait(ctx); err != nil {
		return "", err
	}
	values := url.Values{
		"ver":            {"1"},
		"man":            {"yes"},
		"client":         {"pc"},
		"keyword":        {""},
		"duration":       {""},
		"hash":           {hash},
		"album_audio_id": {""},
	}
	var search lyricSearchResponse
	if err := providers.GetJSONWithHeaders(ctx, c.http, c.config.LyricsSearchURL+"?"+values.Encode(), c.config.UserAgent, kugouHeaders(c.config.Auth, c.config.Cookie), &search); err != nil {
		return "", err
	}
	if err := search.businessError(); err != nil {
		return "", err
	}
	if len(search.Candidates) == 0 {
		return "", nil
	}
	lyricID := search.Candidates[0].ID.String()
	accessKey := strings.TrimSpace(search.Candidates[0].AccessKey)
	if lyricID == "" || accessKey == "" {
		return "", nil
	}
	if err := c.gate.Wait(ctx); err != nil {
		return "", err
	}
	values = url.Values{
		"ver":       {"1"},
		"client":    {"pc"},
		"id":        {lyricID},
		"accesskey": {accessKey},
		"fmt":       {"lrc"},
		"charset":   {"utf8"},
	}
	var download lyricDownloadResponse
	if err := providers.GetJSONWithHeaders(ctx, c.http, c.config.LyricsDownloadURL+"?"+values.Encode(), c.config.UserAgent, kugouHeaders(c.config.Auth, c.config.Cookie), &download); err != nil {
		return "", err
	}
	if err := download.businessError(); err != nil {
		return "", err
	}
	return decodeLyrics(download.Content)
}

func (c *Client) fetchArtwork(ctx context.Context, hash, albumID string) (string, error) {
	if strings.TrimSpace(albumID) == "" {
		return "", nil
	}
	if err := c.gate.Wait(ctx); err != nil {
		return "", err
	}
	values := url.Values{
		"r":        {"play/getdata"},
		"hash":     {hash},
		"album_id": {albumID},
		"_":        {strconv.FormatInt(time.Now().UnixMilli(), 10)},
	}
	var response artworkResponse
	if err := providers.GetJSONWithHeaders(ctx, c.http, c.config.ArtworkEndpoint+"?"+values.Encode(), c.config.UserAgent, kugouHeaders(c.config.Auth, c.config.Cookie), &response); err != nil {
		return "", err
	}
	if err := response.businessError(); err != nil {
		return "", err
	}
	imageURL := strings.TrimSpace(response.Data.Image)
	imageURL = strings.TrimPrefix(imageURL, "http://")
	if imageURL != "" && !strings.HasPrefix(imageURL, "https://") {
		imageURL = "https://" + imageURL
	}
	return imageURL, nil
}

func mapCandidate(item songItem) providers.Candidate {
	artists := splitArtists(item.SingerName)
	duration := item.Duration
	if duration.String() == "" {
		duration = item.TimeLength
	}
	if duration.String() == "" {
		duration = item.SongDuration
	}
	return providers.Candidate{
		ProviderID: "kugou", ExternalID: strings.TrimSpace(item.Hash), Title: strings.TrimSpace(item.SongName),
		Artists: artists, Album: strings.TrimSpace(item.AlbumName), AlbumArtists: artists,
		DurationSeconds: parseDuration(duration), TrackNumber: item.TrackNumber,
	}
}

func searchKeywords(query providers.Query) []string {
	values := make([]string, 0, 3)
	appendKeyword := func(parts ...string) {
		filtered := make([]string, 0, len(parts))
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				filtered = append(filtered, part)
			}
		}
		keyword := strings.TrimSpace(strings.Join(filtered, " "))
		if keyword == "" {
			return
		}
		for _, existing := range values {
			if existing == keyword {
				return
			}
		}
		values = append(values, keyword)
	}
	appendKeyword(query.Title, strings.Join(query.Artists, " "), query.Album)
	appendKeyword(query.Title, strings.Join(query.Artists, " "))
	appendKeyword(query.Title)
	if index := strings.IndexAny(query.Title, "([〔【"); index > 0 {
		appendKeyword(strings.TrimSpace(query.Title[:index]), strings.Join(query.Artists, " "))
		appendKeyword(strings.TrimSpace(query.Title[:index]))
	}
	return values
}

func decodeLyrics(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(value)
	}
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(value)
	}
	if err == nil {
		value = string(decoded)
	}
	value = providers.NormalizeLyrics(value)
	if !providers.HasLyrics(value) {
		return "", nil
	}
	return value, nil
}

func splitArtists(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '&' || r == ',' || r == '/' || r == '、' || r == '|' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func songScore(query providers.Query, item songItem) float64 {
	titleScore := similarity(normalize(query.Title), normalize(item.SongName))
	artistScore := similarity(normalize(strings.Join(query.Artists, " ")), normalize(item.SingerName))
	score := titleScore*0.7 + artistScore*0.3
	if query.DurationSeconds > 0 {
		candidateDuration := parseDuration(item.Duration)
		if candidateDuration == 0 {
			candidateDuration = parseDuration(item.TimeLength)
		}
		if candidateDuration > 0 {
			delta := abs(query.DurationSeconds - candidateDuration)
			if delta <= 15 {
				score += (1 - float64(delta)/15) * 0.15
			}
		}
	}
	return score
}

func similarity(left, right string) float64 {
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 1
	}
	maximum := maxInt(len([]rune(left)), len([]rune(right)))
	if maximum == 0 {
		return 0
	}
	return maxFloat(0, 1-float64(levenshtein([]rune(left), []rune(right)))/float64(maximum))
}

func normalize(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= '\u4e00' && r <= '\u9fff') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func levenshtein(left, right []rune) int {
	previous := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	for i, leftRune := range left {
		current := make([]int, len(right)+1)
		current[0] = i + 1
		for j, rightRune := range right {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[j+1] = minInt(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func parseDuration(value stringOrNumber) int64 {
	text := strings.TrimSpace(value.String())
	if text == "" {
		return 0
	}
	if strings.Contains(text, ":") {
		parts := strings.Split(text, ":")
		if len(parts) == 2 {
			minutes, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
			seconds, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			return minutes*60 + seconds
		}
	}
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0
	}
	if number > 10000 {
		return number / 1000
	}
	return number
}

func kugouHeaders(auth, cookie string) map[string]string {
	result := map[string]string{"Referer": "https://www.kugou.com/", "Origin": "https://www.kugou.com"}
	if strings.TrimSpace(auth) != "" {
		result["Authorization"] = strings.TrimSpace(auth)
	}
	if strings.TrimSpace(cookie) != "" {
		result["Cookie"] = strings.TrimSpace(cookie)
	}
	return result
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func maxInt(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func minInt(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

type stringOrNumber string

func (value *stringOrNumber) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*value = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*value = stringOrNumber(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("decode string or number: %w", err)
	}
	*value = stringOrNumber(number.String())
	return nil
}

func (value stringOrNumber) String() string { return string(value) }

type songItem struct {
	Hash         string         `json:"hash"`
	SongName     string         `json:"songname"`
	SingerName   string         `json:"singername"`
	AlbumID      stringOrNumber `json:"album_id"`
	AlbumName    string         `json:"album_name"`
	Duration     stringOrNumber `json:"duration"`
	TimeLength   stringOrNumber `json:"timelength"`
	SongDuration stringOrNumber `json:"song_duration"`
	TrackNumber  int            `json:"tracknum"`
}

type searchResponse struct {
	Status    stringOrNumber `json:"status"`
	ErrorCode stringOrNumber `json:"error_code"`
	Message   string         `json:"error"`
	Data      struct {
		Info []songItem `json:"info"`
	} `json:"data"`
}

type lyricCandidate struct {
	ID        stringOrNumber `json:"id"`
	AccessKey string         `json:"accesskey"`
}

type lyricSearchResponse struct {
	Status     stringOrNumber   `json:"status"`
	ErrorCode  stringOrNumber   `json:"error_code"`
	Message    string           `json:"error"`
	Candidates []lyricCandidate `json:"candidates"`
}

type lyricDownloadResponse struct {
	Status    stringOrNumber `json:"status"`
	ErrorCode stringOrNumber `json:"error_code"`
	Message   string         `json:"error"`
	Content   string         `json:"content"`
}

type artworkResponse struct {
	Status    stringOrNumber `json:"status"`
	ErrorCode stringOrNumber `json:"error_code"`
	Message   string         `json:"error"`
	Data      struct {
		Image string `json:"img"`
	} `json:"data"`
}

func (r searchResponse) businessError() error {
	return kugouBusinessError(r.Status, r.ErrorCode, r.Message)
}

func (r lyricSearchResponse) businessError() error {
	return kugouBusinessError(r.Status, r.ErrorCode, r.Message)
}

func (r lyricDownloadResponse) businessError() error {
	return kugouBusinessError(r.Status, r.ErrorCode, r.Message)
}

func (r artworkResponse) businessError() error {
	return kugouBusinessError(r.Status, r.ErrorCode, r.Message)
}

func kugouBusinessError(status, errorCode stringOrNumber, message string) error {
	code := strings.TrimSpace(errorCode.String())
	if code != "" && code != "0" {
		return &providers.BusinessError{Provider: "kugou", Code: "error_code=" + code, Message: message, Retryable: code == "429" || strings.HasPrefix(code, "5")}
	}
	value := strings.ToLower(strings.TrimSpace(status.String()))
	if value == "" || value == "0" || value == "1" || value == "200" || value == "ok" || value == "success" {
		return nil
	}
	if value == "error" || value == "fail" || value == "failed" || value == "unauthorized" || value == "forbidden" || value == "ratelimit" || value == "rate_limited" {
		return &providers.BusinessError{Provider: "kugou", Code: "status=" + value, Message: message}
	}
	parsed, err := strconv.Atoi(value)
	if err == nil && (parsed < 0 || parsed == 401 || parsed == 403 || parsed == 429 || parsed >= 500) {
		return &providers.BusinessError{Provider: "kugou", Code: "status=" + value, Message: message, Retryable: parsed == 429 || parsed >= 500}
	}
	return nil
}
