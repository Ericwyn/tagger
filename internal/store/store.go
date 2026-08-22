package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
	"github.com/ericwyn/tagger/internal/scanner"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var migrationMu sync.Mutex

type Store struct {
	db   *sql.DB
	path string
	now  func() time.Time
}

func Open(ctx context.Context, path string) (*Store, error) {
	path, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	values := url.Values{}
	values.Add("_pragma", "foreign_keys(1)")
	values.Add("_pragma", "busy_timeout(5000)")
	values.Add("_pragma", "journal_mode(WAL)")
	values.Add("_pragma", "synchronous(NORMAL)")
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: values.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, path: path, now: time.Now}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()
	goose.SetBaseFS(migrationFiles)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations", goose.WithNoColor(true)); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Path() string { return s.path }

func (s *Store) SaveScan(ctx context.Context, root string, result scanner.Result) error {
	summaryJSON, err := json.Marshal(result.Library)
	if err != nil {
		return fmt.Errorf("encode library summary: %w", err)
	}
	reportJSON, err := json.Marshal(result.Report)
	if err != nil {
		return fmt.Errorf("encode scan report: %w", err)
	}
	now := s.now().UTC()
	token := fmt.Sprintf("scan-%d", now.UnixNano())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
        INSERT INTO libraries(id, root_path, name, summary_json, report_json, scan_token, updated_at)
        VALUES(?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            root_path=excluded.root_path,
            name=excluded.name,
            summary_json=excluded.summary_json,
            report_json=excluded.report_json,
            scan_token=excluded.scan_token,
            updated_at=excluded.updated_at`,
		result.Library.ID, root, result.Library.Name, summaryJSON, reportJSON, token, now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert library: %w", err)
	}
	statement, err := tx.PrepareContext(ctx, `
        INSERT INTO tracks(
            id, library_id, relative_path, folder_id, format, title, artists_text,
            album, health, revision, payload_json, scan_token, updated_at
        ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            library_id=excluded.library_id,
            relative_path=excluded.relative_path,
            folder_id=excluded.folder_id,
            format=excluded.format,
            title=excluded.title,
            artists_text=excluded.artists_text,
            album=excluded.album,
            health=excluded.health,
            revision=excluded.revision,
            payload_json=excluded.payload_json,
            scan_token=excluded.scan_token,
            updated_at=excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("prepare track upsert: %w", err)
	}
	defer statement.Close()
	for _, track := range result.Tracks {
		payload, err := json.Marshal(track)
		if err != nil {
			return fmt.Errorf("encode track %s: %w", track.ID, err)
		}
		if _, err := statement.ExecContext(ctx,
			track.ID, result.Library.ID, track.RelativePath, track.FolderID, track.Format,
			track.Title, strings.Join(track.Artists, "\x1f"), track.Album, track.Health,
			track.Revision, payload, token, now.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("upsert track %s: %w", track.ID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE library_id = ? AND scan_token <> ?`, result.Library.ID, token); err != nil {
		return fmt.Errorf("remove stale tracks: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit scan: %w", err)
	}
	return nil
}

func (s *Store) LoadScan(ctx context.Context, root string) (scanner.Result, bool, error) {
	var libraryID string
	var summaryJSON, reportJSON []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, summary_json, report_json FROM libraries WHERE root_path = ?`, root).
		Scan(&libraryID, &summaryJSON, &reportJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return scanner.Result{}, false, nil
	}
	if err != nil {
		return scanner.Result{}, false, fmt.Errorf("load library: %w", err)
	}
	var result scanner.Result
	if err := json.Unmarshal(summaryJSON, &result.Library); err != nil {
		return scanner.Result{}, false, fmt.Errorf("decode library summary: %w", err)
	}
	if err := json.Unmarshal(reportJSON, &result.Report); err != nil {
		return scanner.Result{}, false, fmt.Errorf("decode scan report: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM tracks WHERE library_id = ? ORDER BY relative_path`, libraryID)
	if err != nil {
		return scanner.Result{}, false, fmt.Errorf("load tracks: %w", err)
	}
	defer rows.Close()
	result.Tracks = make([]domain.Track, 0, result.Library.TrackCount)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return scanner.Result{}, false, err
		}
		var track domain.Track
		if err := json.Unmarshal(payload, &track); err != nil {
			return scanner.Result{}, false, fmt.Errorf("decode track: %w", err)
		}
		result.Tracks = append(result.Tracks, track)
	}
	if err := rows.Err(); err != nil {
		return scanner.Result{}, false, err
	}
	return result, true, nil
}

func (s *Store) CreateRevision(ctx context.Context, revision domain.Revision) (domain.Revision, error) {
	if revision.ID == "" {
		revision.ID = newRevisionID(s.now())
	}
	if revision.CreatedAt.IsZero() {
		revision.CreatedAt = s.now().UTC()
	}
	if revision.Fields == nil {
		revision.Fields = make([]string, 0, len(revision.Diff))
		for _, diff := range revision.Diff {
			revision.Fields = append(revision.Fields, diff.Field)
		}
	}
	fieldsJSON, err := json.Marshal(revision.Fields)
	if err != nil {
		return domain.Revision{}, err
	}
	diffJSON, err := json.Marshal(revision.Diff)
	if err != nil {
		return domain.Revision{}, err
	}
	beforeJSON, err := json.Marshal(nonNilTags(revision.BeforeTags))
	if err != nil {
		return domain.Revision{}, err
	}
	afterJSON, err := json.Marshal(nonNilTags(revision.AfterTags))
	if err != nil {
		return domain.Revision{}, err
	}
	_, err = s.db.ExecContext(ctx, `
        INSERT INTO revisions(
            id, library_id, track_id, track_title, file_name, action, source,
            base_revision, result_revision, fields_json, diff_json,
            before_tags_json, after_tags_json, cover_tone, created_at
        ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revision.ID, revision.LibraryID, revision.TrackID, revision.TrackTitle,
		revision.FileName, revision.Action, revision.Source, revision.BaseRevision,
		revision.ResultRevision, fieldsJSON, diffJSON, beforeJSON, afterJSON,
		revision.CoverTone, revision.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return domain.Revision{}, fmt.Errorf("insert revision: %w", err)
	}
	return revision, nil
}

func (s *Store) ListRevisions(ctx context.Context, limit int) ([]domain.Revision, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, library_id, track_id, track_title, file_name, action, source,
               base_revision, result_revision, fields_json, diff_json,
               before_tags_json, after_tags_json, cover_tone, created_at
        FROM revisions ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Revision, 0)
	for rows.Next() {
		revision, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, revision)
	}
	return result, rows.Err()
}

func (s *Store) Revision(ctx context.Context, id string) (domain.Revision, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, library_id, track_id, track_title, file_name, action, source,
               base_revision, result_revision, fields_json, diff_json,
               before_tags_json, after_tags_json, cover_tone, created_at
        FROM revisions WHERE id = ?`, id)
	return scanRevision(row)
}

type rowScanner interface{ Scan(...any) error }

func scanRevision(row rowScanner) (domain.Revision, error) {
	var revision domain.Revision
	var fieldsJSON, diffJSON, beforeJSON, afterJSON []byte
	var createdAt string
	err := row.Scan(
		&revision.ID, &revision.LibraryID, &revision.TrackID, &revision.TrackTitle,
		&revision.FileName, &revision.Action, &revision.Source, &revision.BaseRevision,
		&revision.ResultRevision, &fieldsJSON, &diffJSON, &beforeJSON, &afterJSON,
		&revision.CoverTone, &createdAt,
	)
	if err != nil {
		return domain.Revision{}, err
	}
	if err := json.Unmarshal(fieldsJSON, &revision.Fields); err != nil {
		return domain.Revision{}, err
	}
	if err := json.Unmarshal(diffJSON, &revision.Diff); err != nil {
		return domain.Revision{}, err
	}
	if err := json.Unmarshal(beforeJSON, &revision.BeforeTags); err != nil {
		return domain.Revision{}, err
	}
	if err := json.Unmarshal(afterJSON, &revision.AfterTags); err != nil {
		return domain.Revision{}, err
	}
	revision.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.Revision{}, err
	}
	return revision, nil
}

func nonNilTags(tags map[string][]string) map[string][]string {
	if tags == nil {
		return map[string][]string{}
	}
	return tags
}

func newRevisionID(now time.Time) string {
	random := make([]byte, 6)
	_, _ = rand.Read(random)
	return fmt.Sprintf("revlog-%d-%s", now.UTC().UnixMilli(), hex.EncodeToString(random))
}
