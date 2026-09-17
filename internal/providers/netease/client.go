package netease

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ericwyn/tagger/internal/providers"
)

type Config struct {
	Client        *http.Client
	Endpoint      string
	LyricEndpoint string
	AlbumEndpoint string
	UserAgent     string
	Auth          string
	Cookie        string
	RateInterval  time.Duration
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
		config.Endpoint = "https://music.163.com/api/cloudsearch/pc"
	}
	if config.LyricEndpoint == "" {
		config.LyricEndpoint = "https://music.163.com/api/song/lyric"
	}
	if config.AlbumEndpoint == "" {
		config.AlbumEndpoint = "https://music.163.com/api/album"
	}
	if config.UserAgent == "" {
		config.UserAgent = providers.DefaultUserAgent("netease")
	}
	if config.RateInterval == 0 {
		config.RateInterval = 180 * time.Millisecond
	}
	gate := providers.NewGate(config.RateInterval)
	return &Client{config: config, baseHTTP: config.Client, http: providers.WrapHTTPClient(config.Client, gate), gate: gate}
}

// ResetConfig restores the public web endpoint defaults and clears optional
// credentials. The injected HTTP client remains unchanged.
func (c *Client) ResetConfig() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.Endpoint = "https://music.163.com/api/cloudsearch/pc"
	c.config.LyricEndpoint = "https://music.163.com/api/song/lyric"
	c.config.AlbumEndpoint = "https://music.163.com/api/album"
	c.config.UserAgent = providers.DefaultUserAgent("netease")
	c.config.Auth = ""
	c.config.Cookie = ""
	c.gate.SetInterval(180 * time.Millisecond)
	return c.setProxyLocked("")
}

func (c *Client) Descriptor() providers.Descriptor {
	return providers.Descriptor{
		ID: "netease", Name: "网易云音乐", ShortName: "NE",
		Description:  "中文曲库、同步歌词与专辑封面",
		Capabilities: []string{"歌曲", "专辑", "音轨", "歌词", "同步歌词", "封面"},
		Health:       providers.HealthDegraded, Enabled: false, Experimental: true,
		Accent: "#d62d20", QuotaLabel: "实验性网页接口 · 设置中启用",
	}
}

func (c *Client) ConfigFields() []providers.ConfigField {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return []providers.ConfigField{
		{Key: "endpoint", Label: "搜索 API URL", Type: "url", Value: c.config.Endpoint, Required: true},
		{Key: "lyricEndpoint", Label: "歌词 API URL", Type: "url", Value: c.config.LyricEndpoint, Required: true},
		{Key: "albumEndpoint", Label: "专辑 API URL", Type: "url", Value: c.config.AlbumEndpoint, Required: true},
		{Key: "userAgent", Label: "User-Agent", Type: "text", Value: c.config.UserAgent, Required: true},
		{Key: "auth", Label: "鉴权头（可选）", Type: "password", Value: c.config.Auth, Secret: true, Placeholder: "Bearer …"},
		{Key: "cookie", Label: "Cookie（可选）", Type: "password", Value: c.config.Cookie, Secret: true, Placeholder: "MUSIC_U=…"},
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
		case "lyricEndpoint":
			endpoint, err := providers.ValidateHTTPURL(value, "lyricEndpoint")
			if err != nil {
				return err
			}
			c.config.LyricEndpoint = endpoint
		case "albumEndpoint":
			endpoint, err := providers.ValidateHTTPURL(value, "albumEndpoint")
			if err != nil {
				return err
			}
			c.config.AlbumEndpoint = endpoint
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
	// A full metadata keyword is useful for disambiguation, while a title-only
	// retry catches files whose artist/album tags contain a translation or a
	// collaboration spelling that NetEase does not index the same way.
	keywords := searchKeywords(query)
	fetchLimit := limit * 4
	if fetchLimit < 20 {
		fetchLimit = 20
	}
	if fetchLimit > 100 {
		fetchLimit = 100
	}
	songs := make([]song, 0, fetchLimit)
	seen := make(map[int64]struct{}, fetchLimit)
	for index, keyword := range keywords {
		values := url.Values{
			"s":      {keyword},
			"type":   {"1"},
			"limit":  {strconv.Itoa(fetchLimit)},
			"offset": {"0"},
			"total":  {"true"},
		}
		var response searchResponse
		if err := providers.GetJSONWithHeaders(ctx, c.http, c.config.Endpoint+"?"+values.Encode(), c.config.UserAgent, neteaseHeaders(c.config.Auth, c.config.Cookie), &response); err != nil {
			if index == 0 {
				return nil, err
			}
			continue
		}
		if err := response.businessError(); err != nil {
			if index == 0 {
				return nil, err
			}
			continue
		}
		for _, item := range response.Result.Songs {
			if item.ID == 0 {
				continue
			}
			if _, exists := seen[item.ID]; exists {
				continue
			}
			seen[item.ID] = struct{}{}
			songs = append(songs, item)
		}
		if len(songs) >= fetchLimit {
			break
		}
	}

	// NetEase returns a broad result set. Rank locally before fetching lyrics so
	// the extra lyric requests are spent on the likely recordings first.
	sort.SliceStable(songs, func(left, right int) bool {
		return songScore(query, songs[left]) > songScore(query, songs[right])
	})
	lyricsLimit := limit * 2
	if lyricsLimit < 5 {
		lyricsLimit = 5
	}
	if lyricsLimit > len(songs) {
		lyricsLimit = len(songs)
	}
	result := make([]providers.Candidate, 0, min(limit, len(songs)))
	for index, item := range songs {
		if index >= lyricsLimit {
			break
		}
		candidate := mapCandidate(item)
		if candidate.ArtworkURL == "" && item.Album.ID > 0 {
			if artwork, artworkErr := c.fetchArtwork(ctx, item.Album.ID); artworkErr == nil {
				candidate.ArtworkURL = artwork
			}
		}
		lyrics, err := c.fetchLyrics(ctx, item.ID)
		if err == nil {
			candidate.SyncedLyrics = lyrics
			candidate.Lyrics = lyrics
		}
		result = append(result, candidate)
	}
	// Keep lyric-bearing candidates ahead of metadata-only records. The
	// registry still applies its normal title/artist score across providers.
	sort.SliceStable(result, func(left, right int) bool {
		leftLyrics := providers.HasLyrics(result[left].SyncedLyrics) || providers.HasLyrics(result[left].Lyrics)
		rightLyrics := providers.HasLyrics(result[right].SyncedLyrics) || providers.HasLyrics(result[right].Lyrics)
		if leftLyrics != rightLyrics {
			return leftLyrics
		}
		return false // songs were already score-sorted before lyric enrichment
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (c *Client) fetchLyrics(ctx context.Context, id int64) (string, error) {
	values := url.Values{"id": {strconv.FormatInt(id, 10)}, "lv": {"-1"}, "tv": {"-1"}}
	var response lyricResponse
	if err := providers.GetJSONWithHeaders(ctx, c.http, c.config.LyricEndpoint+"?"+values.Encode(), c.config.UserAgent, neteaseHeaders(c.config.Auth, c.config.Cookie), &response); err != nil {
		return "", err
	}
	if err := response.businessError(); err != nil {
		return "", err
	}
	// NetEase returns its translated LRC separately. Merge it with the original
	// by timestamp so players show the source line followed by its translation,
	// rather than discarding the translation or appending a second timeline.
	if original := providers.NormalizeLyrics(response.LRC.Lyric); providers.HasLyrics(original) {
		if translated := providers.NormalizeLyrics(response.TranslatedLRC.Lyric); providers.HasLyrics(translated) {
			return mergeTimedLyrics(original, translated), nil
		}
		return original, nil
	}
	for _, value := range []string{response.TranslatedLRC.Lyric, response.RomaLRC.Lyric, response.YRC.Lyric, response.KLyric.Lyric} {
		if normalized := providers.NormalizeLyrics(value); providers.HasLyrics(normalized) {
			return normalized, nil
		}
	}
	return "", nil
}

var lrcTimestampPattern = regexp.MustCompile(`^\[(\d+):([0-5]?\d(?:\.\d+)?)\]`)

type timedLyricLine struct {
	text         string
	milliseconds int64
	source       int
	index        int
}

// mergeTimedLyrics produces the conventional bilingual LRC layout: original
// first and translation second at an equal timestamp. Stable time sorting also
// handles providers that return either stream slightly out of order.
func mergeTimedLyrics(original, translated string) string {
	original = providers.NormalizeLyrics(original)
	translated = providers.NormalizeLyrics(translated)
	if original == "" {
		return translated
	}
	if translated == "" {
		return original
	}

	headers := make([]string, 0, 8)
	timed := make([]timedLyricLine, 0, strings.Count(original, "\n")+strings.Count(translated, "\n")+2)
	seenHeaders := make(map[string]struct{})
	for source, value := range []string{original, translated} {
		for index, line := range strings.Split(value, "\n") {
			if milliseconds, ok := lyricTimestamp(line); ok {
				timed = append(timed, timedLyricLine{text: line, milliseconds: milliseconds, source: source, index: index})
				continue
			}
			if _, exists := seenHeaders[line]; !exists {
				seenHeaders[line] = struct{}{}
				headers = append(headers, line)
			}
		}
	}
	if len(timed) == 0 {
		return providers.NormalizeLyrics(original + "\n" + translated)
	}
	sort.SliceStable(timed, func(left, right int) bool {
		if timed[left].milliseconds != timed[right].milliseconds {
			return timed[left].milliseconds < timed[right].milliseconds
		}
		if timed[left].source != timed[right].source {
			return timed[left].source < timed[right].source
		}
		return timed[left].index < timed[right].index
	})
	lines := make([]string, 0, len(headers)+len(timed))
	lines = append(lines, headers...)
	for _, line := range timed {
		lines = append(lines, line.text)
	}
	return providers.NormalizeLyrics(strings.Join(lines, "\n"))
}

func lyricTimestamp(line string) (int64, bool) {
	matches := lrcTimestampPattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 3 {
		return 0, false
	}
	minutes, minuteErr := strconv.ParseInt(matches[1], 10, 64)
	seconds, secondErr := strconv.ParseFloat(matches[2], 64)
	if minuteErr != nil || secondErr != nil {
		return 0, false
	}
	return minutes*60_000 + int64(seconds*1000+0.5), true
}

func (c *Client) fetchArtwork(ctx context.Context, albumID int64) (string, error) {
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Album   struct {
			PictureURL string `json:"picUrl"`
		} `json:"album"`
	}
	endpoint := strings.TrimRight(c.config.AlbumEndpoint, "/") + "/" + strconv.FormatInt(albumID, 10) + "?ext=true"
	if err := providers.GetJSONWithHeaders(ctx, c.http, endpoint, c.config.UserAgent, neteaseHeaders(c.config.Auth, c.config.Cookie), &response); err != nil {
		return "", err
	}
	if response.Code != 0 && response.Code != 200 {
		return "", &providers.BusinessError{Provider: "netease", Code: strconv.Itoa(response.Code), Message: response.Message, Retryable: response.Code == 429}
	}
	artwork := strings.TrimPrefix(strings.TrimSpace(response.Album.PictureURL), "http://")
	if artwork != "" && !strings.HasPrefix(artwork, "https://") {
		artwork = "https://" + artwork
	}
	if artwork != "" && !strings.Contains(artwork, "param=") {
		artwork += "?param=500y"
	}
	return artwork, nil
}

func mapCandidate(item song) providers.Candidate {
	artists := make([]string, 0, len(item.Artists))
	for _, artist := range item.Artists {
		if name := strings.TrimSpace(artist.Name); name != "" {
			artists = append(artists, name)
		}
	}
	aliases := make([]string, 0, len(item.Aliases))
	for _, alias := range item.Aliases {
		if alias = strings.TrimSpace(alias); alias != "" && alias != item.Name {
			aliases = append(aliases, alias)
		}
	}
	artwork := strings.TrimSpace(item.Album.PictureURL)
	artwork = strings.TrimPrefix(artwork, "http://")
	if artwork != "" && !strings.HasPrefix(artwork, "https://") {
		artwork = "https://" + artwork
	}
	if artwork != "" && !strings.Contains(artwork, "param=") {
		artwork += "?param=500y"
	}
	return providers.Candidate{
		ProviderID: "netease", ExternalID: strconv.FormatInt(item.ID, 10), Title: item.Name,
		AlternateTitles: aliases, Artists: artists, Album: item.Album.Name, AlbumArtists: artists,
		TrackNumber: item.TrackNumber, DurationSeconds: item.Duration / 1000, ArtworkURL: artwork,
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

func songScore(query providers.Query, item song) float64 {
	title := normalize(query.Title)
	if title == "" {
		return 0
	}
	score := similarity(title, normalize(item.Name))
	for _, alias := range item.Aliases {
		score = max(score, similarity(title, normalize(alias)))
	}
	artistQuery := normalize(strings.Join(query.Artists, " "))
	artistValue := make([]string, 0, len(item.Artists))
	for _, artist := range item.Artists {
		artistValue = append(artistValue, artist.Name)
	}
	if artistQuery != "" {
		score += similarity(artistQuery, normalize(strings.Join(artistValue, " "))) * 0.25
	}
	if query.DurationSeconds > 0 && item.Duration > 0 {
		delta := abs(query.DurationSeconds - item.Duration/1000)
		if delta <= 15 {
			score += (1 - float64(delta)/15) * 0.15
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
	return max(0, 1-float64(levenshtein([]rune(left), []rune(right)))/float64(maximum))
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

func neteaseHeaders(auth, cookie string) map[string]string {
	result := map[string]string{"Origin": "https://music.163.com", "Referer": "https://music.163.com/"}
	if strings.TrimSpace(auth) != "" {
		result["Authorization"] = strings.TrimSpace(auth)
	}
	if strings.TrimSpace(cookie) != "" {
		result["Cookie"] = strings.TrimSpace(cookie)
	}
	return result
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
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

func maxInt(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func max(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

type song struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Duration int64    `json:"dt"`
	Aliases  []string `json:"alia"`
	Artists  []struct {
		Name string `json:"name"`
	} `json:"ar"`
	Album struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		PictureURL string `json:"picUrl"`
	} `json:"al"`
	TrackNumber int `json:"no"`
}

type searchResponse struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Result  struct {
		Songs []song `json:"songs"`
	} `json:"result"`
}

type lyricField struct {
	Lyric string `json:"lyric"`
}

type lyricResponse struct {
	Code          int        `json:"code"`
	Message       string     `json:"msg"`
	LRC           lyricField `json:"lrc"`
	TranslatedLRC lyricField `json:"tlyric"`
	RomaLRC       lyricField `json:"romalrc"`
	YRC           lyricField `json:"yrc"`
	KLyric        lyricField `json:"klyric"`
}

func (r searchResponse) businessError() error {
	if r.Code == 0 || r.Code == 200 {
		return nil
	}
	return &providers.BusinessError{Provider: "netease", Code: strconv.Itoa(r.Code), Message: r.Message, Retryable: r.Code == 429}
}

func (r lyricResponse) businessError() error {
	if r.Code == 0 || r.Code == 200 {
		return nil
	}
	return &providers.BusinessError{Provider: "netease", Code: strconv.Itoa(r.Code), Message: r.Message, Retryable: r.Code == 429}
}
