package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// LocalTrack is a track described by the user for a file that MusicBrainz
// does not know.
type LocalTrack struct {
	ID                 string
	Title, Artist      string
	Album, AlbumArtist string
	Disc, Position     int
	Year               int // 0 when unknown
	// CoverPath is the base name of the cover image in the local covers
	// folder, "" when the track has none.
	CoverPath string
	CreatedAt time.Time
}

const localCols = `id, title, artist, album, album_artist, disc, position, year, cover_path, created_at`

func scanLocal(r rowScanner) (LocalTrack, error) {
	var lt LocalTrack
	var created string
	if err := r.Scan(&lt.ID, &lt.Title, &lt.Artist, &lt.Album, &lt.AlbumArtist, &lt.Disc, &lt.Position,
		&lt.Year, &lt.CoverPath, &created); err != nil {
		return LocalTrack{}, err
	}
	lt.CreatedAt = parseTime(created)
	return lt, nil
}

// CreateLocalTrack records lt and attaches the pending file fileID to it in
// one transaction: the file becomes "local", takes the disc and position of
// lt, and loses its identification candidates. A missing id and creation
// time are filled in. It returns ErrNotPending when the file is no longer
// pending and ErrNotFound when it is gone; nothing is written then.
func (s *Store) CreateLocalTrack(ctx context.Context, fileID string, lt LocalTrack) (LocalTrack, error) {
	if lt.ID == "" {
		lt.ID = newFileID()
	}
	if lt.CreatedAt.IsZero() {
		lt.CreatedAt = time.Now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LocalTrack{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
UPDATE files SET status = 'local', local_track_id = ?, recording_id = '', album_id = '',
  disc = ?, position = ?, identify_attempted_at = NULL
WHERE id = ? AND status = 'pending'`, lt.ID, lt.Disc, lt.Position, fileID)
	if err != nil {
		return LocalTrack{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return LocalTrack{}, notPendingOrMissing(ctx, tx, fileID)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO local_tracks (`+localCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		lt.ID, lt.Title, lt.Artist, lt.Album, lt.AlbumArtist, lt.Disc, lt.Position, lt.Year,
		lt.CoverPath, formatTime(lt.CreatedAt)); err != nil {
		return LocalTrack{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM identification_candidates WHERE file_id = ?`, fileID); err != nil {
		return LocalTrack{}, err
	}
	if err := tx.Commit(); err != nil {
		return LocalTrack{}, err
	}
	return s.GetLocalTrack(ctx, lt.ID)
}

// GetLocalTrack returns the local track with the given id, or ErrNotFound.
func (s *Store) GetLocalTrack(ctx context.Context, id string) (LocalTrack, error) {
	lt, err := scanLocal(s.db.QueryRowContext(ctx, `SELECT `+localCols+` FROM local_tracks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return LocalTrack{}, ErrNotFound
	}
	return lt, err
}

// ListLocalTracks returns every local track, keyed by id.
func (s *Store) ListLocalTracks(ctx context.Context) (map[string]LocalTrack, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+localCols+` FROM local_tracks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]LocalTrack{}
	for rows.Next() {
		lt, err := scanLocal(rows)
		if err != nil {
			return nil, err
		}
		out[lt.ID] = lt
	}
	return out, rows.Err()
}

// SetLocalTrackCover records the base name of the cover image of a local
// track ("" removes it), or returns ErrNotFound.
func (s *Store) SetLocalTrackCover(ctx context.Context, id, coverPath string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE local_tracks SET cover_path = ? WHERE id = ?`, coverPath, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
