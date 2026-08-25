package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericwyn/tagger/internal/domain"
)

// TrackQueries builds provider queries without promoting path-derived hints to
// embedded metadata. Explicit request values win, followed by real track tags;
// hints only fill fields that are still missing. Ambiguous filename hints are
// intentionally retained as separate queries.
func TrackQueries(track domain.Track, override Query) []Query {
	base := override
	if strings.TrimSpace(base.Title) == "" {
		base.Title = track.Title
	}
	if len(base.Artists) == 0 {
		base.Artists = append([]string(nil), track.Artists...)
	}
	if strings.TrimSpace(base.Album) == "" {
		base.Album = track.Album
	}
	if base.DurationSeconds <= 0 {
		base.DurationSeconds = track.DurationSeconds
	}

	queries := make([]Query, 0, max(1, len(track.TagHints)))
	seen := make(map[string]struct{})
	add := func(query Query) {
		query.Title = strings.TrimSpace(query.Title)
		query.Album = strings.TrimSpace(query.Album)
		query.Artists = cleanQueryArtists(query.Artists)
		if query.Title == "" {
			return
		}
		key := strings.ToLower(query.Title) + "\x00" + strings.ToLower(strings.Join(query.Artists, "\x1f")) + "\x00" + strings.ToLower(query.Album)
		if _, found := seen[key]; found {
			return
		}
		seen[key] = struct{}{}
		queries = append(queries, query)
	}

	needsHint := strings.TrimSpace(base.Title) == "" || len(base.Artists) == 0 || strings.TrimSpace(base.Album) == ""
	if !needsHint || len(track.TagHints) == 0 {
		add(base)
		return queries
	}
	for _, hint := range track.TagHints {
		query := base
		if query.Title == "" {
			query.Title = hint.Title
		}
		if len(query.Artists) == 0 {
			query.Artists = append([]string(nil), hint.Artists...)
		}
		if query.Album == "" {
			query.Album = hint.Album
		}
		add(query)
	}
	if len(queries) == 0 {
		add(base)
	}
	return queries
}

func cleanQueryArtists(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

// SearchTrack searches every viable real-tag/hint query and merges duplicate
// provider candidates by stable candidate ID, retaining the highest-scoring
// interpretation of an ambiguous filename.
func (r *Registry) SearchTrack(ctx context.Context, track domain.Track, override Query, providerIDs []string, limit int) (SearchResult, error) {
	queries := TrackQueries(track, override)
	if len(queries) == 0 {
		return SearchResult{}, fmt.Errorf("title is required")
	}
	merged := SearchResult{Candidates: []MatchCandidate{}, Providers: map[string]ProviderResult{}}
	candidates := make(map[string]MatchCandidate)
	var firstErr error
	succeeded := 0
	for _, query := range queries {
		result, err := r.Search(ctx, query, providerIDs, limit)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		succeeded++
		for _, candidate := range result.Candidates {
			current, found := candidates[candidate.ID]
			if !found || candidate.Score > current.Score {
				candidates[candidate.ID] = candidate
			}
		}
		for id, provider := range result.Providers {
			current, found := merged.Providers[id]
			if !found {
				merged.Providers[id] = provider
				continue
			}
			current.Count += provider.Count
			current.LatencyMS += provider.LatencyMS
			current.Cached = current.Cached && provider.Cached
			if current.Status != "ok" && provider.Status == "ok" {
				current.Status = "ok"
				current.Error = ""
				current.Hint = ""
				current.Retryable = false
				current.RetryAfterMS = 0
			}
			merged.Providers[id] = current
		}
	}
	if succeeded == 0 {
		return SearchResult{}, firstErr
	}
	for _, candidate := range candidates {
		merged.Candidates = append(merged.Candidates, candidate)
	}
	sortViews(merged.Candidates)
	return merged, nil
}
