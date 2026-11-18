package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/catalog/mbtest"
	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/store"
)

type fixture struct {
	cat *Catalog
	st  *store.Store
	srv *mbtest.Server
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := mbtest.New()
	t.Cleanup(srv.Close)
	f := &fixture{st: st, srv: srv, now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	mb := musicbrainz.New(srv.URL, "MusicHub/test ( https://example.invalid )", 0)
	f.cat = New(st, mb, func() time.Time { return f.now })
	return f
}

func TestAlbumIsCachedAfterFirstFetch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, err := f.cat.Album(ctx, mbtest.AlbumTwoDisc)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Tracks) == 0 {
		t.Fatal("first call returned no tracks")
	}
	n := f.srv.Requests()
	second, err := f.cat.Album(ctx, mbtest.AlbumTwoDisc)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.srv.Requests(); got != n {
		t.Errorf("second call made %d MusicBrainz requests, want 0", got-n)
	}
	if second.Title != first.Title || len(second.Tracks) != len(first.Tracks) {
		t.Errorf("cached album differs: %+v vs %+v", second, first)
	}
}

func TestRefreshesAfterSevenDays(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.cat.Album(ctx, mbtest.AlbumTwoDisc); err != nil {
		t.Fatal(err)
	}
	n := f.srv.Requests()
	f.now = f.now.Add(6 * 24 * time.Hour)
	if _, err := f.cat.Album(ctx, mbtest.AlbumTwoDisc); err != nil {
		t.Fatal(err)
	}
	if got := f.srv.Requests(); got != n {
		t.Fatalf("album refreshed after 6 days (%d requests)", got-n)
	}
	f.now = f.now.Add(2 * 24 * time.Hour)
	al, err := f.cat.Album(ctx, mbtest.AlbumTwoDisc)
	if err != nil {
		t.Fatal(err)
	}
	if f.srv.Requests() == n {
		t.Fatal("album not refreshed after 8 days")
	}
	if !al.FetchedAt.Equal(f.now) {
		t.Errorf("FetchedAt = %v, want %v", al.FetchedAt, f.now)
	}
}

func TestServesStaleCacheWhenMusicBrainzDown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	want, err := f.cat.Album(ctx, mbtest.AlbumTwoDisc)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.cat.Artist(ctx, mbtest.ArtistLumen); err != nil {
		t.Fatal(err)
	}
	f.srv.Close()
	f.now = f.now.Add(30 * 24 * time.Hour)

	got, err := f.cat.Album(ctx, mbtest.AlbumTwoDisc)
	if err != nil {
		t.Fatalf("stale album: %v", err)
	}
	if got.Title != want.Title || len(got.Tracks) != len(want.Tracks) {
		t.Errorf("stale album = %+v, want %+v", got, want)
	}
	if a, _, err := f.cat.Artist(ctx, mbtest.ArtistLumen); err != nil || a.ID != mbtest.ArtistLumen {
		t.Errorf("stale artist = %+v, %v", a, err)
	}
	// Nothing stored: the error is returned.
	if _, err := f.cat.Album(ctx, mbtest.ArtistMany); !errors.Is(err, musicbrainz.ErrUnavailable) {
		t.Errorf("uncached album error = %v, want ErrUnavailable", err)
	}
}

func TestAlbumStubWithoutTracksIsFetched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Artist fetch stores album stubs without tracks.
	a, albums, err := f.cat.Artist(ctx, mbtest.ArtistMany)
	if err != nil || a.ID != mbtest.ArtistMany || len(albums) == 0 {
		t.Fatalf("artist: %+v %d %v", a, len(albums), err)
	}
	// Seed a stub for the two-disc album with a fresh FetchedAt.
	stub := store.Album{ID: mbtest.AlbumTwoDisc, Title: "stub", ArtistID: mbtest.ArtistMany, FetchedAt: f.now}
	if err := f.st.UpsertArtist(ctx, a, []store.Album{stub}); err != nil {
		t.Fatal(err)
	}
	al, err := f.cat.Album(ctx, mbtest.AlbumTwoDisc)
	if err != nil {
		t.Fatal(err)
	}
	if len(al.Tracks) == 0 {
		t.Error("stub was returned instead of being fetched")
	}
}

func TestAlbumNotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.cat.Album(context.Background(), "00000000-0000-4000-8000-00000000ffff")
	if !errors.Is(err, musicbrainz.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestArtistIsCached(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, _, err := f.cat.Artist(ctx, mbtest.ArtistMany); err != nil {
		t.Fatal(err)
	}
	n := f.srv.Requests()
	_, albums, err := f.cat.Artist(ctx, mbtest.ArtistMany)
	if err != nil {
		t.Fatal(err)
	}
	if f.srv.Requests() != n {
		t.Error("second artist call hit MusicBrainz")
	}
	if len(albums) != mbtest.ManyAlbums {
		t.Errorf("albums = %d, want %d", len(albums), mbtest.ManyAlbums)
	}
}

func TestSearchReturnsAllKinds(t *testing.T) {
	f := newFixture(t)
	res, err := f.cat.Search(context.Background(), "lumen")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Artists) == 0 || len(res.Albums) == 0 || len(res.Tracks) == 0 {
		t.Errorf("result = %d artists, %d albums, %d tracks", len(res.Artists), len(res.Albums), len(res.Tracks))
	}
	if got := f.srv.Requests(); got != 3 {
		t.Errorf("requests = %d, want 3", got)
	}
}

func TestEmptyQueryReturnsEmptyResult(t *testing.T) {
	f := newFixture(t)
	for _, q := range []string{"", "   "} {
		res, err := f.cat.Search(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Artists)+len(res.Albums)+len(res.Tracks) != 0 {
			t.Errorf("query %q: non-empty result", q)
		}
	}
	if f.srv.Requests() != 0 {
		t.Errorf("empty query made %d requests", f.srv.Requests())
	}
}
