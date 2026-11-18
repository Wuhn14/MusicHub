package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wuhn14/MusicHub/internal/media"
)

// File statuses.
const (
	FilePending    = "pending"
	FileIdentified = "identified"
	FileLocal      = "local"
	FileUnreadable = "unreadable"
	FileHidden     = "hidden"
	FileMissing    = "missing"
)

// File origins.
const (
	OriginDropped  = "dropped"
	OriginUploaded = "uploaded"
)

// File is one audio file known to the library.
type File struct {
	ID, Path, Origin     string
	Size                 int64
	Mtime                time.Time
	Format, Codec        string
	BitRate, SampleRate  int
	DurationMs           int
	Lossless             bool
	Tags                 media.Tags
	Status               string
	RecordingID, AlbumID string
	LocalTrackID         string
	Disc, Position       int
	LastSeen             time.Time
	MissingSince         time.Time
	AddedAt              time.Time
}

// FileFilter narrows ListFiles. Empty fields match everything.
type FileFilter struct {
	Status      string
	Path        string
	RecordingID string
}

const fileCols = `id, path, origin, size, mtime, format, codec, bitrate, sample_rate,
duration_ms, lossless, tags_json, status, recording_id, album_id, local_track_id,
disc, position, last_seen, missing_since, added_at`

func newFileID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(b)
}

func optTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return formatTime(t)
}

type rowScanner interface{ Scan(...any) error }

func scanFile(r rowScanner) (File, error) {
	var f File
	var mtime int64
	var lossless int
	var tags, lastSeen, missing, added string
	err := r.Scan(&f.ID, &f.Path, &f.Origin, &f.Size, &mtime, &f.Format, &f.Codec,
		&f.BitRate, &f.SampleRate, &f.DurationMs, &lossless, &tags, &f.Status,
		&f.RecordingID, &f.AlbumID, &f.LocalTrackID, &f.Disc, &f.Position,
		&lastSeen, &missing, &added)
	if err != nil {
		return File{}, err
	}
	f.Mtime = time.Unix(0, mtime).UTC()
	f.Lossless = lossless != 0
	if err := json.Unmarshal([]byte(tags), &f.Tags); err != nil {
		f.Tags = media.Tags{} // corrupt tags_json: treat as empty
	}
	f.LastSeen, f.MissingSince, f.AddedAt = parseTime(lastSeen), parseTime(missing), parseTime(added)
	return f, nil
}

// UpsertScannedFile records a file found by the scanner. A file already stored
// at the same path keeps its id, origin, identification links and status; only
// the physical fields (size, date, probe result, tags) and last-seen time are
// refreshed. A missing file regains the status it had before; an unreadable
// file takes the status given in f. The stored row is returned.
func (s *Store) UpsertScannedFile(ctx context.Context, f File) (File, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback()
	tags, err := json.Marshal(f.Tags)
	if err != nil {
		return File{}, err
	}
	var id, status, prev string
	err = tx.QueryRowContext(ctx, `SELECT id, status, prev_status FROM files WHERE path = ?`, f.Path).Scan(&id, &status, &prev)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if f.ID == "" {
			f.ID = newFileID()
		}
		if f.Origin == "" {
			f.Origin = OriginDropped
		}
		if f.Status == "" {
			f.Status = FilePending
		}
		if f.AddedAt.IsZero() {
			f.AddedAt = f.LastSeen
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO files (id, path, origin, size, mtime, format, codec, bitrate, sample_rate,
  duration_ms, lossless, tags_json, status, recording_id, album_id, local_track_id,
  disc, position, last_seen, missing_since, added_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			f.ID, f.Path, f.Origin, f.Size, f.Mtime.UnixNano(), f.Format, f.Codec, f.BitRate,
			f.SampleRate, f.DurationMs, f.Lossless, string(tags), f.Status, f.RecordingID,
			f.AlbumID, f.LocalTrackID, f.Disc, f.Position, optTime(f.LastSeen), "", formatTime(f.AddedAt))
	case err != nil:
		return File{}, err
	default:
		newStatus := status
		switch {
		case f.Status == FileUnreadable:
			// Whatever it was before, the file no longer reads as audio.
			// Identification links stay in the row in case it recovers.
			newStatus = FileUnreadable
		case status == FileMissing:
			newStatus = prev
			if newStatus == "" {
				newStatus = f.Status
			}
		case status == FileUnreadable:
			newStatus = f.Status
		}
		if newStatus == "" {
			newStatus = FilePending
		}
		if f.Status == FileUnreadable {
			_, err = tx.ExecContext(ctx, `
UPDATE files SET size=?, mtime=?, format=?, codec=?, bitrate=?, sample_rate=?,
  duration_ms=?, lossless=?, tags_json=?, status=?, prev_status='', last_seen=?, missing_since=''
WHERE id = ?`,
				f.Size, f.Mtime.UnixNano(), f.Format, f.Codec, f.BitRate, f.SampleRate,
				f.DurationMs, f.Lossless, string(tags), newStatus, optTime(f.LastSeen), id)
		} else {
			_, err = tx.ExecContext(ctx, `
UPDATE files SET size=?, mtime=?, format=?, codec=?, bitrate=?, sample_rate=?,
  duration_ms=?, lossless=?, tags_json=?, disc=?, position=?, status=?, prev_status='',
  last_seen=?, missing_since=''
WHERE id = ?`,
				f.Size, f.Mtime.UnixNano(), f.Format, f.Codec, f.BitRate, f.SampleRate,
				f.DurationMs, f.Lossless, string(tags), f.Disc, f.Position, newStatus, optTime(f.LastSeen), id)
		}
		f.ID = id
	}
	if err != nil {
		return File{}, err
	}
	out, err := scanFile(tx.QueryRowContext(ctx, `SELECT `+fileCols+` FROM files WHERE id = ?`, f.ID))
	if err != nil {
		return File{}, err
	}
	return out, tx.Commit()
}

// GetFile returns the file with the given id, or ErrNotFound.
func (s *Store) GetFile(ctx context.Context, id string) (File, error) {
	f, err := scanFile(s.db.QueryRowContext(ctx, `SELECT `+fileCols+` FROM files WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return f, err
}

// GetFileByPath returns the file stored at path, or ErrNotFound.
func (s *Store) GetFileByPath(ctx context.Context, path string) (File, error) {
	f, err := scanFile(s.db.QueryRowContext(ctx, `SELECT `+fileCols+` FROM files WHERE path = ?`, path))
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return f, err
}

// ListFiles returns the files matching the filter, ordered by path.
func (s *Store) ListFiles(ctx context.Context, flt FileFilter) ([]File, error) {
	q := `SELECT ` + fileCols + ` FROM files`
	var where []string
	var args []any
	if flt.Status != "" {
		where = append(where, "status = ?")
		args = append(args, flt.Status)
	}
	if flt.Path != "" {
		where = append(where, "path = ?")
		args = append(args, flt.Path)
	}
	if flt.RecordingID != "" {
		where = append(where, "recording_id = ?")
		args = append(args, flt.RecordingID)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := s.db.QueryContext(ctx, q+" ORDER BY path", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetFileStatus changes a file's status, or returns ErrNotFound.
func (s *Store) SetFileStatus(ctx context.Context, id, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE files SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkSeen records that the given unchanged files were seen at the given
// time. A missing file regains its previous status.
func (s *Store) MarkSeen(ctx context.Context, ids []string, seen time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
UPDATE files SET last_seen = ?,
  status = CASE WHEN status = 'missing' THEN COALESCE(NULLIF(prev_status, ''), 'pending') ELSE status END,
  prev_status = CASE WHEN status = 'missing' THEN '' ELSE prev_status END,
  missing_since = ''
WHERE id = ?`, formatTime(seen), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MarkMissing flags as missing every non-missing file last seen before
// seenSince, remembering its status so it can be restored. It returns how
// many files were flagged.
func (s *Store) MarkMissing(ctx context.Context, seenSince, now time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE files SET prev_status = status, status = 'missing', missing_since = ?
WHERE status <> 'missing' AND last_seen < ?`, formatTime(now), formatTime(seenSince))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// PurgeMissing deletes files missing since before olderThan.
func (s *Store) PurgeMissing(ctx context.Context, olderThan time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM files WHERE status = 'missing' AND missing_since <> '' AND missing_since < ?`, formatTime(olderThan))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// LinkFile marks a file as identified: it is the given recording, at the
// given disc and position of the album (a release group; empty when the
// recording belongs to none). Its identification candidates are dropped.
func (s *Store) LinkFile(ctx context.Context, id, recordingID, albumID string, disc, position int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
UPDATE files SET status = 'identified', recording_id = ?, album_id = ?, disc = ?, position = ?
WHERE id = ?`, recordingID, albumID, disc, position, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM identification_candidates WHERE file_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteFile removes a file row with its jobs and candidates. It does not
// touch the disk. Deleting an unknown file is not an error.
func (s *Store) DeleteFile(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id)
	return err
}

// AlbumAvailability maps the recordings of an album that the library can
// play to one identified file each. A recording counts when a file is linked
// to it on this album, or on another album while the recording is in this
// album's track list. Files linked to this album are preferred.
func (s *Store) AlbumAvailability(ctx context.Context, albumID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT recording_id, id FROM files
WHERE status = 'identified' AND recording_id <> ''
  AND (album_id = ? OR recording_id IN (SELECT recording_id FROM album_tracks WHERE album_id = ?))
ORDER BY CASE WHEN album_id = ? THEN 0 ELSE 1 END, lossless DESC, bitrate DESC, id`, albumID, albumID, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var rec, id string
		if err := rows.Scan(&rec, &id); err != nil {
			return nil, err
		}
		if _, ok := out[rec]; !ok {
			out[rec] = id
		}
	}
	return out, rows.Err()
}

// ArtistAvailability counts, for each known album of the artist, how many of
// its recordings the library can play (same rule as AlbumAvailability).
// Albums with nothing available are left out.
func (s *Store) ArtistAvailability(ctx context.Context, artistID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT al.id, COUNT(DISTINCT f.recording_id) FROM albums al
JOIN files f ON f.status = 'identified' AND f.recording_id <> ''
  AND (f.album_id = al.id OR f.recording_id IN (SELECT t.recording_id FROM album_tracks t WHERE t.album_id = al.id))
WHERE al.artist_id = ?
GROUP BY al.id`, artistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
