-- +goose Up
ALTER TABLE match_items ADD COLUMN review_fields_json BLOB;
ALTER TABLE match_items ADD COLUMN review_artwork INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE match_items DROP COLUMN review_artwork;
ALTER TABLE match_items DROP COLUMN review_fields_json;
