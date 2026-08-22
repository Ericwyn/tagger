package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (s *Store) LoadProviderCache(ctx context.Context, key string) ([]byte, bool, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM provider_cache WHERE cache_key=? AND expires_at>?`, key, formatTime(s.now().UTC())).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return append([]byte(nil), payload...), true, nil
}

func (s *Store) SaveProviderCache(ctx context.Context, key, providerID string, payload []byte, ttl time.Duration) error {
	now := s.now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_cache(cache_key, provider_id, payload_json, expires_at, updated_at) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(cache_key) DO UPDATE SET payload_json=excluded.payload_json, expires_at=excluded.expires_at, updated_at=excluded.updated_at`,
		key, providerID, payload, formatTime(now.Add(ttl)), formatTime(now))
	if err != nil {
		return fmt.Errorf("save provider cache: %w", err)
	}
	return nil
}

func (s *Store) DeleteExpiredProviderCache(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM provider_cache WHERE expires_at<=?`, formatTime(s.now().UTC()))
	return err
}

func (s *Store) LoadProviderSettings(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider_id, enabled FROM provider_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var id string
		var enabled bool
		if err := rows.Scan(&id, &enabled); err != nil {
			return nil, err
		}
		result[id] = enabled
	}
	return result, rows.Err()
}

func (s *Store) SaveProviderEnabled(ctx context.Context, providerID string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_settings(provider_id, enabled, config_json, updated_at) VALUES(?, ?, '{}', ?)
		ON CONFLICT(provider_id) DO UPDATE SET enabled=excluded.enabled, updated_at=excluded.updated_at`,
		providerID, enabled, formatTime(s.now().UTC()))
	return err
}
