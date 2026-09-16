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
	"sync/atomic"
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
	db               *sql.DB
	path             string
	now              func() time.Time
	historyRetention atomic.Int64
	writeHistory     atomic.Bool
	batchTrackLimit  atomic.Int64
	secretBox        *secretBox
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
	box, err := openSecretBox(filepath.Dir(path))
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &Store{db: db, path: path, now: time.Now, secretBox: box}
	retention, err := store.loadHistoryRetention(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	store.historyRetention.Store(int64(retention))
	writeHistory, err := store.loadWriteHistory(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	store.writeHistory.Store(writeHistory)
	batchTrackLimit, err := store.loadBatchTrackLimit(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	store.batchTrackLimit.Store(int64(batchTrackLimit))
	return store, nil
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
            album, health, revision, payload_json, scan_token, updated_at,
            file_size, file_mtime_ns, sidecar_size, sidecar_mtime_ns, missing, missing_since
        ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(library_id, id) DO UPDATE SET
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
            updated_at=excluded.updated_at,
            file_size=excluded.file_size,
            file_mtime_ns=excluded.file_mtime_ns,
            sidecar_size=excluded.sidecar_size,
            sidecar_mtime_ns=excluded.sidecar_mtime_ns,
            missing=excluded.missing,
            missing_since=excluded.missing_since`)
	if err != nil {
		return fmt.Errorf("prepare track upsert: %w", err)
	}
	defer statement.Close()
	fileStatement, err := prepareLibraryFileUpsert(ctx, tx)
	if err != nil {
		return err
	}
	defer fileStatement.Close()
	for _, track := range result.Tracks {
		if track.SyncState == "" {
			track.SyncState = domain.SyncIndexed
		}
		payload, err := json.Marshal(track)
		if err != nil {
			return fmt.Errorf("encode track %s: %w", track.ID, err)
		}
		if _, err := statement.ExecContext(ctx,
			track.ID, result.Library.ID, track.RelativePath, track.FolderID, track.Format,
			track.Title, strings.Join(track.Artists, "\x1f"), track.Album, track.Health,
			track.Revision, payload, token, now.Format(time.RFC3339Nano),
			track.FileFingerprint.SizeBytes, track.FileFingerprint.ModifiedUnixNano,
			track.FileFingerprint.SidecarSize, track.FileFingerprint.SidecarUnixNano,
			boolToInt(track.Missing), track.MissingSince); err != nil {
			return fmt.Errorf("upsert track %s: %w", track.ID, err)
		}
		if err := upsertLibraryFile(fileStatement, result.Library.ID, track, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE library_id = ? AND scan_token <> ?`, result.Library.ID, token); err != nil {
		return fmt.Errorf("remove stale tracks: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM library_files WHERE library_id=? AND relative_path NOT IN (SELECT relative_path FROM tracks WHERE library_id=?)`, result.Library.ID, result.Library.ID); err != nil {
		return fmt.Errorf("remove stale library files: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit scan: %w", err)
	}
	return nil
}

// SaveScanDelta updates only changed tracks and the library summary. It is
// deliberately separate from SaveScan so quick scans never rewrite every row
// in a large library.
func (s *Store) SaveScanDelta(ctx context.Context, root string, result scanner.Result, changed []domain.Track) error {
	return s.saveTrackDelta(ctx, root, result, changed)
}

func (s *Store) SaveTrackUpdates(ctx context.Context, root string, result scanner.Result, tracks []domain.Track) error {
	return s.saveTrackDelta(ctx, root, result, tracks)
}

func (s *Store) saveTrackDelta(ctx context.Context, root string, result scanner.Result, tracks []domain.Track) error {
	summaryJSON, err := json.Marshal(result.Library)
	if err != nil {
		return fmt.Errorf("encode library summary: %w", err)
	}
	reportJSON, err := json.Marshal(result.Report)
	if err != nil {
		return fmt.Errorf("encode scan report: %w", err)
	}
	now := s.now().UTC()
	token := fmt.Sprintf("delta-%d", now.UnixNano())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO libraries(id, root_path, name, summary_json, report_json, scan_token, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET root_path=excluded.root_path, name=excluded.name,
		 summary_json=excluded.summary_json, report_json=excluded.report_json,
		 scan_token=excluded.scan_token, updated_at=excluded.updated_at`,
		result.Library.ID, root, result.Library.Name, summaryJSON, reportJSON, token, now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert library delta: %w", err)
	}
	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO tracks(
			id, library_id, relative_path, folder_id, format, title, artists_text,
			album, health, revision, payload_json, scan_token, updated_at,
			file_size, file_mtime_ns, sidecar_size, sidecar_mtime_ns, missing, missing_since
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(library_id, id) DO UPDATE SET relative_path=excluded.relative_path,
		 folder_id=excluded.folder_id, format=excluded.format, title=excluded.title,
		 artists_text=excluded.artists_text, album=excluded.album, health=excluded.health,
		 revision=excluded.revision, payload_json=excluded.payload_json,
		 scan_token=excluded.scan_token, updated_at=excluded.updated_at,
		 file_size=excluded.file_size, file_mtime_ns=excluded.file_mtime_ns,
		 sidecar_size=excluded.sidecar_size, sidecar_mtime_ns=excluded.sidecar_mtime_ns,
		 missing=excluded.missing, missing_since=excluded.missing_since`)
	if err != nil {
		return fmt.Errorf("prepare track delta upsert: %w", err)
	}
	defer statement.Close()
	fileStatement, err := prepareLibraryFileUpsert(ctx, tx)
	if err != nil {
		return err
	}
	defer fileStatement.Close()
	for _, track := range tracks {
		if track.SyncState == "" {
			track.SyncState = domain.SyncIndexed
		}
		payload, marshalErr := json.Marshal(track)
		if marshalErr != nil {
			return fmt.Errorf("encode track %s: %w", track.ID, marshalErr)
		}
		if _, execErr := statement.ExecContext(ctx,
			track.ID, result.Library.ID, track.RelativePath, track.FolderID, track.Format,
			track.Title, strings.Join(track.Artists, "\x1f"), track.Album, track.Health,
			track.Revision, payload, token, now.Format(time.RFC3339Nano),
			track.FileFingerprint.SizeBytes, track.FileFingerprint.ModifiedUnixNano,
			track.FileFingerprint.SidecarSize, track.FileFingerprint.SidecarUnixNano,
			boolToInt(track.Missing), track.MissingSince); execErr != nil {
			return fmt.Errorf("upsert track delta %s: %w", track.ID, execErr)
		}
		if execErr := upsertLibraryFile(fileStatement, result.Library.ID, track, now); execErr != nil {
			return execErr
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit track delta: %w", err)
	}
	return nil
}

func prepareLibraryFileUpsert(ctx context.Context, tx *sql.Tx) (*sql.Stmt, error) {
	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO library_files(
			library_id, relative_path, track_id, folder_id, format,
			file_size, file_mtime_ns, sidecar_size, sidecar_mtime_ns,
			writable, present, sync_state, parse_error, missing_since, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(library_id, relative_path) DO UPDATE SET
			track_id=excluded.track_id, folder_id=excluded.folder_id, format=excluded.format,
			file_size=excluded.file_size, file_mtime_ns=excluded.file_mtime_ns,
			sidecar_size=excluded.sidecar_size, sidecar_mtime_ns=excluded.sidecar_mtime_ns,
			writable=excluded.writable, present=excluded.present, sync_state=excluded.sync_state,
			parse_error=excluded.parse_error, missing_since=excluded.missing_since,
			updated_at=excluded.updated_at`)
	if err != nil {
		return nil, fmt.Errorf("prepare library file upsert: %w", err)
	}
	return statement, nil
}

func upsertLibraryFile(statement *sql.Stmt, libraryID string, track domain.Track, now time.Time) error {
	state := track.SyncState
	if state == "" {
		state = domain.SyncIndexed
	}
	if _, err := statement.Exec(
		libraryID, track.RelativePath, track.ID, track.FolderID, track.Format,
		track.FileFingerprint.SizeBytes, track.FileFingerprint.ModifiedUnixNano,
		track.FileFingerprint.SidecarSize, track.FileFingerprint.SidecarUnixNano,
		boolToInt(track.Writable), boolToInt(!track.Missing), state, track.ParseError,
		track.MissingSince, now.Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("upsert library file %s: %w", track.RelativePath, err)
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.payload_json,
		       COALESCE(f.file_size, t.file_size), COALESCE(f.file_mtime_ns, t.file_mtime_ns),
		       COALESCE(f.sidecar_size, t.sidecar_size), COALESCE(f.sidecar_mtime_ns, t.sidecar_mtime_ns),
		       CASE WHEN COALESCE(f.present, CASE WHEN t.missing=0 THEN 1 ELSE 0 END)=0 THEN 1 ELSE 0 END,
		       COALESCE(f.missing_since, t.missing_since), COALESCE(f.sync_state, 'indexed'),
		       COALESCE(f.parse_error, ''), f.writable
		FROM tracks t
		LEFT JOIN library_files f ON f.library_id=t.library_id AND f.relative_path=t.relative_path
		WHERE t.library_id = ? ORDER BY t.relative_path`, libraryID)
	if err != nil {
		return scanner.Result{}, false, fmt.Errorf("load tracks: %w", err)
	}
	defer rows.Close()
	result.Tracks = make([]domain.Track, 0, result.Library.TrackCount)
	for rows.Next() {
		var payload []byte
		var size, mtime, sidecarSize, sidecarMtime int64
		var missing int
		var missingSince, syncState, parseError string
		var writable sql.NullInt64
		if err := rows.Scan(&payload, &size, &mtime, &sidecarSize, &sidecarMtime, &missing, &missingSince, &syncState, &parseError, &writable); err != nil {
			return scanner.Result{}, false, err
		}
		var track domain.Track
		if err := json.Unmarshal(payload, &track); err != nil {
			return scanner.Result{}, false, fmt.Errorf("decode track: %w", err)
		}
		track.FileFingerprint = domain.FileFingerprint{SizeBytes: size, ModifiedUnixNano: mtime, SidecarSize: sidecarSize, SidecarUnixNano: sidecarMtime}
		track.Missing = missing != 0
		track.MissingSince = missingSince
		track.SyncState = domain.TrackSyncState(syncState)
		track.ParseError = parseError
		if writable.Valid {
			track.Writable = writable.Int64 != 0
		}
		if track.Missing {
			track.Health = domain.HealthMissing
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
	beforeSidecarJSON, err := marshalSidecarSnapshot(revision.BeforeSidecar)
	if err != nil {
		return domain.Revision{}, err
	}
	afterSidecarJSON, err := marshalSidecarSnapshot(revision.AfterSidecar)
	if err != nil {
		return domain.Revision{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Revision{}, err
	}
	defer tx.Rollback()
	beforeArtworkHash, err := s.saveArtworkBlob(ctx, tx, revision.BeforeArtwork)
	if err != nil {
		return domain.Revision{}, err
	}
	afterArtworkHash, err := s.saveArtworkBlob(ctx, tx, revision.AfterArtwork)
	if err != nil {
		return domain.Revision{}, err
	}
	revision.BeforeArtworkHash = beforeArtworkHash
	revision.AfterArtworkHash = afterArtworkHash
	_, err = tx.ExecContext(ctx, `
        INSERT INTO revisions(
            id, library_id, track_id, track_title, file_name, action, source,
            base_revision, result_revision, fields_json, diff_json,
            before_tags_json, after_tags_json, cover_tone, created_at,
            before_artwork_hash, after_artwork_hash, before_sidecar_json, after_sidecar_json
        ) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revision.ID, revision.LibraryID, revision.TrackID, revision.TrackTitle,
		revision.FileName, revision.Action, revision.Source, revision.BaseRevision,
		revision.ResultRevision, fieldsJSON, diffJSON, beforeJSON, afterJSON,
		revision.CoverTone, revision.CreatedAt.UTC().Format(time.RFC3339Nano),
		beforeArtworkHash, afterArtworkHash, beforeSidecarJSON, afterSidecarJSON)
	if err != nil {
		return domain.Revision{}, fmt.Errorf("insert revision: %w", err)
	}
	if err := s.pruneTrackRevisions(ctx, tx, revision.TrackID); err != nil {
		return domain.Revision{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Revision{}, fmt.Errorf("commit revision: %w", err)
	}
	return revision, nil
}

func (s *Store) saveArtworkBlob(ctx context.Context, tx *sql.Tx, snapshot *domain.ArtworkSnapshot) (string, error) {
	if snapshot == nil {
		return "", nil
	}
	if snapshot.Hash == "" {
		return "", fmt.Errorf("artwork snapshot hash is required")
	}
	if len(snapshot.Data) == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM artwork_blobs WHERE hash=?`, snapshot.Hash).Scan(&exists); err != nil {
			return "", fmt.Errorf("artwork blob %s is unavailable: %w", snapshot.Hash, err)
		}
		return snapshot.Hash, nil
	}
	if len(snapshot.Data) > 10<<20 {
		return "", fmt.Errorf("artwork snapshot exceeds 10 MiB")
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO artwork_blobs(hash, mime, format, width, height, size, data, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hash) DO NOTHING`, snapshot.Hash, snapshot.MIME, snapshot.Format, snapshot.Width, snapshot.Height, snapshot.Size, snapshot.Data, formatTime(s.now().UTC()))
	if err != nil {
		return "", fmt.Errorf("save artwork blob: %w", err)
	}
	return snapshot.Hash, nil
}

func (s *Store) loadArtworkSnapshot(ctx context.Context, hash string) (*domain.ArtworkSnapshot, error) {
	if hash == "" {
		return nil, nil
	}
	var snapshot domain.ArtworkSnapshot
	var data []byte
	if err := s.db.QueryRowContext(ctx, `SELECT mime, format, width, height, size, data FROM artwork_blobs WHERE hash=?`, hash).
		Scan(&snapshot.MIME, &snapshot.Format, &snapshot.Width, &snapshot.Height, &snapshot.Size, &data); err != nil {
		return nil, err
	}
	snapshot.Hash = hash
	snapshot.Data = append([]byte(nil), data...)
	return &snapshot, nil
}

func (s *Store) hydrateRevisionArtwork(ctx context.Context, revision domain.Revision) (domain.Revision, error) {
	if revision.BeforeArtworkHash != "" {
		snapshot, err := s.loadArtworkSnapshot(ctx, revision.BeforeArtworkHash)
		if err != nil {
			return domain.Revision{}, err
		}
		revision.BeforeArtwork = snapshot
	}
	if revision.AfterArtworkHash != "" {
		snapshot, err := s.loadArtworkSnapshot(ctx, revision.AfterArtworkHash)
		if err != nil {
			return domain.Revision{}, err
		}
		revision.AfterArtwork = snapshot
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
               before_tags_json, after_tags_json, cover_tone, created_at,
               before_artwork_hash, after_artwork_hash, before_sidecar_json, after_sidecar_json
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
               before_tags_json, after_tags_json, cover_tone, created_at,
               before_artwork_hash, after_artwork_hash, before_sidecar_json, after_sidecar_json
        FROM revisions WHERE id = ?`, id)
	revision, err := scanRevision(row)
	if err != nil {
		return domain.Revision{}, err
	}
	return s.hydrateRevisionArtwork(ctx, revision)
}

type rowScanner interface{ Scan(...any) error }

func scanRevision(row rowScanner) (domain.Revision, error) {
	var revision domain.Revision
	var fieldsJSON, diffJSON, beforeJSON, afterJSON, beforeSidecarJSON, afterSidecarJSON []byte
	var createdAt string
	var beforeArtworkHash, afterArtworkHash sql.NullString
	err := row.Scan(
		&revision.ID, &revision.LibraryID, &revision.TrackID, &revision.TrackTitle,
		&revision.FileName, &revision.Action, &revision.Source, &revision.BaseRevision,
		&revision.ResultRevision, &fieldsJSON, &diffJSON, &beforeJSON, &afterJSON,
		&revision.CoverTone, &createdAt, &beforeArtworkHash, &afterArtworkHash,
		&beforeSidecarJSON, &afterSidecarJSON,
	)
	if err != nil {
		return domain.Revision{}, err
	}
	if len(fieldsJSON) == 0 || string(fieldsJSON) == "null" {
		revision.Fields = []string{}
	} else if err := json.Unmarshal(fieldsJSON, &revision.Fields); err != nil {
		return domain.Revision{}, err
	}
	if revision.Fields == nil {
		revision.Fields = []string{}
	}
	if len(diffJSON) == 0 || string(diffJSON) == "null" {
		revision.Diff = []domain.RevisionDiff{}
	} else if err := json.Unmarshal(diffJSON, &revision.Diff); err != nil {
		return domain.Revision{}, err
	}
	if revision.Diff == nil {
		revision.Diff = []domain.RevisionDiff{}
	}
	if len(beforeJSON) == 0 || string(beforeJSON) == "null" {
		revision.BeforeTags = map[string][]string{}
	} else if err := json.Unmarshal(beforeJSON, &revision.BeforeTags); err != nil {
		return domain.Revision{}, err
	}
	if revision.BeforeTags == nil {
		revision.BeforeTags = map[string][]string{}
	}
	if len(afterJSON) == 0 || string(afterJSON) == "null" {
		revision.AfterTags = map[string][]string{}
	} else if err := json.Unmarshal(afterJSON, &revision.AfterTags); err != nil {
		return domain.Revision{}, err
	}
	if revision.AfterTags == nil {
		revision.AfterTags = map[string][]string{}
	}
	if err := unmarshalSidecarSnapshot(beforeSidecarJSON, &revision.BeforeSidecar); err != nil {
		return domain.Revision{}, err
	}
	if err := unmarshalSidecarSnapshot(afterSidecarJSON, &revision.AfterSidecar); err != nil {
		return domain.Revision{}, err
	}
	revision.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.Revision{}, err
	}
	if beforeArtworkHash.Valid {
		revision.BeforeArtworkHash = beforeArtworkHash.String
	}
	if afterArtworkHash.Valid {
		revision.AfterArtworkHash = afterArtworkHash.String
	}
	if revision.BeforeArtworkHash != "" {
		revision.BeforeArtwork = &domain.ArtworkSnapshot{Hash: revision.BeforeArtworkHash}
	}
	if revision.AfterArtworkHash != "" {
		revision.AfterArtwork = &domain.ArtworkSnapshot{Hash: revision.AfterArtworkHash}
	}
	return revision, nil
}

func nonNilTags(tags map[string][]string) map[string][]string {
	if tags == nil {
		return map[string][]string{}
	}
	return tags
}

func marshalSidecarSnapshot(snapshot *domain.SidecarSnapshot) ([]byte, error) {
	if snapshot == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(snapshot)
}

func unmarshalSidecarSnapshot(data []byte, target **domain.SidecarSnapshot) error {
	if len(data) == 0 || string(data) == "{}" || string(data) == "null" {
		*target = nil
		return nil
	}
	var snapshot domain.SidecarSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	*target = &snapshot
	return nil
}

func newRevisionID(now time.Time) string {
	return newID("revlog", now)
}

func newID(prefix string, now time.Time) string {
	random := make([]byte, 6)
	_, _ = rand.Read(random)
	return fmt.Sprintf("%s-%d-%s", prefix, now.UTC().UnixMilli(), hex.EncodeToString(random))
}
