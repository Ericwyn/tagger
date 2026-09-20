-- +goose Up
CREATE TABLE organize_items (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    track_id TEXT NOT NULL,
    source_path TEXT NOT NULL,
    target_path TEXT NOT NULL,
    sidecar_source TEXT NOT NULL DEFAULT '',
    sidecar_target TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    error_text TEXT NOT NULL DEFAULT '',
    warnings_json BLOB NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL,
    UNIQUE(job_id, track_id)
);
CREATE INDEX organize_items_job_state_idx ON organize_items(job_id, state);

-- +goose Down
DROP INDEX organize_items_job_state_idx;
DROP TABLE organize_items;
