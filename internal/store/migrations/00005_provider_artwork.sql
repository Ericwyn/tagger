-- +goose Up
CREATE TABLE provider_artwork_refs (
    candidate_id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    artwork_url TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX provider_artwork_expiry_idx ON provider_artwork_refs(expires_at);

-- +goose Down
DROP TABLE provider_artwork_refs;
