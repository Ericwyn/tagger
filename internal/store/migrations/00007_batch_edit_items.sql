-- +goose Up
CREATE TABLE batch_edit_items (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    track_id TEXT NOT NULL,
    state TEXT NOT NULL,
    error_text TEXT NOT NULL DEFAULT '',
    diff_json BLOB NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL,
    UNIQUE(job_id, track_id)
);
CREATE INDEX batch_edit_items_job_state_idx ON batch_edit_items(job_id, state);

-- +goose Down
DROP TABLE batch_edit_items;
