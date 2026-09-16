package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ericwyn/tagger/internal/domain"
)

const (
	historyRetentionSetting = "history_retention"
	writeHistorySetting     = "write_history"
	batchTrackLimitSetting  = "batch_track_limit"
	libraryRootSetting      = "library_root"
	defaultHistoryRetention = 20
	defaultWriteHistory     = true
)

func validateBatchTrackLimit(value int) error {
	if value < domain.MinBatchTrackLimit || value > domain.MaxBatchTrackLimit {
		return fmt.Errorf("批量曲目上限必须在 %d 到 %d 之间", domain.MinBatchTrackLimit, domain.MaxBatchTrackLimit)
	}
	return nil
}

func (s *Store) loadBatchTrackLimit(ctx context.Context) (int, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key=?`, batchTrackLimitSetting).Scan(&raw)
	if err == sql.ErrNoRows {
		return domain.DefaultBatchTrackLimit, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load batch track limit: %w", err)
	}
	value, err := strconv.Atoi(raw)
	if err != nil || validateBatchTrackLimit(value) != nil {
		return domain.DefaultBatchTrackLimit, nil
	}
	return value, nil
}

// BatchTrackLimit returns the maximum number of tracks accepted by one batch
// resolve, match, edit, or snapshot operation.
func (s *Store) BatchTrackLimit(_ context.Context) int {
	value := int(s.batchTrackLimit.Load())
	if value == 0 {
		return domain.DefaultBatchTrackLimit
	}
	return value
}

func (s *Store) SetBatchTrackLimit(ctx context.Context, value int) error {
	if err := validateBatchTrackLimit(value); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO system_settings(key, value, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		batchTrackLimitSetting, strconv.Itoa(value), formatTime(s.now().UTC()))
	if err != nil {
		return fmt.Errorf("save batch track limit: %w", err)
	}
	s.batchTrackLimit.Store(int64(value))
	return nil
}

func (s *Store) LibraryRoot(ctx context.Context) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key=?`, libraryRootSetting).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load library root: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false, nil
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", false, fmt.Errorf("resolve library root: %w", err)
	}
	return root, true, nil
}

func (s *Store) SetLibraryRoot(ctx context.Context, root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return fmt.Errorf("library root cannot be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve library root: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO system_settings(key, value, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		libraryRootSetting, abs, formatTime(s.now().UTC()))
	if err != nil {
		return fmt.Errorf("save library root: %w", err)
	}
	return nil
}

// ClearLibraryRoot removes the active-root pointer while keeping previously
// indexed library summaries available for inspection and re-selection. It is
// used when a persisted root is no longer present on disk and the process
// must start in the explicit "no active library" state.
func (s *Store) ClearLibraryRoot(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM system_settings WHERE key=?`, libraryRootSetting); err != nil {
		return fmt.Errorf("clear library root: %w", err)
	}
	return nil
}

var supportedHistoryRetention = map[int]struct{}{3: {}, 5: {}, 10: {}, 20: {}}

func validateHistoryRetention(value int) error {
	if _, ok := supportedHistoryRetention[value]; !ok {
		return fmt.Errorf("历史保留次数必须为 3、5、10 或 20")
	}
	return nil
}

func (s *Store) loadHistoryRetention(ctx context.Context) (int, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key=?`, historyRetentionSetting).Scan(&raw)
	if err == sql.ErrNoRows {
		return defaultHistoryRetention, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load history retention: %w", err)
	}
	value, err := strconv.Atoi(raw)
	if err != nil || validateHistoryRetention(value) != nil {
		return defaultHistoryRetention, nil
	}
	return value, nil
}

// HistoryRetention returns the per-track audit revision limit. The value is
// cached for the write path so normal metadata writes do not query settings.
func (s *Store) HistoryRetention(_ context.Context) int {
	value := int(s.historyRetention.Load())
	if value == 0 {
		return defaultHistoryRetention
	}
	return value
}

func (s *Store) loadWriteHistory(ctx context.Context) (bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key=?`, writeHistorySetting).Scan(&raw)
	if err == sql.ErrNoRows {
		return defaultWriteHistory, nil
	}
	if err != nil {
		return false, fmt.Errorf("load write history: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return defaultWriteHistory, nil
	}
}

// WriteHistory reports whether successful file mutations should create audit
// revisions. It is cached so write paths do not query SQLite for every file.
func (s *Store) WriteHistory(_ context.Context) bool {
	return s.writeHistory.Load()
}

// SetWriteHistory persists the audit switch and updates the in-memory write
// path immediately. Existing revisions are intentionally retained when the
// switch is turned off; disabling future history must not destroy recovery
// data the user already created.
func (s *Store) SetWriteHistory(ctx context.Context, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO system_settings(key, value, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		writeHistorySetting, strconv.FormatBool(enabled), formatTime(s.now().UTC()))
	if err != nil {
		return fmt.Errorf("save write history: %w", err)
	}
	s.writeHistory.Store(enabled)
	return nil
}

// SetHistoryRetention persists the limit and immediately removes older
// revisions. Artwork blobs are content-addressed and intentionally retained;
// a later garbage-collection pass can safely remove unreferenced blobs.
func (s *Store) SetHistoryRetention(ctx context.Context, value int) error {
	if err := validateHistoryRetention(value); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO system_settings(key, value, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		historyRetentionSetting, strconv.Itoa(value), formatTime(s.now().UTC())); err != nil {
		return fmt.Errorf("save history retention: %w", err)
	}
	if err := pruneRevisionsTx(ctx, tx, value, ""); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit history retention: %w", err)
	}
	s.historyRetention.Store(int64(value))
	return nil
}

func pruneRevisionsTx(ctx context.Context, tx *sql.Tx, limit int, trackID string) error {
	var rows *sql.Rows
	var err error
	if trackID != "" {
		rows, err = tx.QueryContext(ctx, `
			SELECT id FROM revisions WHERE track_id=?
			ORDER BY created_at DESC, id DESC LIMIT -1 OFFSET ?`, trackID, limit)
	} else {
		rows, err = tx.QueryContext(ctx, `
			SELECT id FROM revisions WHERE id IN (
				SELECT id FROM revisions r
				WHERE (SELECT COUNT(*) FROM revisions newer WHERE newer.track_id=r.track_id
					AND (newer.created_at > r.created_at OR (newer.created_at = r.created_at AND newer.id >= r.id))) > ?
			)`, limit)
	}
	if err != nil {
		return fmt.Errorf("find old revisions: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan old revision: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read old revisions: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM revisions WHERE id=?`, id); err != nil {
			return fmt.Errorf("delete old revision: %w", err)
		}
	}
	return nil
}

func (s *Store) pruneTrackRevisions(ctx context.Context, tx *sql.Tx, trackID string) error {
	if trackID == "" {
		return nil
	}
	return pruneRevisionsTx(ctx, tx, s.HistoryRetention(ctx), trackID)
}
