-- +goose Up
-- Filesystem presence and cheap fingerprints are kept independently from the
-- parsed track payload. This lets live browsing expose drafts without treating
-- cached metadata as the source of truth for file existence.
CREATE TABLE library_files (
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL,
    track_id TEXT NOT NULL,
    folder_id TEXT NOT NULL,
    format TEXT NOT NULL,
    file_size INTEGER NOT NULL DEFAULT 0,
    file_mtime_ns INTEGER NOT NULL DEFAULT 0,
    sidecar_size INTEGER NOT NULL DEFAULT 0,
    sidecar_mtime_ns INTEGER NOT NULL DEFAULT 0,
    writable INTEGER NOT NULL DEFAULT 0,
    present INTEGER NOT NULL DEFAULT 1,
    sync_state TEXT NOT NULL DEFAULT 'indexed',
    parse_error TEXT NOT NULL DEFAULT '',
    missing_since TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    PRIMARY KEY(library_id, relative_path)
);

INSERT INTO library_files(
    library_id, relative_path, track_id, folder_id, format,
    file_size, file_mtime_ns, sidecar_size, sidecar_mtime_ns,
    writable, present, sync_state, parse_error, missing_since, updated_at
)
SELECT library_id, relative_path, id, folder_id, format,
       file_size, file_mtime_ns, sidecar_size, sidecar_mtime_ns,
       CASE WHEN json_extract(payload_json, '$.writable') = 1 THEN 1 ELSE 0 END,
       CASE WHEN missing = 0 THEN 1 ELSE 0 END,
       'indexed', COALESCE(json_extract(payload_json, '$.parseError'), ''), missing_since, updated_at
FROM tracks;

CREATE INDEX library_files_library_folder_idx ON library_files(library_id, folder_id, present);
CREATE INDEX library_files_library_sync_idx ON library_files(library_id, sync_state, present);

-- +goose Down
DROP INDEX library_files_library_sync_idx;
DROP INDEX library_files_library_folder_idx;
DROP TABLE library_files;
