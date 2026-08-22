-- +goose Up
ALTER TABLE revisions ADD COLUMN before_sidecar_json BLOB NOT NULL DEFAULT '{}';
ALTER TABLE revisions ADD COLUMN after_sidecar_json BLOB NOT NULL DEFAULT '{}';

-- +goose Down
-- SQLite versions supported by the embedded driver do not consistently
-- support DROP COLUMN across existing databases. The columns are additive
-- audit data and are intentionally retained on downgrade.
