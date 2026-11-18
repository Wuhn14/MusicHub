package library

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/store"
)

const testAlbumID = "11111111-1111-4111-8111-111111111111"

func put(t *testing.T, st *store.Store, f store.File) store.File {
	t.Helper()
	if f.LastSeen.IsZero() {
		f.LastSeen = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	}
	out, err := st.UpsertScannedFile(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func seedCatalogAlbum(t *testing.T, st *store.Store) {
	t.Helper()
	err := st.UpsertAlbum(context.Background(), store.Album{
		ID: testAlbumID, Title: "Night Drive", ArtistID: "22222222-2222-4222-8222-222222222222",
		ArtistName: "Lumen", PrimaryType: "Album", FirstReleaseDate: "2019-05-03",
		Tracks:    []store.Track{{RecordingID: "rec-1", Title: "Opening", Disc: 1, Position: 1, LengthMs: 1000}},
		FetchedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAlbumsGroupIdentifiedAndTagged(t *testing.T) {
	st := openStore(t)
	seedCatalogAlbum(t, st)
	t0 := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	put(t, st, store.File{Path: "/m/1.mp3", Status: store.FileIdentified, AlbumID: testAlbumID, RecordingID: "rec-1",
		Tags: media.Tags{Title: "Opening", Artist: "Lumen (tag)", Album: "tag name differs"}, Disc: 1, Position: 1, DurationMs: 1000, AddedAt: t0})
	put(t, st, store.File{Path: "/m/2.mp3", Status: store.FileIdentified, AlbumID: testAlbumID, RecordingID: "rec-2",
		Tags: media.Tags{Title: "Second"}, Disc: 1, Position: 2, DurationMs: 2000, AddedAt: t0})
	// Unidentified files group by (album artist or artist, album), ignoring case.
	put(t, st, store.File{Path: "/m/a.mp3", Status: store.FilePending, AddedAt: t0.Add(time.Hour), DurationMs: 3000,
		Tags: media.Tags{Title: "A", Artist: "Test Band", Album: "Demo", Date: "2021-01-01", Track: 1}})
	put(t, st, store.File{Path: "/m/b.mp3", Status: store.FileLocal, AddedAt: t0.Add(2 * time.Hour), DurationMs: 3000,
		Tags: media.Tags{Title: "B", Artist: "Guest", AlbumArtist: " test band ", Album: "DEMO", Track: 2}})
	put(t, st, store.File{Path: "/m/c.mp3", Status: store.FilePending, AddedAt: t0, Tags: media.Tags{Title: "Loose"}})

	albums, err := NewQuery(st).Albums(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 3 {
		t.Fatalf("albums = %+v", albums)
	}
	// Default order: most recently added first.
	if albums[0].Title != "Demo" || albums[0].TrackCount != 2 || albums[0].Artist != "Test Band" ||
		albums[0].Year != "2021" || albums[0].CoverURL != "" || !albums[0].Local {
		t.Errorf("tagged album = %+v", albums[0])
	}
	var ident, loose LibAlbum
	for _, a := range albums {
		switch a.Title {
		case "Night Drive":
			ident = a
		case "Titres sans album":
			loose = a
		}
	}
	if ident.TrackCount != 2 || ident.Artist != "Lumen" || ident.Year != "2019" ||
		ident.CoverURL != "/api/covers/"+testAlbumID || ident.DurationMs != 3000 || ident.Local {
		t.Errorf("identified album = %+v", ident)
	}
	if loose.TrackCount != 1 || loose.Artist != "Artiste inconnu" {
		t.Errorf("loose album = %+v", loose)
	}

	byName, _ := NewQuery(st).Albums(context.Background(), "name")
	if byName[0].Title != "Demo" || byName[1].Title != "Night Drive" || byName[2].Title != "Titres sans album" {
		t.Errorf("name order = %+v", byName)
	}
	byYear, _ := NewQuery(st).Albums(context.Background(), "year")
	if byYear[0].Year != "2021" || byYear[1].Year != "2019" || byYear[2].Year != "" {
		t.Errorf("year order = %+v", byYear)
	}

	_, tracks, err := NewQuery(st).Album(context.Background(), ident.Key)
	if err != nil || len(tracks) != 2 || tracks[0].Title != "Opening" || tracks[1].Title != "Second" || tracks[1].Position != 2 {
		t.Fatalf("Album = %+v, %v", tracks, err)
	}
	artists, _ := NewQuery(st).Artists(context.Background())
	if len(artists) != 3 || artists[0].Name != "Artiste inconnu" || artists[1].Name != "Lumen" ||
		artists[2].Name != "Test Band" || artists[2].TrackCount != 2 || artists[2].AlbumCount != 1 {
		t.Errorf("artists = %+v", artists)
	}
}

func TestHiddenAndMissingExcluded(t *testing.T) {
	st := openStore(t)
	for _, status := range []string{store.FileHidden, store.FileMissing, store.FileUnreadable, store.FilePending} {
		put(t, st, store.File{Path: "/m/" + status + ".mp3", Status: status, Tags: media.Tags{Title: status, Artist: "X", Album: "Y"}})
	}
	q := NewQuery(st)
	tracks, err := q.Tracks(context.Background(), "", "")
	if err != nil || len(tracks) != 1 || tracks[0].Title != "pending" {
		t.Fatalf("tracks = %+v, %v", tracks, err)
	}
	albums, _ := q.Albums(context.Background(), "")
	if len(albums) != 1 || albums[0].TrackCount != 1 {
		t.Errorf("albums = %+v", albums)
	}
	if tr, _ := q.Tracks(context.Background(), "name", "PEND"); len(tr) != 1 {
		t.Errorf("search = %+v", tr)
	}
	if tr, _ := q.Tracks(context.Background(), "name", "zzz"); len(tr) != 0 {
		t.Errorf("search miss = %+v", tr)
	}
}

func TestAlbumKeyRoundTrip(t *testing.T) {
	st := openStore(t)
	put(t, st, store.File{Path: "/m/a.mp3", Status: store.FilePending,
		Tags: media.Tags{Title: "A", Artist: "AC/DC & Co ?#", Album: "Back / In Black %"}})
	q := NewQuery(st)
	albums, _ := q.Albums(context.Background(), "")
	if len(albums) != 1 {
		t.Fatalf("albums = %+v", albums)
	}
	key := albums[0].Key
	if url.PathEscape(key) != key {
		t.Errorf("key %q is not URL-safe", key)
	}
	al, tracks, err := q.Album(context.Background(), key)
	if err != nil || al.Title != "Back / In Black %" || al.Artist != "AC/DC & Co ?#" || len(tracks) != 1 {
		t.Fatalf("Album = %+v %+v %v", al, tracks, err)
	}
	for _, bad := range []string{"", "!!!", encodeKey("junk"), encodeKey("mb:unknown"), encodeKey("tag:x\x00y")} {
		if _, _, err := q.Album(context.Background(), bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("Album(%q) err = %v, want ErrNotFound", bad, err)
		}
	}
}
