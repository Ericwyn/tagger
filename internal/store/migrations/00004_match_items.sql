-- +goose Up
ALTER TABLE jobs ADD COLUMN payload_json BLOB NOT NULL DEFAULT '{}';

CREATE TABLE match_items (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    track_id TEXT NOT NULL,
    state TEXT NOT NULL,
    candidates_json BLOB NOT NULL DEFAULT '[]',
    selected_candidate_id TEXT NOT NULL DEFAULT '',
    error_text TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    UNIQUE(job_id, track_id)
);
CREATE INDEX match_items_job_idx ON match_items(job_id, state);

-- +goose Down
DROP TABLE match_items;
-- SQLite cannot drop an ALTERed column on all supported versions; the jobs
-- payload column is intentionally retained during down migrations.
