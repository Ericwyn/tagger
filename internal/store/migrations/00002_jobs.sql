-- +goose Up
CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    state TEXT NOT NULL,
    library_id TEXT NOT NULL,
    title TEXT NOT NULL,
    detail TEXT NOT NULL,
    processed INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    succeeded INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    error_text TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE INDEX jobs_state_created_idx ON jobs(state, created_at);
CREATE INDEX jobs_updated_idx ON jobs(updated_at DESC);

-- +goose Down
DROP TABLE jobs;
