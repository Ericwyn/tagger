package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
)

func (s *Store) UpsertOrganizeItem(ctx context.Context, item domain.OrganizeItem) error {
	warnings, err := json.Marshal(item.Warnings)
	if err != nil {
		return err
	}
	if item.ID == "" {
		item.ID = newID("org", s.now())
	}
	updatedAt := item.UpdatedAt
	if updatedAt == "" {
		updatedAt = s.now().UTC().Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO organize_items(
			id, job_id, track_id, source_path, target_path, sidecar_source,
			sidecar_target, state, error_text, warnings_json, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id, track_id) DO UPDATE SET
			source_path=excluded.source_path, target_path=excluded.target_path,
			sidecar_source=excluded.sidecar_source, sidecar_target=excluded.sidecar_target,
			state=excluded.state, error_text=excluded.error_text,
			warnings_json=excluded.warnings_json, updated_at=excluded.updated_at`,
		item.ID, item.JobID, item.TrackID, item.Source, item.Target,
		item.SidecarSource, item.SidecarTarget, item.State, item.Error, warnings, updatedAt)
	if err != nil {
		return fmt.Errorf("upsert organize item: %w", err)
	}
	return nil
}

func (s *Store) ListOrganizeItems(ctx context.Context, jobID string) ([]domain.OrganizeItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, job_id, track_id, source_path, target_path, sidecar_source,
		       sidecar_target, state, error_text, warnings_json, updated_at
		FROM organize_items WHERE job_id=? ORDER BY source_path`, jobID)
	if err != nil {
		return nil, fmt.Errorf("list organize items: %w", err)
	}
	defer rows.Close()
	items := make([]domain.OrganizeItem, 0)
	for rows.Next() {
		var item domain.OrganizeItem
		var warnings []byte
		if err := rows.Scan(&item.ID, &item.JobID, &item.TrackID, &item.Source, &item.Target,
			&item.SidecarSource, &item.SidecarTarget, &item.State, &item.Error, &warnings, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if len(warnings) > 0 && json.Unmarshal(warnings, &item.Warnings) != nil {
			item.Warnings = []string{}
		}
		if item.Warnings == nil {
			item.Warnings = []string{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
