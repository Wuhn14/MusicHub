package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Job is a persistent background task about one file.
type Job struct {
	ID        int64
	Kind      string
	FileID    string
	Attempts  int
	RunAfter  time.Time
	CreatedAt time.Time
}

// EnqueueJob adds a job of the given kind for the file, runnable from
// runAfter. It is idempotent: when the file already has a job of that kind,
// nothing changes and false is returned.
func (s *Store) EnqueueJob(ctx context.Context, kind, fileID string, runAfter time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO jobs (kind, file_id, attempts, run_after, created_at) VALUES (?, ?, 0, ?, ?)
ON CONFLICT(kind, file_id) DO NOTHING`, kind, fileID, runAfter.UnixNano(), runAfter.UnixNano())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func scanJob(r rowScanner) (Job, error) {
	var j Job
	var runAfter, created int64
	if err := r.Scan(&j.ID, &j.Kind, &j.FileID, &j.Attempts, &runAfter, &created); err != nil {
		return Job{}, err
	}
	j.RunAfter, j.CreatedAt = time.Unix(0, runAfter).UTC(), time.Unix(0, created).UTC()
	return j, nil
}

// NextJob returns the oldest job of the given kind that may run at now, or
// ErrNotFound when none is due.
func (s *Store) NextJob(ctx context.Context, kind string, now time.Time) (Job, error) {
	j, err := scanJob(s.db.QueryRowContext(ctx, `
SELECT id, kind, file_id, attempts, run_after, created_at FROM jobs
WHERE kind = ? AND run_after <= ? ORDER BY created_at, id LIMIT 1`, kind, now.UnixNano()))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return j, err
}

// GetJob returns the job of the given kind for the file, or ErrNotFound.
func (s *Store) GetJob(ctx context.Context, kind, fileID string) (Job, error) {
	j, err := scanJob(s.db.QueryRowContext(ctx, `
SELECT id, kind, file_id, attempts, run_after, created_at FROM jobs WHERE kind = ? AND file_id = ?`, kind, fileID))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return j, err
}

// RescheduleJob records a failed attempt: the job keeps its place in the
// queue but does not run before runAfter.
func (s *Store) RescheduleJob(ctx context.Context, id int64, attempts int, runAfter time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET attempts = ?, run_after = ? WHERE id = ?`,
		attempts, runAfter.UnixNano(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteJob removes a job. Deleting a job that no longer exists is not an
// error.
func (s *Store) DeleteJob(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, id)
	return err
}

// CountJobs returns how many jobs of the given kind are queued, due or not.
func (s *Store) CountJobs(ctx context.Context, kind string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE kind = ?`, kind).Scan(&n)
	return n, err
}

// PendingFilesWithoutJob returns the ids of the pending files that have no
// job of the given kind and no identification candidates yet, oldest first.
func (s *Store) PendingFilesWithoutJob(ctx context.Context, kind string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT f.id FROM files f
WHERE f.status = 'pending'
  AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.kind = ? AND j.file_id = f.id)
  AND NOT EXISTS (SELECT 1 FROM identification_candidates c WHERE c.file_id = f.id)
ORDER BY f.added_at, f.path`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
