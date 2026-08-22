-- +goose Up
CREATE TABLE artwork_blobs (
    hash TEXT PRIMARY KEY,
    mime TEXT NOT NULL,
    format TEXT NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    size INTEGER NOT NULL,
    data BLOB NOT NULL,
    created_at TEXT NOT NULL
);

ALTER TABLE revisions ADD COLUMN before_artwork_hash TEXT;
ALTER TABLE revisions ADD COLUMN after_artwork_hash TEXT;

CREATE INDEX revisions_before_artwork_idx ON revisions(before_artwork_hash);
CREATE INDEX revisions_after_artwork_idx ON revisions(after_artwork_hash);

-- +goose Down
DROP INDEX revisions_after_artwork_idx;
DROP INDEX revisions_before_artwork_idx;
DROP TABLE artwork_blobs;
