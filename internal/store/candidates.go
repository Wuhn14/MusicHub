package store

import (
	"context"
	"time"
)

// Candidate is a recording proposed for a file that could not be identified
// automatically.
type Candidate struct {
	Rank                 int // 1 for the best
	RecordingID, AlbumID string
	Title, Artist, Album string
	LengthMs             int
	Score                int // 0 to 100
	Source               string
}

// Candidate sources.
const (
	SourceTag      = "tag"
	SourceAcoustID = "acoustid"
	SourceSearch   = "search"
)

// SetCandidates replaces the candidates of a pending file. Ranks are assigned
// in the order given, starting at 1. It returns ErrNotPending when the file
// is no longer pending, ErrNotFound when it is gone.
func (s *Store) SetCandidates(ctx context.Context, fileID string, cands []Candidate) error {
	return s.setCandidates(ctx, fileID, cands, time.Time{})
}

// FinishUnmatched records the end of an identification run that found no
// match: the candidates replace the previous ones and the attempt time is
// kept, so the file is not queued again at startup until it changes. Same
// errors as SetCandidates.
func (s *Store) FinishUnmatched(ctx context.Context, fileID string, cands []Candidate, at time.Time) error {
	return s.setCandidates(ctx, fileID, cands, at)
}

func (s *Store) setCandidates(ctx context.Context, fileID string, cands []Candidate, attempted time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Touch the row first: it guards on the status and takes the write lock.
	q := `UPDATE files SET identify_attempted_at = identify_attempted_at WHERE id = ? AND status = 'pending'`
	args := []any{fileID}
	if !attempted.IsZero() {
		q = `UPDATE files SET identify_attempted_at = ? WHERE id = ? AND status = 'pending'`
		args = []any{formatTime(attempted), fileID}
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notPendingOrMissing(ctx, tx, fileID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM identification_candidates WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	for i, c := range cands {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO identification_candidates (file_id, rank, recording_id, album_id, title, artist, album, length_ms, score, source)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fileID, i+1, c.RecordingID, c.AlbumID, c.Title, c.Artist, c.Album, c.LengthMs, c.Score, c.Source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListCandidates returns the candidates of a file, best first.
func (s *Store) ListCandidates(ctx context.Context, fileID string) ([]Candidate, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT rank, recording_id, album_id, title, artist, album, length_ms, score, source
FROM identification_candidates WHERE file_id = ? ORDER BY rank`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.Rank, &c.RecordingID, &c.AlbumID, &c.Title, &c.Artist, &c.Album,
			&c.LengthMs, &c.Score, &c.Source); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
