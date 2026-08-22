package netease

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ericwyn/tagger/internal/providers"
)

type Config struct {
	Client        *http.Client
	Endpoint      string
	LyricEndpoint string
	AlbumEndpoint string
	UserAgent     string
	RateInterval  time.Duration
}

type Client struct {
	config Config
	http   *http.Client
	gate   *providers.Gate
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
		config.UserAgent = "Tagger/0.1 (experimental netease adapter)"
	}
	if config.RateInterval == 0 {
		config.RateInterval = 180 * time.Millisecond
	}
	return &Client{config: config, http: config.Client, gate: providers.NewGate(config.RateInterval)}
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

func (c *Client) Search(ctx context.Context, query providers.Query, limit int) ([]providers.Candidate, error) {
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
		if index > 0 {
			if err := c.gate.Wait(ctx); err != nil {
				return nil, err
			}
		}
		values := url.Values{
			"s":      {keyword},
			"type":   {"1"},
			"limit":  {strconv.Itoa(fetchLimit)},
			"offset": {"0"},
			"total":  {"true"},
		}
		var response searchResponse
		if err := providers.GetJSONWithHeaders(ctx, c.http, c.config.Endpoint+"?"+values.Encode(), c.config.UserAgent, neteaseHeaders(), &response); err != nil {
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
	if err := c.gate.Wait(ctx); err != nil {
		return "", err
	}
	values := url.Values{"id": {strconv.FormatInt(id, 10)}, "lv": {"-1"}, "tv": {"-1"}}
	var response lyricResponse
	if err := providers.GetJSONWithHeaders(ctx, c.http, c.config.LyricEndpoint+"?"+values.Encode(), c.config.UserAgent, neteaseHeaders(), &response); err != nil {
		return "", err
	}
	// Prefer original lyrics. YRC/KLyric are still useful when an ordinary LRC
	// is absent; translated lyrics are deliberately not used as a fallback.
	for _, value := range []string{response.LRC.Lyric, response.RomaLRC.Lyric, response.YRC.Lyric, response.KLyric.Lyric} {
		if normalized := providers.NormalizeLyrics(value); providers.HasLyrics(normalized) {
			return normalized, nil
		}
	}
	return "", nil
}

func (c *Client) fetchArtwork(ctx context.Context, albumID int64) (string, error) {
	if err := c.gate.Wait(ctx); err != nil {
		return "", err
	}
	var response struct {
		Album struct {
			PictureURL string `json:"picUrl"`
		} `json:"album"`
	}
	endpoint := strings.TrimRight(c.config.AlbumEndpoint, "/") + "/" + strconv.FormatInt(albumID, 10) + "?ext=true"
	if err := providers.GetJSONWithHeaders(ctx, c.http, endpoint, c.config.UserAgent, neteaseHeaders(), &response); err != nil {
		return "", err
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

func neteaseHeaders() map[string]string {
	return map[string]string{"Origin": "https://music.163.com", "Referer": "https://music.163.com/"}
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
	Result struct {
		Songs []song `json:"songs"`
	} `json:"result"`
}

type lyricField struct {
	Lyric string `json:"lyric"`
}

type lyricResponse struct {
	LRC     lyricField `json:"lrc"`
	RomaLRC lyricField `json:"romalrc"`
	YRC     lyricField `json:"yrc"`
	KLyric  lyricField `json:"klyric"`
}
