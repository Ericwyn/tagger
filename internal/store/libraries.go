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
