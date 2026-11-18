package store

import "context"

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

// SetCandidates replaces the candidates of a file. Ranks are assigned in the
// order given, starting at 1.
func (s *Store) SetCandidates(ctx context.Context, fileID string, cands []Candidate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
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
