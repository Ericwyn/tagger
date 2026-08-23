-- +goose Up
-- Track IDs were historically globally unique even though they are derived
-- from relative paths. Scope the key by library so identical paths in two
-- libraries cannot overwrite one another.
ALTER TABLE tracks RENAME TO tracks_legacy;

CREATE TABLE tracks (
    id TEXT NOT NULL,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL,
    folder_id TEXT NOT NULL,
    format TEXT NOT NULL,
    title TEXT NOT NULL,
    artists_text TEXT NOT NULL,
    album TEXT NOT NULL,
    health TEXT NOT NULL,
    revision TEXT NOT NULL,
    payload_json BLOB NOT NULL,
    scan_token TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    file_size INTEGER NOT NULL DEFAULT 0,
    file_mtime_ns INTEGER NOT NULL DEFAULT 0,
    sidecar_size INTEGER NOT NULL DEFAULT 0,
    sidecar_mtime_ns INTEGER NOT NULL DEFAULT 0,
    missing INTEGER NOT NULL DEFAULT 0,
    missing_since TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(library_id, id),
    UNIQUE(library_id, relative_path)
);

INSERT INTO tracks(
    id, library_id, relative_path, folder_id, format, title, artists_text,
    album, health, revision, payload_json, scan_token, updated_at
)
SELECT id, library_id, relative_path, folder_id, format, title, artists_text,
       album, health, revision, payload_json, scan_token, updated_at
FROM tracks_legacy;

DROP TABLE tracks_legacy;

CREATE INDEX tracks_library_path_idx ON tracks(library_id, relative_path);
CREATE INDEX tracks_library_folder_idx ON tracks(library_id, folder_id);
CREATE INDEX tracks_library_health_idx ON tracks(library_id, health);
CREATE INDEX tracks_library_format_idx ON tracks(library_id, format);
CREATE INDEX tracks_library_missing_idx ON tracks(library_id, missing);

ALTER TABLE match_query_history ADD COLUMN library_id TEXT NOT NULL DEFAULT '';
UPDATE match_query_history
SET library_id = COALESCE((
    SELECT library_id FROM tracks WHERE tracks.id = match_query_history.track_id LIMIT 1
), '');
CREATE INDEX match_query_history_library_track_idx ON match_query_history(library_id, track_id, created_at DESC);

-- +goose Down
DROP INDEX match_query_history_library_track_idx;
DROP INDEX tracks_library_missing_idx;
DROP TABLE tracks;
ALTER TABLE match_query_history DROP COLUMN library_id;
-- SQLite versions used by older installations may not support restoring the
-- previous table shape during a down migration; goose treats this migration
-- as forward-only in those environments.
