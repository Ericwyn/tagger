package providers

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const smartAlgorithmVersion = "smart-v1"

type recordingCluster struct {
	members            []MatchCandidate
	query              Query
	identityScore      float64
	releaseScore       float64
	conflicts          []string
	strongIdentifier   bool
	independentSources int
}

type releaseCluster struct {
	members   []MatchCandidate
	score     float64
	key       string
	ambiguous bool
}

// buildSmartCandidate creates one deterministic, field-level composite from
// every source candidate that belongs to the best recording cluster. It never
// performs network IO: provider proxy/rate-limit ownership stays in Registry.
func buildSmartCandidate(queries []Query, candidates []MatchCandidate) (MatchCandidate, bool) {
	sources := make([]MatchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Kind == "" || candidate.Kind == CandidateKindSource {
			sources = append(sources, candidate)
		}
	}
	if len(sources) == 0 {
		return MatchCandidate{}, false
	}
	sort.SliceStable(sources, func(i, j int) bool {
		if sources[i].Score == sources[j].Score {
			if sourceCompleteness(sources[i]) == sourceCompleteness(sources[j]) {
				if sources[i].ProviderID == sources[j].ProviderID {
					return sources[i].ID < sources[j].ID
				}
				return sources[i].ProviderID < sources[j].ProviderID
			}
			return sourceCompleteness(sources[i]) > sourceCompleteness(sources[j])
		}
		return sources[i].Score > sources[j].Score
	})

	clusters := clusterRecordings(sources)
	for index := range clusters {
		scoreRecordingCluster(&clusters[index], queries)
	}
	sort.SliceStable(clusters, func(i, j int) bool {
		if clusters[i].identityScore == clusters[j].identityScore {
			if clusters[i].independentSources == clusters[j].independentSources {
				if len(clusters[i].members) == len(clusters[j].members) {
					return clusters[i].members[0].ID < clusters[j].members[0].ID
				}
				return len(clusters[i].members) > len(clusters[j].members)
			}
			return clusters[i].independentSources > clusters[j].independentSources
		}
		return clusters[i].identityScore > clusters[j].identityScore
	})
	best := clusters[0]
	if best.identityScore < 0.55 {
		return MatchCandidate{}, false
	}
	margin := best.identityScore
	if len(clusters) > 1 {
		margin = math.Max(0, best.identityScore-clusters[1].identityScore)
	}

	release := selectReleaseCluster(best.members, best.query)
	best.releaseScore = release.score
	if release.ambiguous {
		best.conflicts = appendUnique(best.conflicts, "存在多个相近发行版本")
	}
	releaseMembers := release.members
	if len(releaseMembers) == 0 {
		releaseMembers = best.members
	}

	memberIDs := make([]string, 0, len(best.members))
	contributors := make([]SourceReference, 0, len(best.members))
	seenContributor := make(map[string]struct{})
	for _, member := range best.members {
		memberIDs = append(memberIDs, member.ID)
		ref := candidateSourceReference(member)
		key := ref.ProviderID + "\x00" + ref.CandidateID
		if _, found := seenContributor[key]; found {
			continue
		}
		seenContributor[key] = struct{}{}
		contributors = append(contributors, ref)
	}
	sort.Strings(memberIDs)

	title := chooseField(best.members, func(candidate MatchCandidate) Field[string] { return candidate.Title }, emptyString, normalize)
	artists := chooseField(best.members, func(candidate MatchCandidate) Field[[]string] { return candidate.Artists }, emptyStrings, normalizedStringsKey)
	duration := chooseField(best.members, func(candidate MatchCandidate) Field[int64] { return candidate.DurationSeconds }, emptyInt64, func(value int64) string {
		if value <= 0 {
			return ""
		}
		// Providers often round by one second; group those values together.
		return strconv.FormatInt((value+1)/2, 10)
	})

	album := chooseField(releaseMembers, func(candidate MatchCandidate) Field[string] { return candidate.Album }, emptyString, normalize)
	albumArtists := chooseField(releaseMembers, func(candidate MatchCandidate) Field[[]string] { return candidate.AlbumArtists }, emptyStrings, normalizedStringsKey)
	year := chooseField(releaseMembers, func(candidate MatchCandidate) Field[int] { return candidate.Year }, emptyInt, positiveIntKey)
	trackNumber := chooseField(releaseMembers, func(candidate MatchCandidate) Field[int] { return candidate.TrackNumber }, emptyInt, positiveIntKey)
	trackTotal := chooseField(releaseMembers, func(candidate MatchCandidate) Field[int] { return candidate.TrackTotal }, emptyInt, positiveIntKey)
	discNumber := chooseField(releaseMembers, func(candidate MatchCandidate) Field[int] { return candidate.DiscNumber }, emptyInt, positiveIntKey)
	discTotal := chooseField(releaseMembers, func(candidate MatchCandidate) Field[int] { return candidate.DiscTotal }, emptyInt, positiveIntKey)
	genres := mergeStringValues(releaseMembers, func(candidate MatchCandidate) Field[[]string] { return candidate.Genres })
	comment := chooseField(releaseMembers, func(candidate MatchCandidate) Field[string] { return candidate.Comment }, emptyString, normalize)
	composers := chooseField(releaseMembers, func(candidate MatchCandidate) Field[[]string] { return candidate.Composers }, emptyStrings, normalizedStringsKey)
	conductor := chooseField(releaseMembers, func(candidate MatchCandidate) Field[string] { return candidate.Conductor }, emptyString, normalize)
	lyricists := chooseField(releaseMembers, func(candidate MatchCandidate) Field[[]string] { return candidate.Lyricists }, emptyStrings, normalizedStringsKey)
	copyright := chooseField(releaseMembers, func(candidate MatchCandidate) Field[string] { return candidate.Copyright }, emptyString, normalize)
	bpm := chooseField(best.members, func(candidate MatchCandidate) Field[int] { return candidate.BPM }, emptyInt, positiveIntKey)
	isrc := chooseField(best.members, func(candidate MatchCandidate) Field[string] { return candidate.ISRC }, emptyString, normalizeIdentifier)
	mbTrackID := chooseField(best.members, func(candidate MatchCandidate) Field[string] { return candidate.MusicBrainzTrackID }, emptyString, normalizeIdentifier)
	mbReleaseID := chooseField(releaseMembers, func(candidate MatchCandidate) Field[string] { return candidate.MusicBrainzReleaseID }, emptyString, normalizeIdentifier)
	mbArtistIDs := chooseField(best.members, func(candidate MatchCandidate) Field[[]string] { return candidate.MusicBrainzArtistIDs }, emptyStrings, normalizedStringsKey)
	acoustID := chooseField(best.members, func(candidate MatchCandidate) Field[string] { return candidate.AcoustID }, emptyString, normalizeIdentifier)
	fingerprint := chooseField(best.members, func(candidate MatchCandidate) Field[string] { return candidate.AcoustIDFingerprint }, emptyString, normalizeIdentifier)
	artists = nonNilStringField(artists)
	albumArtists = nonNilStringField(albumArtists)
	genres = nonNilStringField(genres)
	composers = nonNilStringField(composers)
	lyricists = nonNilStringField(lyricists)
	mbArtistIDs = nonNilStringField(mbArtistIDs)
	lyrics := chooseLyrics(best.members)
	artworkMember, hasArtwork := chooseArtwork(best.members, release.members)

	externalID := shortHash(strings.Join(memberIDs, "\x00") + "\x00" + release.key + "\x00" + smartAlgorithmVersion)
	identityScore := clamp(best.identityScore)
	level := evidenceLevel(identityScore)
	if best.strongIdentifier && len(best.conflicts) == 0 {
		level = "exact"
	}
	autoAccept := identityScore >= 0.94 && margin >= 0.08 && len(best.conflicts) == 0 &&
		(best.independentSources >= 2 || best.strongIdentifier) && title.Value != "" && len(artists.Value) > 0
	matchReasons := []string{"综合 " + strconv.Itoa(distinctProviderCount(best.members)) + " 个数据源"}
	if best.independentSources >= 2 {
		matchReasons = append(matchReasons, strconv.Itoa(best.independentSources)+" 个独立来源确认同一录音")
	}
	if best.strongIdentifier {
		matchReasons = append(matchReasons, "稳定标识确认")
	}
	if album.Value != "" {
		matchReasons = append(matchReasons, "发行版本字段保持一致")
	}
	matchReasons = append(matchReasons, best.conflicts...)

	smart := MatchCandidate{
		ID:                   "cand-smart-" + externalID,
		Kind:                 CandidateKindSmart,
		ProviderID:           "smart",
		ProviderName:         "智能选择",
		ExternalID:           externalID,
		MemberCandidateIDs:   memberIDs,
		Contributors:         contributors,
		Title:                title,
		Artists:              artists,
		Album:                album,
		AlbumArtists:         albumArtists,
		Year:                 year,
		TrackNumber:          trackNumber,
		TrackTotal:           trackTotal,
		DiscNumber:           discNumber,
		DiscTotal:            discTotal,
		DurationSeconds:      duration,
		Genres:               genres,
		Comment:              comment,
		Composers:            composers,
		Conductor:            conductor,
		Lyricists:            lyricists,
		Copyright:            copyright,
		BPM:                  bpm,
		ISRC:                 isrc,
		MusicBrainzTrackID:   mbTrackID,
		MusicBrainzReleaseID: mbReleaseID,
		MusicBrainzArtistIDs: mbArtistIDs,
		AcoustID:             acoustID,
		AcoustIDFingerprint:  fingerprint,
		HasLyrics:            lyrics != nil,
		HasArtwork:           hasArtwork,
		CoverTone:            toneFor("smart" + externalID),
		Score:                math.Round(identityScore*100) / 100,
		ScoreLabel:           smartScoreLabel(level),
		MatchReasons:         matchReasons,
		Recommended:          true,
		AutoAccept:           autoAccept,
	}
	if lyrics != nil {
		smart.Lyrics = lyrics
	}
	if hasArtwork {
		ref := candidateSourceReference(artworkMember)
		smart.ArtworkReferenceID = artworkMember.ArtworkReferenceID
		if smart.ArtworkReferenceID == "" {
			smart.ArtworkReferenceID = artworkMember.ID
		}
		smart.ArtworkSource = &ref
	}
	smart.Evidence = MatchEvidence{
		IdentityScore: identityScore, ReleaseScore: clamp(best.releaseScore),
		CompletenessScore: smartCompleteness(smart), AssetQuality: smartAssetQuality(smart),
		Margin: margin, Level: level, SourceCount: distinctProviderCount(best.members),
		Conflicts: append([]string(nil), best.conflicts...), AlgorithmVersion: smartAlgorithmVersion,
	}
	return smart, true
}

func clusterRecordings(candidates []MatchCandidate) []recordingCluster {
	clusters := make([]recordingCluster, 0, len(candidates))
	for _, candidate := range candidates {
		bestIndex, bestScore := -1, 0.0
		for index := range clusters {
			score, compatible := recordingClusterFit(clusters[index].members, candidate)
			if !compatible {
				continue
			}
			if bestIndex < 0 || score > bestScore {
				bestIndex, bestScore = index, score
			}
		}
		if bestIndex < 0 {
			clusters = append(clusters, recordingCluster{members: []MatchCandidate{candidate}})
			continue
		}
		clusters[bestIndex].members = append(clusters[bestIndex].members, candidate)
	}
	return clusters
}

func recordingClusterFit(members []MatchCandidate, candidate MatchCandidate) (float64, bool) {
	if len(members) == 0 {
		return 0, false
	}
	total := 0.0
	for _, member := range members {
		score, strong, conflict := recordingPairScore(member, candidate)
		if conflict || (!strong && score < 0.82) {
			return 0, false
		}
		total += score
	}
	return total / float64(len(members)), true
}

func recordingPairScore(left, right MatchCandidate) (float64, bool, bool) {
	strong, identifierConflict := compareCandidateIdentifiers(left, right)
	if identifierConflict {
		return 0, strong, true
	}
	leftBase, leftQualifiers := titleProfile(left.Title.Value)
	rightBase, rightQualifiers := titleProfile(right.Title.Value)
	titleScore := similarity(leftBase, rightBase)
	artistScore := artistSimilarity(left.Artists.Value, right.Artists.Value)
	versionScore := qualifierSimilarity(leftQualifiers, rightQualifiers)
	if qualifierConflict(leftQualifiers, rightQualifiers) {
		return 0, strong, true
	}
	durationScore := 0.0
	hasDuration := left.DurationSeconds.Value > 0 && right.DurationSeconds.Value > 0
	if hasDuration {
		delta := math.Abs(float64(left.DurationSeconds.Value - right.DurationSeconds.Value))
		if delta > 30 && !strong {
			return 0, strong, true
		}
		durationScore = math.Max(0, 1-delta/25)
	}
	if !strong && (titleScore < 0.72 || (len(left.Artists.Value) > 0 && len(right.Artists.Value) > 0 && artistScore < 0.55)) {
		return 0, false, true
	}
	score, weight := titleScore*0.55, 0.55
	if len(left.Artists.Value) > 0 && len(right.Artists.Value) > 0 {
		score += artistScore * 0.27
		weight += 0.27
	}
	if hasDuration {
		score += durationScore * 0.12
		weight += 0.12
	}
	if len(leftQualifiers) > 0 || len(rightQualifiers) > 0 {
		score += versionScore * 0.06
		weight += 0.06
	}
	score /= weight
	if strong {
		score = math.Max(score, 0.99)
	}
	return clamp(score), strong, false
}

func compareCandidateIdentifiers(left, right MatchCandidate) (bool, bool) {
	pairs := [][2]string{
		{left.ISRC.Value, right.ISRC.Value},
		{left.MusicBrainzTrackID.Value, right.MusicBrainzTrackID.Value},
		{left.AcoustID.Value, right.AcoustID.Value},
		{left.AcoustIDFingerprint.Value, right.AcoustIDFingerprint.Value},
	}
	matched := false
	for _, pair := range pairs {
		first, second := normalizeIdentifier(pair[0]), normalizeIdentifier(pair[1])
		if first == "" || second == "" {
			continue
		}
		if first != second {
			return matched, true
		}
		matched = true
	}
	return matched, false
}

func scoreRecordingCluster(cluster *recordingCluster, queries []Query) {
	if len(queries) == 0 {
		queries = []Query{{Title: cluster.members[0].Title.Value, Artists: cluster.members[0].Artists.Value}}
	}
	bestQueryScore := -1.0
	for _, query := range queries {
		maximum, sum := 0.0, 0.0
		for _, member := range cluster.members {
			score, _ := scoreCandidate(query, candidateFromView(member))
			maximum = math.Max(maximum, score)
			sum += score
		}
		queryScore := maximum*0.72 + (sum/float64(len(cluster.members)))*0.28
		if queryScore > bestQueryScore {
			bestQueryScore = queryScore
			cluster.query = query
		}
	}
	consistency := 1.0
	if len(cluster.members) > 1 {
		total := 0.0
		for _, member := range cluster.members[1:] {
			score, _, conflict := recordingPairScore(cluster.members[0], member)
			if conflict {
				continue
			}
			total += score
		}
		consistency = total / float64(len(cluster.members)-1)
	}
	cluster.independentSources = independentProviderCount(cluster.members)
	supportBonus := math.Min(0.06, float64(max(0, cluster.independentSources-1))*0.025)
	cluster.identityScore = clamp(bestQueryScore*0.86 + consistency*0.08 + supportBonus)
	cluster.strongIdentifier = queryHasStrongIdentifierMatch(cluster.query, cluster.members)
	cluster.conflicts = clusterConflicts(cluster.query, cluster.members)
	if len(cluster.conflicts) > 0 {
		cluster.identityScore = math.Min(cluster.identityScore, 0.93)
	}
}

func candidateFromView(candidate MatchCandidate) Candidate {
	return Candidate{
		ProviderID: candidate.ProviderID, ExternalID: candidate.ExternalID,
		Title: candidate.Title.Value, Artists: candidate.Artists.Value, Album: candidate.Album.Value,
		AlbumArtists: candidate.AlbumArtists.Value, Year: candidate.Year.Value,
		TrackNumber: candidate.TrackNumber.Value, TrackTotal: candidate.TrackTotal.Value,
		DiscNumber: candidate.DiscNumber.Value, DiscTotal: candidate.DiscTotal.Value,
		DurationSeconds: candidate.DurationSeconds.Value, Genres: candidate.Genres.Value,
		Comment: candidate.Comment.Value, Composers: candidate.Composers.Value,
		Conductor: candidate.Conductor.Value, Lyricists: candidate.Lyricists.Value,
		Copyright: candidate.Copyright.Value, BPM: candidate.BPM.Value,
		ISRC: candidate.ISRC.Value, MusicBrainzTrackID: candidate.MusicBrainzTrackID.Value,
		MusicBrainzReleaseID: candidate.MusicBrainzReleaseID.Value,
		MusicBrainzArtistIDs: candidate.MusicBrainzArtistIDs.Value,
		AcoustID:             candidate.AcoustID.Value, AcoustIDFingerprint: candidate.AcoustIDFingerprint.Value,
	}
}

func queryHasStrongIdentifierMatch(query Query, members []MatchCandidate) bool {
	for _, member := range members {
		matched, conflict := compareQueryIdentifiers(query, candidateFromView(member))
		if matched && !conflict {
			return true
		}
	}
	return false
}

func clusterConflicts(query Query, members []MatchCandidate) []string {
	conflicts := make([]string, 0, 3)
	queryBase, queryQualifiers := titleProfile(query.Title)
	for _, member := range members {
		matched, identifierConflict := compareQueryIdentifiers(query, candidateFromView(member))
		_ = matched
		if identifierConflict {
			conflicts = appendUnique(conflicts, "稳定标识存在冲突")
		}
		memberBase, memberQualifiers := titleProfile(member.Title.Value)
		if queryBase != "" && memberBase != "" && similarity(queryBase, memberBase) < 0.72 {
			conflicts = appendUnique(conflicts, "标题差异较大")
		}
		if qualifierConflict(queryQualifiers, memberQualifiers) {
			conflicts = appendUnique(conflicts, "版本标记存在冲突")
		} else if len(queryQualifiers) == 0 && len(memberQualifiers) > 0 {
			conflicts = appendUnique(conflicts, "候选包含版本标记，查询未指定版本")
		}
		if query.DurationSeconds > 0 && member.DurationSeconds.Value > 0 && absInt64(query.DurationSeconds-member.DurationSeconds.Value) > 15 {
			conflicts = appendUnique(conflicts, "时长差异较大")
		}
	}
	return conflicts
}

func selectReleaseCluster(members []MatchCandidate, query Query) releaseCluster {
	groups := make([]releaseCluster, 0)
	for _, member := range members {
		if strings.TrimSpace(member.Album.Value) == "" && strings.TrimSpace(member.MusicBrainzReleaseID.Value) == "" {
			continue
		}
		bestIndex, bestScore := -1, 0.0
		for index := range groups {
			score, compatible := releaseClusterFit(groups[index].members, member)
			if compatible && (bestIndex < 0 || score > bestScore) {
				bestIndex, bestScore = index, score
			}
		}
		if bestIndex < 0 {
			groups = append(groups, releaseCluster{members: []MatchCandidate{member}})
		} else {
			groups[bestIndex].members = append(groups[bestIndex].members, member)
		}
	}
	if len(groups) == 0 {
		return releaseCluster{}
	}
	for index := range groups {
		group := &groups[index]
		anchor := group.members[0]
		albumScore := releaseQueryScore(query, group.members)
		if query.MusicBrainzReleaseID != "" && normalizeIdentifier(query.MusicBrainzReleaseID) == normalizeIdentifier(anchor.MusicBrainzReleaseID.Value) {
			albumScore = 1
		}
		support := math.Min(0.12, float64(max(0, independentProviderCount(group.members)-1))*0.04)
		group.score = clamp(albumScore*0.76 + sourceCompleteness(anchor)*0.12 + support)
		group.key = normalizeIdentifier(anchor.MusicBrainzReleaseID.Value)
		if group.key == "" {
			group.key = normalize(anchor.Album.Value) + "\x00" + normalizedStringsKey(anchor.AlbumArtists.Value)
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].score == groups[j].score {
			return independentProviderCount(groups[i].members) > independentProviderCount(groups[j].members)
		}
		return groups[i].score > groups[j].score
	})
	if len(groups) > 1 {
		groups[0].ambiguous = query.Album == "" && query.MusicBrainzReleaseID == "" && len(query.AlbumArtists) == 0 && query.Year <= 0 && query.TrackNumber <= 0 && query.DiscNumber <= 0
		if groups[0].score-groups[1].score < 0.08 {
			groups[0].ambiguous = true
		}
	}
	return groups[0]
}

func releaseQueryScore(query Query, members []MatchCandidate) float64 {
	if len(members) == 0 {
		return 0
	}
	score, weight := 0.0, 0.0
	bestString := func(value string, getter func(MatchCandidate) string) float64 {
		best := 0.0
		for _, member := range members {
			best = math.Max(best, similarity(value, getter(member)))
		}
		return best
	}
	if query.Album != "" {
		score += bestString(query.Album, func(candidate MatchCandidate) string { return candidate.Album.Value }) * 0.5
		weight += 0.5
	}
	if len(query.AlbumArtists) > 0 {
		best := 0.0
		for _, member := range members {
			best = math.Max(best, artistSimilarity(query.AlbumArtists, member.AlbumArtists.Value))
		}
		score += best * 0.2
		weight += 0.2
	}
	if query.Year > 0 {
		best := 0.0
		for _, member := range members {
			if member.Year.Value <= 0 {
				continue
			}
			best = math.Max(best, math.Max(0, 1-float64(absInt(query.Year-member.Year.Value))/3))
		}
		score += best * 0.1
		weight += 0.1
	}
	if query.TrackNumber > 0 {
		best := 0.0
		for _, member := range members {
			if member.TrackNumber.Value == query.TrackNumber {
				best = 1
				break
			}
		}
		score += best * 0.15
		weight += 0.15
	}
	if query.DiscNumber > 0 {
		best := 0.0
		for _, member := range members {
			if member.DiscNumber.Value == query.DiscNumber {
				best = 1
				break
			}
		}
		score += best * 0.05
		weight += 0.05
	}
	if weight == 0 {
		return 0.65
	}
	return clamp(score / weight)
}

func releaseClusterFit(members []MatchCandidate, candidate MatchCandidate) (float64, bool) {
	if len(members) == 0 {
		return 0, false
	}
	total := 0.0
	for _, member := range members {
		score, compatible := releasePairScore(member, candidate)
		if !compatible {
			return 0, false
		}
		total += score
	}
	return total / float64(len(members)), true
}

func releasePairScore(left, right MatchCandidate) (float64, bool) {
	leftID, rightID := normalizeIdentifier(left.MusicBrainzReleaseID.Value), normalizeIdentifier(right.MusicBrainzReleaseID.Value)
	if leftID != "" && rightID != "" {
		return boolScore(leftID == rightID), leftID == rightID
	}
	albumScore := similarity(left.Album.Value, right.Album.Value)
	if albumScore < 0.9 {
		return albumScore, false
	}
	artistScore := 0.8
	if len(left.AlbumArtists.Value) > 0 && len(right.AlbumArtists.Value) > 0 {
		artistScore = artistSimilarity(left.AlbumArtists.Value, right.AlbumArtists.Value)
		if artistScore < 0.6 {
			return 0, false
		}
	}
	if left.Year.Value > 0 && right.Year.Value > 0 && absInt(left.Year.Value-right.Year.Value) > 1 {
		return 0, false
	}
	return albumScore*0.75 + artistScore*0.25, true
}

type fieldChoice[T any] struct {
	value      T
	primary    MatchCandidate
	field      Field[T]
	supporters []MatchCandidate
	score      float64
}

func chooseField[T any](members []MatchCandidate, getter func(MatchCandidate) Field[T], empty func(T) bool, keyFor func(T) string) Field[T] {
	groups := make(map[string]*fieldChoice[T])
	order := make([]string, 0)
	for _, member := range members {
		field := getter(member)
		if empty(field.Value) {
			continue
		}
		key := keyFor(field.Value)
		if key == "" {
			continue
		}
		choice, found := groups[key]
		if !found {
			choice = &fieldChoice[T]{value: field.Value, primary: member, field: field}
			groups[key] = choice
			order = append(order, key)
		}
		choice.supporters = append(choice.supporters, member)
		if sourceWeight(member) > sourceWeight(choice.primary) {
			choice.value, choice.primary, choice.field = field.Value, member, field
		}
	}
	var best *fieldChoice[T]
	for _, key := range order {
		choice := groups[key]
		choice.score = consensusWeight(choice.supporters)
		if best == nil || choice.score > best.score || (choice.score == best.score && sourceWeight(choice.primary) > sourceWeight(best.primary)) {
			best = choice
		}
	}
	if best == nil {
		return Field[T]{}
	}
	refs := referencesForMembers(best.supporters)
	return Field[T]{
		Value: best.value, Source: best.field.Source, Sources: refs,
		Confidence: fieldConfidence(best.primary, refs), Derived: best.field.Derived,
	}
}

func mergeStringValues(members []MatchCandidate, getter func(MatchCandidate) Field[[]string]) Field[[]string] {
	values := make([]string, 0)
	refs := make([]SourceReference, 0)
	seenValues, seenRefs := map[string]struct{}{}, map[string]struct{}{}
	source := ""
	for _, member := range members {
		field := getter(member)
		if source == "" && len(field.Value) > 0 {
			source = field.Source
		}
		for _, value := range field.Value {
			key := normalize(value)
			if key == "" {
				continue
			}
			if _, found := seenValues[key]; !found {
				seenValues[key] = struct{}{}
				values = append(values, strings.TrimSpace(value))
			}
		}
		if len(field.Value) > 0 {
			ref := candidateSourceReference(member)
			key := ref.ProviderID + "\x00" + ref.CandidateID
			if _, found := seenRefs[key]; !found {
				seenRefs[key] = struct{}{}
				refs = append(refs, ref)
			}
		}
	}
	confidence := 0.0
	if len(members) > 0 && len(values) > 0 {
		confidence = fieldConfidence(members[0], refs)
	}
	return Field[[]string]{Value: values, Source: source, Sources: refs, Confidence: confidence}
}

func chooseLyrics(members []MatchCandidate) *Field[string] {
	bestScore := -1.0
	var best MatchCandidate
	var bestField Field[string]
	for _, member := range members {
		if member.Lyrics == nil || strings.TrimSpace(member.Lyrics.Value) == "" {
			continue
		}
		lyrics := member.Lyrics.Value
		syncedBonus := 0.0
		if looksSyncedLyrics(lyrics) {
			syncedBonus = 0.18
		}
		lengthScore := math.Min(1, float64(utf8.RuneCountInString(lyrics))/800)
		score := sourceWeight(member)*0.72 + syncedBonus + lengthScore*0.1
		if score > bestScore {
			bestScore, best, bestField = score, member, *member.Lyrics
		}
	}
	if bestScore < 0 {
		return nil
	}
	refs := referencesForMembers([]MatchCandidate{best})
	result := Field[string]{Value: bestField.Value, Source: bestField.Source, Sources: refs, Confidence: fieldConfidence(best, refs)}
	return &result
}

func looksSyncedLyrics(value string) bool {
	for _, prefix := range []string{"[00:", "[01:", "[02:", "[03:", "[04:", "[05:"} {
		if strings.Contains(value, prefix) {
			return true
		}
	}
	return false
}

func chooseArtwork(recordingMembers, releaseMembers []MatchCandidate) (MatchCandidate, bool) {
	candidates := releaseMembers
	if len(candidates) == 0 {
		candidates = recordingMembers
	}
	bestScore := -1.0
	var best MatchCandidate
	for _, candidate := range candidates {
		if !candidate.HasArtwork {
			continue
		}
		score := sourceWeight(candidate)*0.8 + sourceCompleteness(candidate)*0.2
		if score > bestScore {
			bestScore, best = score, candidate
		}
	}
	if bestScore >= 0 {
		return best, true
	}
	if len(releaseMembers) > 0 {
		return chooseArtwork(recordingMembers, nil)
	}
	return MatchCandidate{}, false
}

func sourceWeight(candidate MatchCandidate) float64 {
	identity := candidate.Evidence.IdentityScore
	if identity == 0 {
		identity = candidate.Score
	}
	weight := 0.55 + clamp(identity)*0.4 + sourceCompleteness(candidate)*0.05
	if candidate.ProviderID == "lrcapi" {
		// LrcApi is an aggregate adapter, so it can provide a value but should not
		// receive the same independent-evidence weight as a first-party source.
		weight *= 0.72
	}
	return weight
}

func consensusWeight(members []MatchCandidate) float64 {
	byProvider := make(map[string]float64)
	for _, member := range members {
		weight := sourceWeight(member)
		if weight > byProvider[member.ProviderID] {
			byProvider[member.ProviderID] = weight
		}
	}
	total := 0.0
	for providerID, weight := range byProvider {
		if providerID == "lrcapi" {
			weight *= 0.6
		}
		total += weight
	}
	return total
}

func fieldConfidence(primary MatchCandidate, refs []SourceReference) float64 {
	return clamp(sourceWeight(primary)*0.82 + math.Min(0.16, float64(max(0, len(refs)-1))*0.055))
}

func candidateSourceReference(candidate MatchCandidate) SourceReference {
	if len(candidate.Contributors) > 0 {
		return candidate.Contributors[0]
	}
	return SourceReference{ProviderID: candidate.ProviderID, ProviderName: candidate.ProviderName, CandidateID: candidate.ID, ExternalID: candidate.ExternalID}
}

func referencesForMembers(members []MatchCandidate) []SourceReference {
	refs := make([]SourceReference, 0, len(members))
	seen := make(map[string]struct{})
	for _, member := range members {
		ref := candidateSourceReference(member)
		key := ref.ProviderID + "\x00" + ref.CandidateID
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

func sourceCompleteness(candidate MatchCandidate) float64 {
	if candidate.Evidence.CompletenessScore > 0 {
		return candidate.Evidence.CompletenessScore
	}
	present, total := 0, 16
	if candidate.Title.Value != "" {
		present++
	}
	if len(candidate.Artists.Value) > 0 {
		present++
	}
	if candidate.Album.Value != "" {
		present++
	}
	if len(candidate.AlbumArtists.Value) > 0 {
		present++
	}
	if candidate.Year.Value > 0 {
		present++
	}
	if candidate.TrackNumber.Value > 0 {
		present++
	}
	if candidate.TrackTotal.Value > 0 {
		present++
	}
	if candidate.DiscNumber.Value > 0 {
		present++
	}
	if candidate.DurationSeconds.Value > 0 {
		present++
	}
	if len(candidate.Genres.Value) > 0 {
		present++
	}
	if candidate.HasLyrics {
		present++
	}
	if candidate.HasArtwork {
		present++
	}
	if candidate.ISRC.Value != "" {
		present++
	}
	if candidate.MusicBrainzTrackID.Value != "" {
		present++
	}
	if candidate.MusicBrainzReleaseID.Value != "" {
		present++
	}
	if candidate.AcoustID.Value != "" || candidate.AcoustIDFingerprint.Value != "" {
		present++
	}
	return float64(present) / float64(total)
}

func smartCompleteness(candidate MatchCandidate) float64 { return sourceCompleteness(candidate) }

func smartAssetQuality(candidate MatchCandidate) float64 {
	quality := 0.0
	if candidate.HasArtwork {
		quality += 0.5
	}
	if candidate.HasLyrics {
		quality += 0.5
	}
	return quality
}

func distinctProviderCount(members []MatchCandidate) int {
	seen := make(map[string]struct{})
	for _, member := range members {
		if member.ProviderID != "" {
			seen[member.ProviderID] = struct{}{}
		}
	}
	return len(seen)
}

func independentProviderCount(members []MatchCandidate) int {
	seen := make(map[string]struct{})
	for _, member := range members {
		if member.ProviderID == "" || member.ProviderID == "lrcapi" {
			continue
		}
		seen[member.ProviderID] = struct{}{}
	}
	return len(seen)
}

func smartScoreLabel(level string) string {
	switch level {
	case "exact":
		return "智能精确匹配"
	case "high":
		return "智能高置信"
	case "review":
		return "智能建议复核"
	default:
		return "智能低置信"
	}
}

func normalizedStringsKey(values []string) string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if item := normalize(value); item != "" {
			normalized = append(normalized, item)
		}
	}
	sort.Strings(normalized)
	return strings.Join(normalized, "\x1f")
}

func positiveIntKey(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func emptyString(value string) bool     { return strings.TrimSpace(value) == "" }
func emptyStrings(values []string) bool { return len(values) == 0 }
func emptyInt(value int) bool           { return value <= 0 }
func emptyInt64(value int64) bool       { return value <= 0 }
func nonNilStringField(field Field[[]string]) Field[[]string] {
	if field.Value == nil {
		field.Value = []string{}
	}
	return field
}
func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
func boolScore(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}
