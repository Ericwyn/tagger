package store

import (
	"context"
	"encoding/json"
	"time"
)

type BatchEditItem struct {
	ID        string          `json:"id"`
	JobID     string          `json:"jobId"`
	TrackID   string          `json:"trackId"`
	State     string          `json:"state"`
	Error     string          `json:"error,omitempty"`
	Diff      json.RawMessage `json:"diff"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

func (s *Store) UpsertBatchEditItem(ctx context.Context, item BatchEditItem) error {
	if item.ID == "" {
		item.ID = newID("batch-edit", s.now())
	}
	if item.State == "" {
		item.State = "pending"
	}
	diff := item.Diff
	if len(diff) == 0 {
		diff = []byte("[]")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO batch_edit_items(id, job_id, track_id, state, error_text, diff_json, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id, track_id) DO UPDATE SET state=excluded.state, error_text=excluded.error_text, diff_json=excluded.diff_json, updated_at=excluded.updated_at`,
		item.ID, item.JobID, item.TrackID, item.State, item.Error, diff, formatTime(s.now().UTC()))
	return err
}

func (s *Store) ListBatchEditItems(ctx context.Context, jobID string) ([]BatchEditItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, job_id, track_id, state, error_text, diff_json, updated_at FROM batch_edit_items WHERE job_id=? ORDER BY track_id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []BatchEditItem{}
	for rows.Next() {
		var item BatchEditItem
		var updated string
		if err := rows.Scan(&item.ID, &item.JobID, &item.TrackID, &item.State, &item.Error, &item.Diff, &updated); err != nil {
			return nil, err
		}
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
