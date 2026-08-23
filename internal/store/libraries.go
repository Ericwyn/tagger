package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ericwyn/tagger/internal/domain"
)

// ListLibraries returns every successfully indexed root. The active flag is
// derived from the single-user runtime setting rather than persisted in the
// library row, so switching roots remains atomic and easy to recover.
func (s *Store) ListLibraries(ctx context.Context, activeRoot string) ([]domain.LibrarySummary, error) {
	activeRoot = normalizeRoot(activeRoot)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, root_path, summary_json
		FROM libraries
		ORDER BY updated_at DESC, root_path ASC`)
	if err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}
	defer rows.Close()
	result := make([]domain.LibrarySummary, 0)
	for rows.Next() {
		var id, root string
		var summaryJSON []byte
		if err := rows.Scan(&id, &root, &summaryJSON); err != nil {
			return nil, fmt.Errorf("scan library row: %w", err)
		}
		var summary domain.LibrarySummary
		if err := json.Unmarshal(summaryJSON, &summary); err != nil {
			return nil, fmt.Errorf("decode library %s: %w", id, err)
		}
		if summary.ID == "" {
			summary.ID = id
		}
		if summary.RootPath == "" {
			summary.RootPath = root
		}
		if summary.RootLabel == "" {
			summary.RootLabel = filepath.Base(root)
		}
		summary.Active = normalizeRoot(root) == activeRoot
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate libraries: %w", err)
	}
	// Keep the currently selected root first even if an older database has
	// timestamps with low precision.
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Active != result[j].Active {
			return result[i].Active
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// LibraryByID returns the persisted summary and its canonical root path.
func (s *Store) LibraryByID(ctx context.Context, id string) (domain.LibrarySummary, string, bool, error) {
	var root string
	var summaryJSON []byte
	err := s.db.QueryRowContext(ctx, `SELECT root_path, summary_json FROM libraries WHERE id = ?`, strings.TrimSpace(id)).Scan(&root, &summaryJSON)
	if err == sql.ErrNoRows {
		return domain.LibrarySummary{}, "", false, nil
	}
	if err != nil {
		return domain.LibrarySummary{}, "", false, fmt.Errorf("load library %s: %w", id, err)
	}
	var summary domain.LibrarySummary
	if err := json.Unmarshal(summaryJSON, &summary); err != nil {
		return domain.LibrarySummary{}, "", false, fmt.Errorf("decode library %s: %w", id, err)
	}
	if summary.ID == "" {
		summary.ID = id
	}
	if summary.RootPath == "" {
		summary.RootPath = root
	}
	if summary.RootLabel == "" {
		summary.RootLabel = filepath.Base(root)
	}
	return summary, normalizeRoot(root), true, nil
}

// FirstLibraryRoot supports upgrading installations created before the
// explicit active-root setting was introduced.
func (s *Store) FirstLibraryRoot(ctx context.Context) (string, bool, error) {
	var root string
	err := s.db.QueryRowContext(ctx, `SELECT root_path FROM libraries ORDER BY updated_at DESC, root_path ASC LIMIT 1`).Scan(&root)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load first library root: %w", err)
	}
	return normalizeRoot(root), true, nil
}

// DeleteLibraryByRoot is used only for the private empty-root placeholder
// created while the application is waiting for the first user-selected path.
func (s *Store) DeleteLibraryByRoot(ctx context.Context, root string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM libraries WHERE root_path = ?`, normalizeRoot(root))
	if err != nil {
		return fmt.Errorf("delete library placeholder: %w", err)
	}
	return nil
}

// DeleteLibrary removes only Tagger-owned database state. It intentionally
// does not inspect, truncate, or delete anything below the library root.
func (s *Store) DeleteLibrary(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("library id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var root string
	if err := tx.QueryRowContext(ctx, `SELECT root_path FROM libraries WHERE id=?`, id).Scan(&root); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("load library for deletion: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM match_query_history WHERE library_id=? OR (library_id='' AND track_id IN (SELECT id FROM tracks WHERE library_id=?))`, id, id); err != nil {
		return fmt.Errorf("delete library query history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM revisions WHERE library_id=?`, id); err != nil {
		return fmt.Errorf("delete library revisions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM match_items WHERE job_id IN (SELECT id FROM jobs WHERE library_id=?)`, id); err != nil {
		return fmt.Errorf("delete library match items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM batch_edit_items WHERE job_id IN (SELECT id FROM jobs WHERE library_id=?)`, id); err != nil {
		return fmt.Errorf("delete library batch items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE library_id=?`, id); err != nil {
		return fmt.Errorf("delete library jobs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE library_id=?`, id); err != nil {
		return fmt.Errorf("delete library tracks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM libraries WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete library row: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM system_settings WHERE key=? AND value=?`, libraryRootSetting, normalizeRoot(root)); err != nil {
		return fmt.Errorf("clear deleted library root: %w", err)
	}
	// Artwork blobs are content-addressed and shared by revisions. Remove only
	// blobs no longer referenced by any remaining revision.
	if _, err := tx.ExecContext(ctx, `DELETE FROM artwork_blobs WHERE hash NOT IN (
		SELECT before_artwork_hash FROM revisions WHERE before_artwork_hash IS NOT NULL AND before_artwork_hash <> ''
		UNION SELECT after_artwork_hash FROM revisions WHERE after_artwork_hash IS NOT NULL AND after_artwork_hash <> ''
	)`); err != nil {
		return fmt.Errorf("gc artwork blobs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library deletion: %w", err)
	}
	return nil
}

func (s *Store) PurgeMissing(ctx context.Context, libraryID string) (int, error) {
	libraryID = strings.TrimSpace(libraryID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tracks WHERE library_id=? AND missing=1`, libraryID)
	if err != nil {
		return 0, fmt.Errorf("find missing tracks: %w", err)
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM match_query_history WHERE (library_id=? OR library_id='') AND track_id=?`, libraryID, id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM revisions WHERE library_id=? AND track_id=?`, libraryID, id); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE library_id=? AND missing=1`, libraryID); err != nil {
		return 0, fmt.Errorf("purge missing tracks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM artwork_blobs WHERE hash NOT IN (
		SELECT before_artwork_hash FROM revisions WHERE before_artwork_hash IS NOT NULL AND before_artwork_hash <> ''
		UNION SELECT after_artwork_hash FROM revisions WHERE after_artwork_hash IS NOT NULL AND after_artwork_hash <> ''
	)`); err != nil {
		return 0, fmt.Errorf("gc missing-track artwork blobs: %w", err)
	}
	var summaryJSON []byte
	if err := tx.QueryRowContext(ctx, `SELECT summary_json FROM libraries WHERE id=?`, libraryID).Scan(&summaryJSON); err == nil {
		var summary domain.LibrarySummary
		if json.Unmarshal(summaryJSON, &summary) == nil {
			var trackCount, folderCount int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks WHERE library_id=?`, libraryID).Scan(&trackCount)
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT folder_id) FROM tracks WHERE library_id=?`, libraryID).Scan(&folderCount)
			folderNames := make(map[string]string, len(summary.Folders))
			for _, folder := range summary.Folders {
				folderNames[folder.ID] = folder.Name
			}
			folderRows, queryErr := tx.QueryContext(ctx, `SELECT folder_id, COUNT(*) FROM tracks WHERE library_id=? GROUP BY folder_id`, libraryID)
			if queryErr == nil {
				folders := make([]domain.FolderNode, 0, folderCount)
				for folderRows.Next() {
					var id string
					var count int
					if scanErr := folderRows.Scan(&id, &count); scanErr != nil {
						continue
					}
					name := folderNames[id]
					if name == "" {
						name = id
					}
					folders = append(folders, domain.FolderNode{ID: id, Name: name, Count: count})
				}
				_ = folderRows.Close()
				summary.Folders = folders
			}
			summary.TrackCount = trackCount
			summary.FolderCount = folderCount
			if encoded, marshalErr := json.Marshal(summary); marshalErr == nil {
				if _, updateErr := tx.ExecContext(ctx, `UPDATE libraries SET summary_json=? WHERE id=?`, encoded, libraryID); updateErr != nil {
					return 0, updateErr
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func normalizeRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	return filepath.Clean(abs)
}
