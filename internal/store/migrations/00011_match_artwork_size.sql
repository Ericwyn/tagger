-- +goose Up
ALTER TABLE match_items ADD COLUMN review_artwork_max_size INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE match_items DROP COLUMN review_artwork_max_size;
