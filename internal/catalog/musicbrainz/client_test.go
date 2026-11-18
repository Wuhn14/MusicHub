package musicbrainz

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/catalog/mbtest"
)

const testUA = "MusicHub/test ( https://example.invalid/musichub )"

func newTestClient(t *testing.T, minInterval time.Duration) (*Client, *mbtest.Server) {
	t.Helper()
	srv := mbtest.New()
	t.Cleanup(srv.Close)
	c := New(srv.URL, testUA, minInterval)
	c.retryBase = 10 * time.Millisecond
	return c, srv
}

func TestSendsUserAgentAndJSON(t *testing.T) {
	c, srv := newTestClient(t, 0)
	if _, err := c.SearchArtists(context.Background(), "lumen", 5); err != nil {
		t.Fatal(err)
	}
	last := srv.Last()
	if last.UserAgent != testUA {
		t.Errorf("User-Agent = %q, want %q", last.UserAgent, testUA)
	}
	if last.Query.Get("fmt") != "json" {
		t.Errorf("fmt = %q, want json", last.Query.Get("fmt"))
	}
	if last.Query.Get("limit") != "5" {
		t.Errorf("limit = %q, want 5", last.Query.Get("limit"))
	}
}

func TestRespectsMinInterval(t *testing.T) {
	const interval = 100 * time.Millisecond
	c, srv := newTestClient(t, interval)
	var wg sync.WaitGroup
	// Concurrent callers must share the limiter.
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.SearchArtists(context.Background(), "lumen", 1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	times := srv.Times()
	if len(times) != 3 {
		t.Fatalf("got %d requests, want 3", len(times))
	}
	for i := 1; i < len(times); i++ {
		// Small tolerance for timer/clock granularity.
		if gap := times[i].Sub(times[i-1]); gap < interval-10*time.Millisecond {
			t.Errorf("gap %d = %v, want >= %v", i, gap, interval)
		}
	}
}

func TestSearchEscapesLuceneSyntax(t *testing.T) {
	c, srv := newTestClient(t, 0)
	cases := []struct{ in, want string }{
		{`AC/DC`, `AC\/DC`},
		{`a "b"`, `a \"b\"`},
		{`x:y+z`, `x\:y\+z`},
		{`(a) [b] {c} ^~*?!`, `\(a\) \[b\] \{c\} \^\~\*\?\!`},
		{`a && b || c-d`, `a \&\& b \|\| c\-d`},
		{`back\slash`, `back\\slash`},
		{`Sigur Rós`, `Sigur Rós`},
	}
	for _, tc := range cases {
		if _, err := c.SearchArtists(context.Background(), tc.in, 5); err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got := srv.Last().Query.Get("query"); got != tc.want {
			t.Errorf("query for %q = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRetriesOn503(t *testing.T) {
	c, srv := newTestClient(t, 0)
	srv.FailNext(2)
	if _, err := c.SearchArtists(context.Background(), "lumen", 5); err != nil {
		t.Fatalf("after two 503s: %v", err)
	}
	if got := srv.Requests(); got != 3 {
		t.Errorf("requests = %d, want 3", got)
	}

	c2, srv2 := newTestClient(t, 0)
	srv2.FailNext(4)
	_, err := c2.SearchArtists(context.Background(), "lumen", 5)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if got := srv2.Requests(); got != 4 {
		t.Errorf("requests = %d, want 4 (1 try + 3 retries)", got)
	}
}

func TestRetryWaitHonoursContext(t *testing.T) {
	c, srv := newTestClient(t, 0)
	c.retryBase = time.Hour
	srv.FailNext(1)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.SearchArtists(ctx, "lumen", 5)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestUnreachableIsUnavailable(t *testing.T) {
	srv := mbtest.New()
	url := srv.URL
	srv.Close()
	c := New(url, testUA, 0)
	_, err := c.SearchArtists(context.Background(), "x", 1)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestSearchParsing(t *testing.T) {
	c, _ := newTestClient(t, 0)
	ctx := context.Background()

	artists, err := c.SearchArtists(ctx, "lumen", 5)
	if err != nil || len(artists) != 2 {
		t.Fatalf("artists = %v, %v", artists, err)
	}
	if a := artists[0]; a.ID != mbtest.ArtistLumen || a.Name != "Lumen Harbor" || a.SortName != "Harbor, Lumen" || a.Country != "FR" || a.Disambiguation != "invented band" {
		t.Errorf("artist = %+v", a)
	}

	albums, err := c.SearchAlbums(ctx, "paper", 5)
	if err != nil || len(albums) != 2 {
		t.Fatalf("albums = %v, %v", albums, err)
	}
	if a := albums[0]; a.ID != mbtest.AlbumTwoDisc || a.Title != "Paper Lanterns" || a.ArtistID != mbtest.ArtistLumen || a.ArtistName != "Lumen Harbor" ||
		a.PrimaryType != "Album" || len(a.SecondaryTypes) != 1 || a.SecondaryTypes[0] != "Live" || a.FirstReleaseDate != "2003-05-12" {
		t.Errorf("album = %+v", a)
	}

	tracks, err := c.SearchTracks(ctx, "glass", 5)
	if err != nil || len(tracks) != 2 {
		t.Fatalf("tracks = %v, %v", tracks, err)
	}
	h := tracks[0]
	if h.RecordingID != "00000000-0000-4000-8000-0000000000d1" || h.Title != "Glass Tide" || h.LengthMs != 201000 ||
		h.AlbumID != mbtest.AlbumTwoDisc || h.AlbumTitle != "Paper Lanterns" || h.ArtistName != "Lumen Harbor feat. Mira Vale" ||
		h.Disc != 2 || h.Position != 3 {
		t.Errorf("track hit = %+v", h)
	}
	if o := tracks[1]; o.AlbumID != "" || o.LengthMs != 0 || o.ArtistName != "Mira Vale" {
		t.Errorf("orphan hit = %+v", o)
	}
}

func TestGetArtistPaginates(t *testing.T) {
	c, srv := newTestClient(t, 0)
	a, albums, err := c.GetArtist(context.Background(), mbtest.ArtistMany)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != mbtest.ArtistMany || a.Name == "" {
		t.Errorf("artist = %+v", a)
	}
	if len(albums) != mbtest.ManyAlbums {
		t.Fatalf("got %d albums, want %d", len(albums), mbtest.ManyAlbums)
	}
	seen := map[string]bool{}
	for _, al := range albums {
		if seen[al.ID] {
			t.Fatalf("duplicate album %s", al.ID)
		}
		seen[al.ID] = true
		if al.ArtistID != mbtest.ArtistMany || al.ArtistName != a.Name {
			t.Fatalf("album artist = %q/%q", al.ArtistID, al.ArtistName)
		}
	}
	if got := srv.Requests(); got != 4 { // lookup + 3 pages
		t.Errorf("requests = %d, want 4", got)
	}
}

func TestGetAlbumPicksEarliestOfficialRelease(t *testing.T) {
	c, srv := newTestClient(t, 0)
	al, err := c.GetAlbum(context.Background(), mbtest.AlbumTwoDisc)
	if err != nil {
		t.Fatal(err)
	}
	if last := srv.Last(); last.Path != "/release/"+mbtest.ReleaseReference {
		t.Errorf("last path = %q, want the reference release", last.Path)
	}
	if got := srv.Last().Query.Get("inc"); got != "recordings artist-credits" {
		t.Errorf("inc = %q", got)
	}
	if al.Title != "Paper Lanterns" || al.ArtistID != mbtest.ArtistLumen || al.ArtistName != "Lumen Harbor" || al.FirstReleaseDate != "2003-05-12" {
		t.Errorf("album = %+v", al)
	}
	want := []struct {
		rec, title    string
		disc, pos, ms int
	}{
		{"00000000-0000-4000-8000-0000000000e1", "Opening Tide", 1, 1, 180000},
		{"00000000-0000-4000-8000-0000000000e2", "Second Wind", 1, 2, 240000}, // recording length fallback
		{"00000000-0000-4000-8000-0000000000e3", "Silent Close", 2, 1, 0},
	}
	if len(al.Tracks) != len(want) {
		t.Fatalf("tracks = %+v", al.Tracks)
	}
	for i, w := range want {
		g := al.Tracks[i]
		if g.RecordingID != w.rec || g.Title != w.title || g.Disc != w.disc || g.Position != w.pos || g.LengthMs != w.ms {
			t.Errorf("track %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestNotFound(t *testing.T) {
	c, _ := newTestClient(t, 0)
	ctx := context.Background()
	if _, _, err := c.GetArtist(ctx, "00000000-0000-4000-8000-00000000ffff"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetArtist err = %v, want ErrNotFound", err)
	}
	if _, err := c.GetAlbum(ctx, "00000000-0000-4000-8000-00000000ffff"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAlbum err = %v, want ErrNotFound", err)
	}
}
