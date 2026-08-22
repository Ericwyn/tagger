-- +goose Up
CREATE TABLE libraries (
    id TEXT PRIMARY KEY,
    root_path TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    summary_json BLOB NOT NULL,
    report_json BLOB NOT NULL,
    scan_token TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE tracks (
    id TEXT PRIMARY KEY,
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
    UNIQUE(library_id, relative_path)
);

CREATE INDEX tracks_library_path_idx ON tracks(library_id, relative_path);
CREATE INDEX tracks_library_folder_idx ON tracks(library_id, folder_id);
CREATE INDEX tracks_library_health_idx ON tracks(library_id, health);
CREATE INDEX tracks_library_format_idx ON tracks(library_id, format);

CREATE TABLE revisions (
    id TEXT PRIMARY KEY,
    library_id TEXT NOT NULL,
    track_id TEXT NOT NULL,
    track_title TEXT NOT NULL,
    file_name TEXT NOT NULL,
    action TEXT NOT NULL,
    source TEXT NOT NULL,
    base_revision TEXT NOT NULL,
    result_revision TEXT NOT NULL,
    fields_json BLOB NOT NULL,
    diff_json BLOB NOT NULL,
    before_tags_json BLOB NOT NULL,
    after_tags_json BLOB NOT NULL,
    cover_tone TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX revisions_created_idx ON revisions(created_at DESC);
CREATE INDEX revisions_track_created_idx ON revisions(track_id, created_at DESC);

-- +goose Down
DROP TABLE revisions;
DROP TABLE tracks;
DROP TABLE libraries;
