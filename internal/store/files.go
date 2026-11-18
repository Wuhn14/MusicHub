package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
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
	// UploadName is the name an uploaded file had on the sender's device
	// (display only, never a path); empty for dropped files.
	UploadName string
	// PlacingFrom is the path a file is being moved from by the organizer
	// ("" when no move is in progress); Path then names the destination.
	PlacingFrom    string
	Disc, Position int
	LastSeen       time.Time
	MissingSince   time.Time
	AddedAt        time.Time
}

// FileFilter narrows ListFiles. Empty fields match everything.
type FileFilter struct {
	Status      string
	Path        string
	RecordingID string
	Origin      string
}

const fileCols = `id, path, origin, size, mtime, format, codec, bitrate, sample_rate,
duration_ms, lossless, tags_json, status, recording_id, album_id, local_track_id,
disc, position, last_seen, missing_since, added_at, upload_name, placing_from`

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
		&lastSeen, &missing, &added, &f.UploadName, &f.PlacingFrom)
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
// refreshed, and its identification attempt is forgotten (the file changed). A missing file regains the status it had before; an unreadable
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
  duration_ms=?, lossless=?, tags_json=?, status=?, prev_status='', last_seen=?, missing_since='',
  identify_attempted_at=NULL
WHERE id = ?`,
				f.Size, f.Mtime.UnixNano(), f.Format, f.Codec, f.BitRate, f.SampleRate,
				f.DurationMs, f.Lossless, string(tags), newStatus, optTime(f.LastSeen), id)
		} else {
			_, err = tx.ExecContext(ctx, `
UPDATE files SET size=?, mtime=?, format=?, codec=?, bitrate=?, sample_rate=?,
  duration_ms=?, lossless=?, tags_json=?, disc=?, position=?, status=?, prev_status='',
  last_seen=?, missing_since='', identify_attempted_at=NULL
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

// InsertUploadedFile records a file received through the upload endpoint:
// origin "uploaded", status "pending", stored under its absolute path in the
// incoming folder. A missing id is generated; the stored row is returned.
func (s *Store) InsertUploadedFile(ctx context.Context, f File) (File, error) {
	tags, err := json.Marshal(f.Tags)
	if err != nil {
		return File{}, err
	}
	if f.ID == "" {
		f.ID = newFileID()
	}
	f.Origin, f.Status = OriginUploaded, FilePending
	if f.AddedAt.IsZero() {
		f.AddedAt = time.Now()
	}
	if f.LastSeen.IsZero() {
		f.LastSeen = f.AddedAt
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO files (id, path, origin, size, mtime, format, codec, bitrate, sample_rate,
  duration_ms, lossless, tags_json, status, recording_id, album_id, local_track_id,
  disc, position, last_seen, missing_since, added_at, upload_name)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		f.ID, f.Path, f.Origin, f.Size, f.Mtime.UnixNano(), f.Format, f.Codec, f.BitRate,
		f.SampleRate, f.DurationMs, f.Lossless, string(tags), f.Status, "", "", "",
		f.Disc, f.Position, optTime(f.LastSeen), "", formatTime(f.AddedAt), f.UploadName)
	if err != nil {
		return File{}, err
	}
	return s.GetFile(ctx, f.ID)
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
	if flt.Origin != "" {
		where = append(where, "origin = ?")
		args = append(args, flt.Origin)
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
// many files were flagged. Uploaded files that are still pending, or whose
// path is still under keepUnder (the incoming folder, which the scanner never
// walks), are never flagged; an empty keepUnder exempts only pending uploads.
func (s *Store) MarkMissing(ctx context.Context, seenSince, now time.Time, keepUnder string) (int, error) {
	prefix := ""
	if keepUnder != "" {
		prefix = strings.TrimRight(keepUnder, `/\`) + string(filepath.Separator)
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE files SET prev_status = status, status = 'missing', missing_since = ?
WHERE status <> 'missing' AND last_seen < ?
  AND NOT (origin = 'uploaded' AND (status = 'pending' OR (? <> '' AND substr(path, 1, length(?)) = ?)))`,
		formatTime(now), formatTime(seenSince), prefix, prefix, prefix)
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

// ErrNotPending is returned by the identification writes when the file is no
// longer pending (the scanner or the user changed it meanwhile).
var ErrNotPending = errors.New("store: file is not pending")

// notPendingOrMissing tells why a guarded update touched no row.
func notPendingOrMissing(ctx context.Context, tx *sql.Tx, id string) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return ErrNotPending
}

// LinkFile marks a pending file as identified: it is the given recording, at
// the given disc and position of the album (a release group; empty when the
// recording belongs to none). Its identification candidates are dropped. It
// returns ErrNotPending when the file is no longer pending, ErrNotFound when
// it is gone.
func (s *Store) LinkFile(ctx context.Context, id, recordingID, albumID string, disc, position int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
UPDATE files SET status = 'identified', recording_id = ?, album_id = ?, disc = ?, position = ?,
  identify_attempted_at = NULL
WHERE id = ? AND status = 'pending'`, recordingID, albumID, disc, position, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notPendingOrMissing(ctx, tx, id)
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

// ErrPathTaken is returned by SetFilePath when another file row already uses
// the path.
var ErrPathTaken = errors.New("store: path already used by another file")

// SetFilePath records a new path for a file, keeping its id, origin and
// status, and forgets any move in progress. It returns ErrPathTaken when
// another row has that path and ErrNotFound when the file is gone.
func (s *Store) SetFilePath(ctx context.Context, id, path string) error {
	return s.setPath(ctx, id, path, "")
}

// BeginPlacing records that a file is about to be moved from its current
// path (from) to path: the row takes the destination path right away and
// remembers the source until FinishPlacing or SetFilePath. Same errors as
// SetFilePath.
func (s *Store) BeginPlacing(ctx context.Context, id, path, from string) error {
	return s.setPath(ctx, id, path, from)
}

func (s *Store) setPath(ctx context.Context, id, path, from string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var other int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE path = ? AND id <> ?`, path, id).Scan(&other); err != nil {
		return err
	}
	if other > 0 {
		return ErrPathTaken
	}
	res, err := tx.ExecContext(ctx, `UPDATE files SET path = ?, placing_from = ? WHERE id = ?`, path, from, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// ListPlacing returns the files whose move was started but never finished.
func (s *Store) ListPlacing(ctx context.Context) ([]File, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+fileCols+` FROM files WHERE placing_from <> '' ORDER BY path`)
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

// FinishPlacing records the state of a file right after it was moved or
// retagged: its size and modification date as now found on the disk, the
// tags written into it (merged over the stored ones; nil when none were
// written) and the time it was seen; the move is over. A file flagged missing meanwhile (a
// scan ran while it was being moved) regains its previous status. With
// matching size and date, the next scan does not read the file again.
func (s *Store) FinishPlacing(ctx context.Context, id string, size int64, mtime time.Time, written *media.Tags, seen time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f, err := scanFile(tx.QueryRowContext(ctx, `SELECT `+fileCols+` FROM files WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	tags := f.Tags
	if written != nil {
		mergeTags(&tags, *written)
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE files SET size = ?, mtime = ?, tags_json = ?, last_seen = ?, missing_since = '', placing_from = '',
  status = CASE WHEN status = 'missing' THEN COALESCE(NULLIF(prev_status, ''), 'pending') ELSE status END,
  prev_status = CASE WHEN status = 'missing' THEN '' ELSE prev_status END
WHERE id = ?`, size, mtime.UnixNano(), string(tagsJSON), formatTime(seen), id); err != nil {
		return err
	}
	return tx.Commit()
}

// mergeTags copies the non-empty fields of src over dst, as
// media.Tools.WriteTags does in the file.
func mergeTags(dst *media.Tags, src media.Tags) {
	set := func(d *string, v string) {
		if v != "" {
			*d = v
		}
	}
	set(&dst.Artist, src.Artist)
	set(&dst.AlbumArtist, src.AlbumArtist)
	set(&dst.Album, src.Album)
	set(&dst.Title, src.Title)
	set(&dst.Date, src.Date)
	set(&dst.MBRecordingID, src.MBRecordingID)
	set(&dst.MBReleaseGroupID, src.MBReleaseGroupID)
	set(&dst.MBReleaseID, src.MBReleaseID)
	set(&dst.MBArtistID, src.MBArtistID)
	if src.Track > 0 {
		dst.Track = src.Track
	}
	if src.Disc > 0 {
		dst.Disc = src.Disc
	}
}

// ErrNotUploaded is returned by DeletePendingUpload for a file that was not
// uploaded (a file dropped in the music folder is never deleted).
var ErrNotUploaded = errors.New("store: file was not uploaded")

// DeletePendingUpload removes the row of an uploaded file that is still
// pending, with its jobs and candidates, and returns it so the caller can
// remove it from the disk. It returns ErrNotUploaded for a dropped file,
// ErrNotPending when the file is no longer pending and ErrNotFound when it is
// gone; nothing is deleted then.
func (s *Store) DeletePendingUpload(ctx context.Context, id string) (File, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback()
	f, err := scanFile(tx.QueryRowContext(ctx, `SELECT `+fileCols+` FROM files WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, err
	}
	switch {
	case f.Origin != OriginUploaded:
		return File{}, ErrNotUploaded
	case f.Status != FilePending:
		return File{}, ErrNotPending
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE id = ? AND status = 'pending'`, id); err != nil {
		return File{}, err
	}
	return f, tx.Commit()
}

// awaitingWhere selects the pending files that no identification job of the
// given kind is still working on: those waiting for the user.
const awaitingWhere = `f.status = 'pending'
  AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.kind = ? AND j.file_id = f.id)`

// CountPending returns how many pending files wait for the user (no job of
// the given kind) and how many are still queued for automatic
// identification.
func (s *Store) CountPending(ctx context.Context, jobKind string) (awaiting, queued int, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(CASE WHEN j.id IS NULL THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN j.id IS NULL THEN 0 ELSE 1 END), 0)
FROM files f LEFT JOIN jobs j ON j.kind = ? AND j.file_id = f.id
WHERE f.status = 'pending'`, jobKind).Scan(&awaiting, &queued)
	return awaiting, queued, err
}

// IsAwaiting reports whether the file is pending with no identification job
// of the given kind: it waits for the user. An unknown file is not awaiting.
func (s *Store) IsAwaiting(ctx context.Context, id, jobKind string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files f WHERE f.id = ? AND `+awaitingWhere, id, jobKind).Scan(&n)
	return n > 0, err
}

// ListAwaiting returns up to limit pending files that wait for the user (no
// job of the given kind), oldest first.
func (s *Store) ListAwaiting(ctx context.Context, jobKind string, limit int) ([]File, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+prefixed("f.", fileCols)+` FROM files f WHERE `+awaitingWhere+`
ORDER BY f.added_at, f.path LIMIT ?`, jobKind, limit)
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

// prefixed qualifies each column of a comma-separated list with p.
func prefixed(p, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = p + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}
