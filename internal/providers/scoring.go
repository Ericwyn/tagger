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
	if strings.TrimSpace(candidate.ArtworkURL) == "" {
		reasons = append(reasons, "来源未提供封面")
	}
	source := descriptor.Name
	id := "cand-" + descriptor.ID + "-" + shortHash(candidate.ExternalID)
	ref := SourceReference{ProviderID: descriptor.ID, ProviderName: descriptor.Name, CandidateID: id, ExternalID: candidate.ExternalID}
	releaseScore := 0.0
	if query.Album != "" && candidate.Album != "" {
		releaseScore = similarity(query.Album, candidate.Album)
	}
	assetQuality := 0.0
	if candidate.ArtworkURL != "" {
		assetQuality += 0.5
	}
	if candidate.Lyrics != "" || candidate.SyncedLyrics != "" {
		assetQuality += 0.5
	}
	level := evidenceLevel(score)
	view := MatchCandidate{
		ID:                   id,
		Kind:                 CandidateKindSource,
		ProviderID:           descriptor.ID,
		ProviderName:         descriptor.Name,
		ExternalID:           candidate.ExternalID,
		MemberCandidateIDs:   []string{id},
		Contributors:         []SourceReference{ref},
		Title:                sourceField(candidate.Title, source, score),
		Artists:              sourceField(nonNil(candidate.Artists), source, score),
		Album:                sourceField(candidate.Album, source, score),
		AlbumArtists:         sourceField(nonNil(candidate.AlbumArtists), source, score),
		Year:                 sourceField(candidate.Year, source, score),
		TrackNumber:          sourceField(candidate.TrackNumber, source, score),
		TrackTotal:           sourceField(candidate.TrackTotal, source, score),
		DiscNumber:           sourceField(candidate.DiscNumber, source, score),
		DiscTotal:            sourceField(candidate.DiscTotal, source, score),
		DurationSeconds:      sourceField(candidate.DurationSeconds, source, score),
		Genres:               sourceField(nonNil(candidate.Genres), source, score),
		Comment:              sourceField(candidate.Comment, source, score),
		Composers:            sourceField(nonNil(candidate.Composers), source, score),
		Conductor:            sourceField(candidate.Conductor, source, score),
		Lyricists:            sourceField(nonNil(candidate.Lyricists), source, score),
		Copyright:            sourceField(candidate.Copyright, source, score),
		BPM:                  sourceField(candidate.BPM, source, score),
		ISRC:                 sourceField(candidate.ISRC, source, score),
		MusicBrainzTrackID:   sourceField(candidate.MusicBrainzTrackID, source, score),
		MusicBrainzReleaseID: sourceField(candidate.MusicBrainzReleaseID, source, score),
		MusicBrainzArtistIDs: sourceField(nonNil(candidate.MusicBrainzArtistIDs), source, score),
		AcoustID:             sourceField(candidate.AcoustID, source, score),
		AcoustIDFingerprint:  sourceField(candidate.AcoustIDFingerprint, source, score),
		HasLyrics:            candidate.Lyrics != "" || candidate.SyncedLyrics != "",
		HasArtwork:           candidate.ArtworkURL != "",
		CoverTone:            toneFor(descriptor.ID + candidate.ExternalID),
		Score:                math.Round(score*100) / 100,
		ScoreLabel:           scoreLabel(score),
		MatchReasons:         reasons,
		Evidence: MatchEvidence{
			IdentityScore: score, ReleaseScore: releaseScore,
			CompletenessScore: candidateCompleteness(candidate), AssetQuality: assetQuality,
			Level: level, SourceCount: 1, AlgorithmVersion: "source-v2",
		},
	}
	if candidate.ArtworkURL != "" {
		view.ArtworkReferenceID = id
		view.ArtworkSource = &ref
	}
	lyrics := candidate.SyncedLyrics
	if lyrics == "" {
		lyrics = candidate.Lyrics
	}
	if lyrics != "" {
		field := sourceField(lyrics, source, score)
		view.Lyrics = &field
	}
	return view
}

func scoreCandidate(query Query, candidate Candidate) (float64, []string) {
	queryBase, queryQualifiers := titleProfile(query.Title)
	candidateBase, candidateQualifiers := titleProfile(candidate.Title)
	titleScore := similarity(queryBase, candidateBase)
	for _, alternate := range candidate.AlternateTitles {
		alternateBase, _ := titleProfile(alternate)
		titleScore = math.Max(titleScore, similarity(queryBase, alternateBase))
	}
	artistScore := artistSimilarity(query.Artists, candidate.Artists)
	albumScore := similarity(query.Album, candidate.Album)
	versionScore := qualifierSimilarity(queryQualifiers, candidateQualifiers)
	durationScore := 0.0
	hasDuration := query.DurationSeconds > 0 && candidate.DurationSeconds > 0
	if hasDuration {
		delta := math.Abs(float64(query.DurationSeconds - candidate.DurationSeconds))
		durationScore = math.Max(0, 1-delta/20)
	}
	score := titleScore * 0.55
	weight := 0.55
	if len(query.Artists) > 0 && len(candidate.Artists) > 0 {
		score += artistScore * 0.3
		weight += 0.3
	}
	if hasDuration {
		score += durationScore * 0.1
		weight += 0.1
	}
	if len(queryQualifiers) > 0 || len(candidateQualifiers) > 0 {
		score += versionScore * 0.05
		weight += 0.05
	}
	score /= weight
	identifierMatch, identifierConflict := compareQueryIdentifiers(query, candidate)
	if identifierMatch {
		score = math.Max(score, 0.99)
	}
	if identifierConflict {
		score = math.Min(score, 0.25)
	}

	reasons := make([]string, 0, 7)
	if identifierMatch {
		reasons = append(reasons, "稳定标识一致")
	}
	if identifierConflict {
		reasons = append(reasons, "稳定标识冲突")
	}
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
	if len(queryQualifiers) > 0 || len(candidateQualifiers) > 0 {
		if versionScore >= 0.99 {
			reasons = append(reasons, "版本标记一致")
		} else if versionScore == 0 {
			reasons = append(reasons, "版本标记冲突")
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "关键词匹配")
	}
	return clamp(score), reasons
}

func sourceField[T any](value T, source string, confidence float64) Field[T] {
	return Field[T]{Value: value, Source: source, Confidence: confidence}
}

func compareQueryIdentifiers(query Query, candidate Candidate) (bool, bool) {
	pairs := [][2]string{
		{query.ISRC, candidate.ISRC},
		{query.MusicBrainzTrackID, candidate.MusicBrainzTrackID},
		{query.AcoustID, candidate.AcoustID},
		{query.AcoustIDFingerprint, candidate.AcoustIDFingerprint},
	}
	matched := false
	for _, pair := range pairs {
		left, right := normalizeIdentifier(pair[0]), normalizeIdentifier(pair[1])
		if left == "" || right == "" {
			continue
		}
		if left != right {
			return matched, true
		}
		matched = true
	}
	return matched, false
}

func normalizeIdentifier(value string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "", "_", "").Replace(strings.TrimSpace(value)))
}

func candidateCompleteness(candidate Candidate) float64 {
	present, total := 0, 16
	if candidate.Title != "" {
		present++
	}
	if len(candidate.Artists) > 0 {
		present++
	}
	if candidate.Album != "" {
		present++
	}
	if len(candidate.AlbumArtists) > 0 {
		present++
	}
	if candidate.Year > 0 {
		present++
	}
	if candidate.TrackNumber > 0 {
		present++
	}
	if candidate.TrackTotal > 0 {
		present++
	}
	if candidate.DiscNumber > 0 {
		present++
	}
	if candidate.DurationSeconds > 0 {
		present++
	}
	if len(candidate.Genres) > 0 {
		present++
	}
	if candidate.Lyrics != "" || candidate.SyncedLyrics != "" {
		present++
	}
	if candidate.ArtworkURL != "" {
		present++
	}
	if candidate.ISRC != "" {
		present++
	}
	if candidate.MusicBrainzTrackID != "" {
		present++
	}
	if candidate.MusicBrainzReleaseID != "" {
		present++
	}
	if candidate.AcoustID != "" || candidate.AcoustIDFingerprint != "" {
		present++
	}
	return float64(present) / float64(total)
}

var qualifierTokens = map[string]string{
	"live": "live", "concert": "live", "现场": "live", "現場": "live", "ライブ": "live",
	"remix": "remix", "mix": "remix", "混音": "remix",
	"acoustic": "acoustic", "unplugged": "acoustic", "原声": "acoustic", "原聲": "acoustic",
	"instrumental": "instrumental", "伴奏": "instrumental", "纯音乐": "instrumental", "純音樂": "instrumental",
	"karaoke": "karaoke", "卡拉ok": "karaoke",
	"remaster": "remaster", "remastered": "remaster", "重制": "remaster", "重製": "remaster",
	"demo": "demo", "试听": "demo", "試聽": "demo",
	"cover": "cover", "翻唱": "cover",
	"sped": "speed", "slowed": "speed",
}

var inlineQualifierTokens = map[string]string{
	"现场": "live", "現場": "live", "ライブ": "live",
	"混音": "remix", "原声": "acoustic", "原聲": "acoustic",
	"伴奏": "instrumental", "纯音乐": "instrumental", "純音樂": "instrumental",
	"卡拉ok": "karaoke", "重制": "remaster", "重製": "remaster",
	"试听": "demo", "試聽": "demo", "翻唱": "cover",
}

func titleProfile(value string) (string, map[string]struct{}) {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	qualifiers := make(map[string]struct{})
	for token, canonical := range inlineQualifierTokens {
		if strings.Contains(cleaned, token) {
			qualifiers[canonical] = struct{}{}
			cleaned = strings.ReplaceAll(cleaned, token, " ")
		}
	}
	words := strings.FieldsFunc(cleaned, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	baseWords := make([]string, 0, len(words))
	for _, word := range words {
		canonical, found := qualifierTokens[word]
		if !found && strings.HasPrefix(word, "remaster") {
			canonical, found = "remaster", true
		}
		if found {
			qualifiers[canonical] = struct{}{}
			continue
		}
		if word == "version" || word == "版" || word == "ver" || word == "edit" || word == "radio" || word == "up" {
			continue
		}
		baseWords = append(baseWords, word)
	}
	base := normalize(strings.Join(baseWords, " "))
	if base == "" {
		base = normalize(value)
	}
	return base, qualifiers
}

func qualifierSimilarity(left, right map[string]struct{}) float64 {
	if len(left) == 0 && len(right) == 0 {
		return 1
	}
	if len(left) == 0 || len(right) == 0 {
		return 0.55
	}
	intersection := 0
	for key := range left {
		if _, found := right[key]; found {
			intersection++
		}
	}
	if intersection == len(left) && intersection == len(right) {
		return 1
	}
	if intersection == 0 {
		return 0
	}
	return float64(2*intersection) / float64(len(left)+len(right))
}

func qualifierConflict(left, right map[string]struct{}) bool {
	return len(left) > 0 && len(right) > 0 && qualifierSimilarity(left, right) == 0
}

func artistSimilarity(left, right []string) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	joined := similarity(strings.Join(left, " "), strings.Join(right, " "))
	leftSet, rightSet := normalizedStringSet(left), normalizedStringSet(right)
	intersection := 0
	for value := range leftSet {
		if _, found := rightSet[value]; found {
			intersection++
		}
	}
	setScore := 0.0
	if len(leftSet)+len(rightSet) > 0 {
		setScore = float64(2*intersection) / float64(len(leftSet)+len(rightSet))
	}
	return math.Max(joined, setScore)
}

func normalizedStringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if normalized := normalize(value); normalized != "" {
			result[normalized] = struct{}{}
		}
	}
	return result
}

func evidenceLevel(score float64) string {
	switch {
	case score >= 0.98:
		return "exact"
	case score >= 0.9:
		return "high"
	case score >= 0.75:
		return "review"
	default:
		return "low"
	}
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
		if candidates[i].Recommended != candidates[j].Recommended {
			return candidates[i].Recommended
		}
		if candidates[i].Score == candidates[j].Score {
			if candidates[i].Evidence.AssetQuality != candidates[j].Evidence.AssetQuality {
				return candidates[i].Evidence.AssetQuality > candidates[j].Evidence.AssetQuality
			}
			if candidates[i].Evidence.CompletenessScore != candidates[j].Evidence.CompletenessScore {
				return candidates[i].Evidence.CompletenessScore > candidates[j].Evidence.CompletenessScore
			}
			return candidates[i].ProviderName < candidates[j].ProviderName
		}
		return candidates[i].Score > candidates[j].Score
	})
}
