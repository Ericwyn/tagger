package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
)

func (s *Store) CreateJob(ctx context.Context, job domain.Job) (domain.Job, error) {
	if job.ID == "" {
		job.ID = newID("job", s.now())
	}
	if job.State == "" {
		job.State = domain.JobWaiting
	}
	now := s.now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs(id, kind, state, library_id, title, detail, processed, total, succeeded, failed, error_text, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Kind, job.State, job.LibraryID, job.Title, job.Detail, job.Processed, job.Total,
		job.Succeeded, job.Failed, job.Error, formatTime(job.CreatedAt), formatTime(job.UpdatedAt))
	if err != nil {
		return domain.Job{}, fmt.Errorf("insert job: %w", err)
	}
	return job, nil
}

func (s *Store) RecoverRunningJobs(ctx context.Context) error {
	now := formatTime(s.now().UTC())
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET state = ?, detail = '服务重启后等待恢复', started_at = NULL, updated_at = ?
		WHERE state = ?`, domain.JobWaiting, now, domain.JobRunning)
	return err
}

func (s *Store) ClaimJob(ctx context.Context) (domain.Job, bool, error) {
	now := formatTime(s.now().UTC())
	row := s.db.QueryRowContext(ctx, `
		UPDATE jobs SET state = ?, started_at = COALESCE(started_at, ?), updated_at = ?
		WHERE id = (SELECT id FROM jobs WHERE state = ? ORDER BY created_at, id LIMIT 1)
		RETURNING id, kind, state, library_id, title, detail, processed, total, succeeded, failed,
		          error_text, created_at, started_at, completed_at, updated_at`,
		domain.JobRunning, now, now, domain.JobWaiting)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Job{}, false, nil
	}
	if err != nil {
		return domain.Job{}, false, err
	}
	return job, true, nil
}

func (s *Store) UpdateJob(ctx context.Context, job domain.Job) error {
	job.UpdatedAt = s.now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET state=?, detail=?, processed=?, total=?, succeeded=?, failed=?, error_text=?,
		started_at=?, completed_at=?, updated_at=? WHERE id=?`,
		job.State, job.Detail, job.Processed, job.Total, job.Succeeded, job.Failed, job.Error,
		nullTime(job.StartedAt), nullTime(job.CompletedAt), formatTime(job.UpdatedAt), job.ID)
	return err
}

func (s *Store) ListJobs(ctx context.Context, limit int) ([]domain.Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, state, library_id, title, detail, processed, total, succeeded, failed,
		       error_text, created_at, started_at, completed_at, updated_at
		FROM jobs ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (s *Store) Job(ctx context.Context, id string) (domain.Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `
		SELECT id, kind, state, library_id, title, detail, processed, total, succeeded, failed,
		       error_text, created_at, started_at, completed_at, updated_at FROM jobs WHERE id=?`, id))
}

func scanJob(row rowScanner) (domain.Job, error) {
	var job domain.Job
	var created, updated string
	var started, completed sql.NullString
	err := row.Scan(&job.ID, &job.Kind, &job.State, &job.LibraryID, &job.Title, &job.Detail,
		&job.Processed, &job.Total, &job.Succeeded, &job.Failed, &job.Error,
		&created, &started, &completed, &updated)
	if err != nil {
		return domain.Job{}, err
	}
	job.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.Job{}, err
	}
	job.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return domain.Job{}, err
	}
	if started.Valid {
		job.StartedAt, err = time.Parse(time.RFC3339Nano, started.String)
	}
	if err == nil && completed.Valid {
		job.CompletedAt, err = time.Parse(time.RFC3339Nano, completed.String)
	}
	return job, err
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}
