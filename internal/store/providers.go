package store

import (
	"context"
	"database/sql"
	"encoding/json"
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

func (s *Store) LoadArtworkReferences(ctx context.Context) ([]byte, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT candidate_id, provider_id, artwork_url, expires_at FROM provider_artwork_refs WHERE expires_at>?`, formatTime(s.now().UTC()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []struct {
		CandidateID string    `json:"candidateId"`
		ProviderID  string    `json:"providerId"`
		URL         string    `json:"url"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}{}
	for rows.Next() {
		var reference struct {
			CandidateID string
			ProviderID  string
			URL         string
			ExpiresAt   time.Time
		}
		var expiresAt string
		if err := rows.Scan(&reference.CandidateID, &reference.ProviderID, &reference.URL, &expiresAt); err != nil {
			return nil, err
		}
		reference.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
		if err != nil {
			return nil, err
		}
		result = append(result, struct {
			CandidateID string    `json:"candidateId"`
			ProviderID  string    `json:"providerId"`
			URL         string    `json:"url"`
			ExpiresAt   time.Time `json:"expiresAt"`
		}{CandidateID: reference.CandidateID, ProviderID: reference.ProviderID, URL: reference.URL, ExpiresAt: reference.ExpiresAt})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (s *Store) SaveArtworkReference(ctx context.Context, candidateID, providerID, artworkURL string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_artwork_refs(candidate_id, provider_id, artwork_url, expires_at, updated_at) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(candidate_id) DO UPDATE SET provider_id=excluded.provider_id, artwork_url=excluded.artwork_url, expires_at=excluded.expires_at, updated_at=excluded.updated_at`,
		candidateID, providerID, artworkURL, formatTime(expiresAt), formatTime(s.now().UTC()))
	return err
}

func (s *Store) DeleteExpiredArtworkReferences(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM provider_artwork_refs WHERE expires_at<=?`, formatTime(s.now().UTC()))
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

// LoadProviderConfigurations returns the persisted strategy-owned settings.
// Unknown provider IDs are intentionally retained so upgrading the binary
// does not silently discard settings for an adapter that is temporarily not
// registered.
func (s *Store) LoadProviderConfigurations(ctx context.Context) (map[string]map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider_id, config_json FROM provider_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]map[string]string)
	for rows.Next() {
		var id string
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		values := make(map[string]string)
		if len(payload) > 0 && string(payload) != "{}" {
			if err := json.Unmarshal(payload, &values); err != nil {
				return nil, fmt.Errorf("decode provider configuration %s: %w", id, err)
			}
		}
		result[id] = values
	}
	return result, rows.Err()
}

func (s *Store) SaveProviderConfiguration(ctx context.Context, providerID string, values map[string]string) error {
	if values == nil {
		values = map[string]string{}
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode provider configuration: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO provider_settings(provider_id, enabled, config_json, updated_at)
		VALUES(?, COALESCE((SELECT enabled FROM provider_settings WHERE provider_id=?), 1), ?, ?)
		ON CONFLICT(provider_id) DO UPDATE SET config_json=excluded.config_json, updated_at=excluded.updated_at`,
		providerID, providerID, payload, formatTime(s.now().UTC()))
	if err != nil {
		return fmt.Errorf("save provider configuration: %w", err)
	}
	return nil
}
