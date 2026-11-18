package musicbrainz

import (
	"context"
	"errors"
	"net/http"
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

func TestRetriesOnServerErrorsAndRateLimit(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusTooManyRequests} {
		c, srv := newTestClient(t, 0)
		srv.FailNextWith(status, 1)
		if _, err := c.SearchArtists(context.Background(), "lumen", 5); err != nil {
			t.Fatalf("after one %d: %v", status, err)
		}
		if got := srv.Requests(); got != 2 {
			t.Errorf("%d: requests = %d, want 2", status, got)
		}
	}

	c, srv := newTestClient(t, 0)
	srv.FailNextWith(http.StatusGatewayTimeout, 4)
	_, err := c.SearchArtists(context.Background(), "lumen", 5)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if got := srv.Requests(); got != 4 {
		t.Errorf("requests = %d, want 4 (1 try + 3 retries)", got)
	}
}

func TestNotFoundIsNotRetried(t *testing.T) {
	c, srv := newTestClient(t, 0)
	_, _, err := c.GetArtist(context.Background(), "00000000-0000-4000-8000-00000000ffff")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if got := srv.Requests(); got != 1 {
		t.Errorf("requests = %d, want exactly 1", got)
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

func TestGetArtistCapsPages(t *testing.T) {
	c, srv := newTestClient(t, 0)
	_, albums, err := c.GetArtist(context.Background(), mbtest.ArtistHuge)
	if err != nil {
		t.Fatal(err)
	}
	if want := maxArtistPages * pageSize; len(albums) != want {
		t.Fatalf("got %d albums, want %d", len(albums), want)
	}
	if got := srv.Requests(); got != 1+maxArtistPages {
		t.Errorf("requests = %d, want %d (lookup + capped pages)", got, 1+maxArtistPages)
	}
	if got := srv.Last().Query.Get("type"); got != "album|ep|single" {
		t.Errorf("browse type = %q, want album|ep|single", got)
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
		{"00000000-0000-4000-8000-0000000000e4", "Harbor Lights", 2, 2, 195000},
		{mbtest.RecordingGlassTide, "Glass Tide", 2, 3, 201000},
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

func TestFindRecordingsFieldedAndEscaped(t *testing.T) {
	c, srv := newTestClient(t, 0)
	hits, err := c.FindRecordings(context.Background(),
		RecordingQuery{Artist: `Lumen "Harbor"`, Title: `Glass\Tide`, Album: "Paper Lanterns"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	last := srv.Last()
	if last.Path != "/recording" {
		t.Errorf("path = %q", last.Path)
	}
	want := `recording:"Glass\\Tide" AND artist:"Lumen \"Harbor\"" AND release:"Paper Lanterns"`
	if got := last.Query.Get("query"); got != want {
		t.Errorf("query = %s, want %s", got, want)
	}
	if last.Query.Get("limit") != "7" {
		t.Errorf("limit = %q", last.Query.Get("limit"))
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	h := hits[0]
	if h.RecordingID != mbtest.RecordingGlassTide || h.Title != "Glass Tide" || h.ArtistName != "Lumen Harbor feat. Mira Vale" ||
		h.LengthMs != 201000 || h.Score != 100 || len(h.Releases) != 1 {
		t.Fatalf("hit = %+v", h)
	}
	r := h.Releases[0]
	if r.ReleaseGroupID != mbtest.AlbumTwoDisc || r.Title != "Paper Lanterns" || r.PrimaryType != "Album" ||
		len(r.SecondaryTypes) != 1 || r.SecondaryTypes[0] != "Compilation" || r.Date != "2003-05-12" ||
		r.Disc != 2 || r.Position != 3 {
		t.Errorf("release = %+v", r)
	}
	if hits[1].Score != 62 || len(hits[1].Releases) != 0 {
		t.Errorf("second hit = %+v", hits[1])
	}

	// A trailing backslash must not escape the closing quote.
	if _, err := c.FindRecordings(context.Background(), RecordingQuery{Artist: "AC\\", Title: "T"}, 3); err != nil {
		t.Fatal(err)
	}
	if got := srv.Last().Query.Get("query"); got != `recording:"T" AND artist:"AC\\"` {
		t.Errorf("trailing backslash: query = %s", got)
	}

	// Empty title or artist: no request at all.
	before := srv.Requests()
	for _, q := range []RecordingQuery{{Artist: "A"}, {Title: "T"}} {
		hits, err := c.FindRecordings(context.Background(), q, 3)
		if err != nil || len(hits) != 0 {
			t.Errorf("%+v: hits=%v err=%v", q, hits, err)
		}
	}
	if srv.Requests() != before {
		t.Error("empty title or artist must not call MusicBrainz")
	}

	// Without an album there is no release clause.
	if _, err := c.FindRecordings(context.Background(), RecordingQuery{Artist: "A", Title: "T"}, 3); err != nil {
		t.Fatal(err)
	}
	if got := srv.Last().Query.Get("query"); got != `recording:"T" AND artist:"A"` {
		t.Errorf("query = %s", got)
	}
}

func TestGetRecordingReleases(t *testing.T) {
	c, srv := newTestClient(t, 0)
	h, err := c.GetRecording(context.Background(), mbtest.RecordingGlassTide)
	if err != nil {
		t.Fatal(err)
	}
	last := srv.Last()
	if last.Path != "/recording/"+mbtest.RecordingGlassTide || last.Query.Get("inc") != "releases release-groups artist-credits media" {
		t.Errorf("request = %s ?%v", last.Path, last.Query)
	}
	if h.Title != "Glass Tide" || h.ArtistName != "Lumen Harbor feat. Mira Vale" || h.LengthMs != 201000 || len(h.Releases) != 1 {
		t.Fatalf("hit = %+v", h)
	}
	if r := h.Releases[0]; r.ReleaseGroupID != mbtest.AlbumTwoDisc || r.Disc != 2 || r.Position != 3 || r.Date != "2003-05-12" {
		t.Errorf("release = %+v", r)
	}
	if _, err := c.GetRecording(context.Background(), "00000000-0000-4000-8000-00000000ffff"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown recording: err = %v, want ErrNotFound", err)
	}
}
