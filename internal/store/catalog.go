package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Artist is a MusicBrainz artist.
type Artist struct {
	ID, Name, SortName, Disambiguation, Country string
	FetchedAt                                   time.Time
}

// Album is a MusicBrainz release group.
type Album struct {
	ID, Title, ArtistID, ArtistName, PrimaryType string
	SecondaryTypes                               []string
	FirstReleaseDate                             string
	Tracks                                       []Track
	FetchedAt                                    time.Time
}

// Track is one recording at a given disc and position of an album.
type Track struct {
	RecordingID, Title string
	Disc, Position     int
	LengthMs           int
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// UpsertArtist stores the artist and its albums (titles only, without
// tracks). Existing albums keep their tracks and fetch date.
func (s *Store) UpsertArtist(ctx context.Context, a Artist, albums []Album) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO artists (id, name, sort_name, disambiguation, country, fetched_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, sort_name=excluded.sort_name,
  disambiguation=excluded.disambiguation, country=excluded.country, fetched_at=excluded.fetched_at`,
		a.ID, a.Name, a.SortName, a.Disambiguation, a.Country, formatTime(a.FetchedAt)); err != nil {
		return err
	}
	for _, al := range albums {
		types, err := encodeTypes(al.SecondaryTypes)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO albums (id, title, artist_id, artist_name, primary_type, secondary_types, first_release_date, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET title=excluded.title, artist_id=excluded.artist_id,
  artist_name=excluded.artist_name, primary_type=excluded.primary_type,
  secondary_types=excluded.secondary_types, first_release_date=excluded.first_release_date`,
			al.ID, al.Title, al.ArtistID, al.ArtistName, al.PrimaryType, types, al.FirstReleaseDate, formatTime(al.FetchedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetArtist returns the artist and its known albums (without tracks), or
// ErrNotFound.
func (s *Store) GetArtist(ctx context.Context, id string) (Artist, []Album, error) {
	var a Artist
	var fetched string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, sort_name, disambiguation, country, fetched_at FROM artists WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.SortName, &a.Disambiguation, &a.Country, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return Artist{}, nil, ErrNotFound
	}
	if err != nil {
		return Artist{}, nil, err
	}
	a.FetchedAt = parseTime(fetched)

	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, artist_id, artist_name, primary_type, secondary_types, first_release_date, fetched_at
FROM albums WHERE artist_id = ? ORDER BY first_release_date, title, id`, id)
	if err != nil {
		return Artist{}, nil, err
	}
	defer rows.Close()
	var albums []Album
	for rows.Next() {
		al, err := scanAlbum(rows)
		if err != nil {
			return Artist{}, nil, err
		}
		albums = append(albums, al)
	}
	return a, albums, rows.Err()
}

// UpsertAlbum stores the album and replaces its track list.
func (s *Store) UpsertAlbum(ctx context.Context, al Album) error {
	types, err := encodeTypes(al.SecondaryTypes)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO albums (id, title, artist_id, artist_name, primary_type, secondary_types, first_release_date, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET title=excluded.title, artist_id=excluded.artist_id,
  artist_name=excluded.artist_name, primary_type=excluded.primary_type,
  secondary_types=excluded.secondary_types, first_release_date=excluded.first_release_date,
  fetched_at=excluded.fetched_at`,
		al.ID, al.Title, al.ArtistID, al.ArtistName, al.PrimaryType, types, al.FirstReleaseDate, formatTime(al.FetchedAt)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM album_tracks WHERE album_id = ?`, al.ID); err != nil {
		return err
	}
	for _, t := range al.Tracks {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO recordings (id, title, length_ms) VALUES (?, ?, ?)
ON CONFLICT(id) DO UPDATE SET title=excluded.title, length_ms=excluded.length_ms`,
			t.RecordingID, t.Title, t.LengthMs); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO album_tracks (album_id, disc, position, recording_id) VALUES (?, ?, ?, ?)`,
			al.ID, t.Disc, t.Position, t.RecordingID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetAlbum returns the album with its tracks ordered by disc then position,
// or ErrNotFound.
func (s *Store) GetAlbum(ctx context.Context, id string) (Album, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, title, artist_id, artist_name, primary_type, secondary_types, first_release_date, fetched_at
FROM albums WHERE id = ?`, id)
	al, err := scanAlbum(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Album{}, ErrNotFound
	}
	if err != nil {
		return Album{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT r.id, r.title, t.disc, t.position, r.length_ms
FROM album_tracks t JOIN recordings r ON r.id = t.recording_id
WHERE t.album_id = ? ORDER BY t.disc, t.position`, id)
	if err != nil {
		return Album{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.RecordingID, &t.Title, &t.Disc, &t.Position, &t.LengthMs); err != nil {
			return Album{}, err
		}
		al.Tracks = append(al.Tracks, t)
	}
	return al, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanAlbum(sc scanner) (Album, error) {
	var al Album
	var types, fetched string
	if err := sc.Scan(&al.ID, &al.Title, &al.ArtistID, &al.ArtistName, &al.PrimaryType, &types, &al.FirstReleaseDate, &fetched); err != nil {
		return Album{}, err
	}
	if err := json.Unmarshal([]byte(types), &al.SecondaryTypes); err != nil {
		return Album{}, err
	}
	if len(al.SecondaryTypes) == 0 {
		al.SecondaryTypes = nil
	}
	al.FetchedAt = parseTime(fetched)
	return al, nil
}

func encodeTypes(types []string) (string, error) {
	if types == nil {
		types = []string{}
	}
	b, err := json.Marshal(types)
	return string(b), err
}
