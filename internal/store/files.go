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
	Status string
	Path   string
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
	_ = json.Unmarshal([]byte(tags), &f.Tags)
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
		switch status {
		case FileMissing:
			newStatus = prev
			if newStatus == "" {
				newStatus = f.Status
			}
		case FileUnreadable:
			newStatus = f.Status
		}
		if newStatus == "" {
			newStatus = FilePending
		}
		_, err = tx.ExecContext(ctx, `
UPDATE files SET size=?, mtime=?, format=?, codec=?, bitrate=?, sample_rate=?,
  duration_ms=?, lossless=?, tags_json=?, status=?, prev_status='', last_seen=?, missing_since=''
WHERE id = ?`,
			f.Size, f.Mtime.UnixNano(), f.Format, f.Codec, f.BitRate, f.SampleRate,
			f.DurationMs, f.Lossless, string(tags), newStatus, optTime(f.LastSeen), id)
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
