package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/ericwyn/tagger/internal/domain"
)

func toView(query Query, descriptor Descriptor, candidate Candidate) MatchCandidate {
	score, reasons := scoreCandidate(query, candidate)
	source := descriptor.Name
	albumArtists := candidate.AlbumArtists
	if len(albumArtists) == 0 {
		albumArtists = candidate.Artists
	}
	view := MatchCandidate{
		ID:                   "cand-" + descriptor.ID + "-" + shortHash(candidate.ExternalID),
		ProviderID:           descriptor.ID,
		ProviderName:         descriptor.Name,
		ExternalID:           candidate.ExternalID,
		Title:                Field[string]{Value: candidate.Title, Source: source},
		Artists:              Field[[]string]{Value: nonNil(candidate.Artists), Source: source},
		Album:                Field[string]{Value: candidate.Album, Source: source},
		AlbumArtists:         Field[[]string]{Value: nonNil(albumArtists), Source: source},
		Year:                 Field[int]{Value: candidate.Year, Source: source},
		TrackNumber:          Field[int]{Value: candidate.TrackNumber, Source: source},
		TrackTotal:           Field[int]{Value: candidate.TrackTotal, Source: source},
		DiscNumber:           Field[int]{Value: candidate.DiscNumber, Source: source},
		DiscTotal:            Field[int]{Value: candidate.DiscTotal, Source: source},
		DurationSeconds:      Field[int64]{Value: candidate.DurationSeconds, Source: source},
		Genres:               Field[[]string]{Value: nonNil(candidate.Genres), Source: source},
		Comment:              Field[string]{Value: candidate.Comment, Source: source},
		Composers:            Field[[]string]{Value: nonNil(candidate.Composers), Source: source},
		Conductor:            Field[string]{Value: candidate.Conductor, Source: source},
		Lyricists:            Field[[]string]{Value: nonNil(candidate.Lyricists), Source: source},
		Copyright:            Field[string]{Value: candidate.Copyright, Source: source},
		BPM:                  Field[int]{Value: candidate.BPM, Source: source},
		ISRC:                 Field[string]{Value: candidate.ISRC, Source: source},
		MusicBrainzTrackID:   Field[string]{Value: candidate.MusicBrainzTrackID, Source: source},
		MusicBrainzReleaseID: Field[string]{Value: candidate.MusicBrainzReleaseID, Source: source},
		MusicBrainzArtistIDs: Field[[]string]{Value: nonNil(candidate.MusicBrainzArtistIDs), Source: source},
		AcoustID:             Field[string]{Value: candidate.AcoustID, Source: source},
		AcoustIDFingerprint:  Field[string]{Value: candidate.AcoustIDFingerprint, Source: source},
		HasLyrics:            candidate.Lyrics != "" || candidate.SyncedLyrics != "",
		HasArtwork:           candidate.ArtworkURL != "",
		CoverTone:            toneFor(descriptor.ID + candidate.ExternalID),
		Score:                math.Round(score*100) / 100,
		ScoreLabel:           scoreLabel(score),
		MatchReasons:         reasons,
	}
	lyrics := candidate.SyncedLyrics
	if lyrics == "" {
		lyrics = candidate.Lyrics
	}
	if lyrics != "" {
		view.Lyrics = &Field[string]{Value: lyrics, Source: source}
	}
	return view
}

func scoreCandidate(query Query, candidate Candidate) (float64, []string) {
	titleScore := similarity(query.Title, candidate.Title)
	for _, alternate := range candidate.AlternateTitles {
		titleScore = math.Max(titleScore, similarity(query.Title, alternate))
	}
	artistScore := similarity(strings.Join(query.Artists, " "), strings.Join(candidate.Artists, " "))
	albumScore := similarity(query.Album, candidate.Album)
	durationScore := 0.0
	hasDuration := query.DurationSeconds > 0 && candidate.DurationSeconds > 0
	if hasDuration {
		delta := math.Abs(float64(query.DurationSeconds - candidate.DurationSeconds))
		durationScore = math.Max(0, 1-delta/15)
	}
	score := titleScore*0.5 + artistScore*0.3
	weight := 0.8
	if query.Album != "" && candidate.Album != "" {
		score += albumScore * 0.12
		weight += 0.12
	}
	if hasDuration {
		score += durationScore * 0.08
		weight += 0.08
	}
	score /= weight

	reasons := make([]string, 0, 4)
	if titleScore >= 0.98 {
		reasons = append(reasons, "标题一致")
	} else if titleScore >= 0.75 {
		reasons = append(reasons, "标题相近")
	}
	if artistScore >= 0.9 {
		reasons = append(reasons, "艺术家一致")
	}
	if albumScore >= 0.9 && query.Album != "" {
		reasons = append(reasons, "专辑一致")
	}
	if durationScore >= 0.85 {
		reasons = append(reasons, "时长接近")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "关键词匹配")
	}
	return clamp(score), reasons
}

func similarity(left, right string) float64 {
	left = normalize(left)
	right = normalize(right)
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 1
	}
	leftRunes, rightRunes := []rune(left), []rune(right)
	distance := levenshtein(leftRunes, rightRunes)
	maximum := max(len(leftRunes), len(rightRunes))
	return clamp(1 - float64(distance)/float64(maximum))
}

func normalize(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
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
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}

func scoreLabel(score float64) string {
	switch {
	case score >= 0.9:
		return "高度匹配"
	case score >= 0.75:
		return "较高匹配"
	case score >= 0.55:
		return "可能匹配"
	default:
		return "低匹配"
	}
}

func toneFor(seed string) domain.CoverTone {
	tones := []domain.CoverTone{domain.CoverVermilion, domain.CoverMoss, domain.CoverCobalt, domain.CoverSand, domain.CoverCharcoal, domain.CoverJade}
	hash := sha256.Sum256([]byte(seed))
	return tones[int(hash[0])%len(tones)]
}

func shortHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:8])
}

func nonNil(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func clamp(value float64) float64 { return math.Max(0, math.Min(1, value)) }

func sortViews(candidates []MatchCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].ProviderName < candidates[j].ProviderName
		}
		return candidates[i].Score > candidates[j].Score
	})
}
