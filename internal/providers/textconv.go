package providers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ericwyn/tagger/internal/textconv"
)

// SimplifyChineseConfigField describes the optional provider-level text
// transform shared by built-in sources. It is disabled by default so existing
// provider output remains unchanged until a user opts in.
func SimplifyChineseConfigField(enabled bool) ConfigField {
	return ConfigField{
		Key: "simplifyChinese", Label: "自动转为简体", Type: "boolean",
		Value:       strconv.FormatBool(enabled),
		Description: "将该数据源返回的标题、歌手、专辑、流派和歌词等文本转换为简体中文",
	}
}

func ParseSimplifyChinese(value string) (bool, error) {
	simplify, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("simplifyChinese 必须是 true 或 false")
	}
	return simplify, nil
}

func SimplifyChineseCacheVariant(enabled bool) string {
	return "simplifyChinese=" + strconv.FormatBool(enabled)
}

// SimplifyCandidate transforms only human-readable candidate text. IDs,
// artwork URLs, durations and other machine-readable values stay untouched.
// Slice fields are copied so provider-owned response buffers are never mutated.
func SimplifyCandidate(candidate Candidate) Candidate {
	candidate.Title = textconv.Simplify(candidate.Title)
	candidate.AlternateTitles = textconv.SimplifyAll(candidate.AlternateTitles)
	candidate.Artists = textconv.SimplifyAll(candidate.Artists)
	candidate.Album = textconv.Simplify(candidate.Album)
	candidate.AlbumArtists = textconv.SimplifyAll(candidate.AlbumArtists)
	candidate.Genres = textconv.SimplifyAll(candidate.Genres)
	candidate.Lyrics = textconv.Simplify(candidate.Lyrics)
	candidate.SyncedLyrics = textconv.Simplify(candidate.SyncedLyrics)
	candidate.Comment = textconv.Simplify(candidate.Comment)
	candidate.Composers = textconv.SimplifyAll(candidate.Composers)
	candidate.Conductor = textconv.Simplify(candidate.Conductor)
	candidate.Lyricists = textconv.SimplifyAll(candidate.Lyricists)
	candidate.Copyright = textconv.Simplify(candidate.Copyright)
	return candidate
}
