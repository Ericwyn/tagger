-- +goose Up
CREATE TABLE system_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO system_settings(key, value, updated_at)
VALUES('history_retention', '20', CURRENT_TIMESTAMP);

-- +goose Down
DROP TABLE system_settings;
