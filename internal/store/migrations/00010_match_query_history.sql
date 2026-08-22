-- +goose Up
CREATE TABLE match_query_history (
    id TEXT PRIMARY KEY,
    track_id TEXT NOT NULL,
    query_json BLOB NOT NULL DEFAULT '{}',
    provider_ids_json BLOB NOT NULL DEFAULT '[]',
    result_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE INDEX match_query_history_track_created_idx ON match_query_history(track_id, created_at DESC);

-- +goose Down
DROP TABLE match_query_history;
