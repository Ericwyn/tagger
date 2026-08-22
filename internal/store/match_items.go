package store

import (
	"context"
	"encoding/json"
	"time"
)

type MatchItem struct {
	ID                  string          `json:"id"`
	JobID               string          `json:"jobId"`
	TrackID             string          `json:"trackId"`
	State               string          `json:"state"`
	Candidates          json.RawMessage `json:"candidates"`
	SelectedCandidateID string          `json:"selectedCandidateId,omitempty"`
	ReviewFields        []string        `json:"reviewFields"`
	ReviewArtwork       bool            `json:"reviewArtwork"`
	Error               string          `json:"error,omitempty"`
	UpdatedAt           time.Time       `json:"updatedAt"`
}

func (s *Store) UpsertMatchItem(ctx context.Context, item MatchItem) error {
	if item.ID == "" {
		item.ID = newID("match", s.now())
	}
	if item.State == "" {
		item.State = "review"
	}
	payload := item.Candidates
	if len(payload) == 0 {
		payload = []byte("[]")
	}
	var reviewFields any
	if item.ReviewFields != nil {
		reviewFields, _ = json.Marshal(item.ReviewFields)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO match_items(id, job_id, track_id, state, candidates_json, selected_candidate_id, review_fields_json, review_artwork, error_text, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id, track_id) DO UPDATE SET state=excluded.state, candidates_json=excluded.candidates_json,
		selected_candidate_id=excluded.selected_candidate_id, review_fields_json=excluded.review_fields_json,
		review_artwork=excluded.review_artwork, error_text=excluded.error_text, updated_at=excluded.updated_at`,
		item.ID, item.JobID, item.TrackID, item.State, payload, item.SelectedCandidateID, reviewFields, boolToInt(item.ReviewArtwork), item.Error, formatTime(s.now()))
	return err
}

func (s *Store) ListMatchItems(ctx context.Context, jobID string) ([]MatchItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, job_id, track_id, state, candidates_json, selected_candidate_id, review_fields_json, review_artwork, error_text, updated_at FROM match_items WHERE job_id=? ORDER BY track_id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []MatchItem{}
	for rows.Next() {
		var item MatchItem
		var payload, reviewFields []byte
		var reviewArtwork int
		var updated string
		if err := rows.Scan(&item.ID, &item.JobID, &item.TrackID, &item.State, &payload, &item.SelectedCandidateID, &reviewFields, &reviewArtwork, &item.Error, &updated); err != nil {
			return nil, err
		}
		item.Candidates = append(json.RawMessage(nil), payload...)
		item.ReviewFields = decodeReviewFields(reviewFields)
		item.ReviewArtwork = reviewArtwork != 0
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) MatchItem(ctx context.Context, jobID, trackID string) (MatchItem, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, job_id, track_id, state, candidates_json, selected_candidate_id, review_fields_json, review_artwork, error_text, updated_at FROM match_items WHERE job_id=? AND track_id=?`, jobID, trackID)
	var item MatchItem
	var payload, reviewFields []byte
	var reviewArtwork int
	var updated string
	if err := row.Scan(&item.ID, &item.JobID, &item.TrackID, &item.State, &payload, &item.SelectedCandidateID, &reviewFields, &reviewArtwork, &item.Error, &updated); err != nil {
		return MatchItem{}, err
	}
	item.Candidates = append(json.RawMessage(nil), payload...)
	item.ReviewFields = decodeReviewFields(reviewFields)
	item.ReviewArtwork = reviewArtwork != 0
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return item, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func decodeReviewFields(payload []byte) []string {
	if len(payload) == 0 {
		return nil
	}
	var fields []string
	if json.Unmarshal(payload, &fields) != nil {
		return nil
	}
	return fields
}

func nonEmptyJSON(value string) string {
	if value == "" {
		return "{}"
	}
	return value
}
