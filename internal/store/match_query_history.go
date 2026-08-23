package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type MatchQueryHistory struct {
	ID          string          `json:"id"`
	TrackID     string          `json:"trackId"`
	Query       json.RawMessage `json:"query"`
	ProviderIDs []string        `json:"providerIds"`
	ResultCount int             `json:"resultCount"`
	CreatedAt   time.Time       `json:"createdAt"`
}

func (s *Store) AddMatchQueryHistory(ctx context.Context, trackID string, query json.RawMessage, providerIDs []string, resultCount int) (MatchQueryHistory, error) {
	return s.addMatchQueryHistory(ctx, "", trackID, query, providerIDs, resultCount)
}

func (s *Store) AddLibraryMatchQueryHistory(ctx context.Context, libraryID, trackID string, query json.RawMessage, providerIDs []string, resultCount int) (MatchQueryHistory, error) {
	return s.addMatchQueryHistory(ctx, libraryID, trackID, query, providerIDs, resultCount)
}

func (s *Store) addMatchQueryHistory(ctx context.Context, libraryID, trackID string, query json.RawMessage, providerIDs []string, resultCount int) (MatchQueryHistory, error) {
	if len(query) == 0 {
		query = []byte("{}")
	}
	if providerIDs == nil {
		providerIDs = []string{}
	}
	providersJSON, err := json.Marshal(providerIDs)
	if err != nil {
		return MatchQueryHistory{}, fmt.Errorf("encode query providers: %w", err)
	}
	history := MatchQueryHistory{
		ID:          newID("match-query", s.now()),
		TrackID:     trackID,
		Query:       append(json.RawMessage(nil), query...),
		ProviderIDs: append([]string(nil), providerIDs...),
		ResultCount: resultCount,
		CreatedAt:   s.now().UTC(),
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO match_query_history(id, library_id, track_id, query_json, provider_ids_json, result_count, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		history.ID, libraryID, history.TrackID, history.Query, providersJSON, history.ResultCount, formatTime(history.CreatedAt))
	if err != nil {
		return MatchQueryHistory{}, fmt.Errorf("save match query history: %w", err)
	}
	return history, nil
}

func (s *Store) ListLibraryMatchQueryHistory(ctx context.Context, libraryID, trackID string, limit int) ([]MatchQueryHistory, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, track_id, query_json, provider_ids_json, result_count, created_at
		FROM match_query_history WHERE (library_id=? OR library_id='') AND track_id=? ORDER BY created_at DESC, id DESC LIMIT ?`, libraryID, trackID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]MatchQueryHistory, 0)
	for rows.Next() {
		var history MatchQueryHistory
		var query, providerIDs, createdAt []byte
		if err := rows.Scan(&history.ID, &history.TrackID, &query, &providerIDs, &history.ResultCount, &createdAt); err != nil {
			return nil, err
		}
		history.Query = append(json.RawMessage(nil), query...)
		if err := json.Unmarshal(providerIDs, &history.ProviderIDs); err != nil {
			history.ProviderIDs = []string{}
		}
		if history.ProviderIDs == nil {
			history.ProviderIDs = []string{}
		}
		var parseErr error
		history.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, string(createdAt))
		if parseErr != nil {
			return nil, parseErr
		}
		result = append(result, history)
	}
	return result, rows.Err()
}

func (s *Store) ListMatchQueryHistory(ctx context.Context, trackID string, limit int) ([]MatchQueryHistory, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, track_id, query_json, provider_ids_json, result_count, created_at
		FROM match_query_history WHERE track_id=? ORDER BY created_at DESC, id DESC LIMIT ?`, trackID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]MatchQueryHistory, 0)
	for rows.Next() {
		var history MatchQueryHistory
		var query, providerIDs, createdAt []byte
		if err := rows.Scan(&history.ID, &history.TrackID, &query, &providerIDs, &history.ResultCount, &createdAt); err != nil {
			return nil, err
		}
		history.Query = append(json.RawMessage(nil), query...)
		if err := json.Unmarshal(providerIDs, &history.ProviderIDs); err != nil {
			history.ProviderIDs = []string{}
		}
		if history.ProviderIDs == nil {
			history.ProviderIDs = []string{}
		}
		history.CreatedAt, err = time.Parse(time.RFC3339Nano, string(createdAt))
		if err != nil {
			return nil, err
		}
		result = append(result, history)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
