-- +goose Up
CREATE TABLE provider_cache (
    cache_key TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    payload_json BLOB NOT NULL,
    expires_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX provider_cache_expiry_idx ON provider_cache(expires_at);

CREATE TABLE provider_settings (
    provider_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL,
    config_json BLOB NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE provider_settings;
DROP TABLE provider_cache;
