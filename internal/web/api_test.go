package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/catalog"
	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/store"
)

const (
	artistID = "11111111-2222-3333-4444-555555555555"
	albumID  = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

type fakeCatalog struct {
	search    func(ctx context.Context, q string) (catalog.SearchResult, error)
	artist    func(ctx context.Context, id string) (store.Artist, []store.Album, error)
	album     func(ctx context.Context, id string) (store.Album, error)
	searchedQ *string
}

func (f fakeCatalog) Search(ctx context.Context, q string) (catalog.SearchResult, error) {
	if f.searchedQ != nil {
		*f.searchedQ = q
	}
	if f.search == nil {
		return catalog.SearchResult{}, nil
	}
	return f.search(ctx, q)
}

func (f fakeCatalog) Artist(ctx context.Context, id string) (store.Artist, []store.Album, error) {
	return f.artist(ctx, id)
}

func (f fakeCatalog) Album(ctx context.Context, id string) (store.Album, error) {
	return f.album(ctx, id)
}

type fakeCovers struct {
	get     func(ctx context.Context, id string, size int) (string, error)
	gotSize *int
}

func (f fakeCovers) Get(ctx context.Context, id string, size int) (string, error) {
	if f.gotSize != nil {
		*f.gotSize = size
	}
	return f.get(ctx, id, size)
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	if got := rec.Body.String(); strings.TrimSpace(got) != `{"error":"`+msg+`"}` {
		t.Fatalf("body = %s, want error %q", got, msg)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestSearch(t *testing.T) {
	var q string
	cat := fakeCatalog{searchedQ: &q, search: func(context.Context, string) (catalog.SearchResult, error) {
		return catalog.SearchResult{
			Artists: []store.Artist{{ID: artistID, Name: "Les Exemples", Disambiguation: "fictif", Country: "FR"}},
			Albums:  []store.Album{{ID: albumID, Title: "Premier", ArtistID: artistID, ArtistName: "Les Exemples", PrimaryType: "Album", FirstReleaseDate: "2001-02-03"}},
			Tracks: []musicbrainz.TrackHit{{
				Track:   store.Track{RecordingID: "r1", Title: "Intro", LengthMs: 1234},
				AlbumID: albumID, AlbumTitle: "Premier", ArtistName: "Les Exemples",
			}},
		}, nil
	}}
	rec := do(t, NewAPI(cat, fakeCovers{}), "GET", "/api/search?q=exemples")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if q != "exemples" {
		t.Fatalf("catalog got q=%q", q)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	var got struct {
		Artists []map[string]any `json:"artists"`
		Albums  []map[string]any `json:"albums"`
		Tracks  []map[string]any `json:"tracks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Artists) != 1 || got.Artists[0]["name"] != "Les Exemples" || got.Artists[0]["id"] != artistID ||
		got.Artists[0]["disambiguation"] != "fictif" || got.Artists[0]["country"] != "FR" {
		t.Fatalf("artists = %v", got.Artists)
	}
	a := got.Albums[0]
	if a["title"] != "Premier" || a["artistId"] != artistID || a["artistName"] != "Les Exemples" ||
		a["primaryType"] != "Album" || a["firstReleaseDate"] != "2001-02-03" {
		t.Fatalf("album = %v", a)
	}
	if sec, ok := a["secondaryTypes"].([]any); !ok || len(sec) != 0 {
		t.Fatalf("secondaryTypes should be [], got %v", a["secondaryTypes"])
	}
	tr := got.Tracks[0]
	if tr["recordingId"] != "r1" || tr["title"] != "Intro" || tr["lengthMs"] != float64(1234) ||
		tr["albumId"] != albumID || tr["albumTitle"] != "Premier" || tr["artistName"] != "Les Exemples" {
		t.Fatalf("track = %v", tr)
	}
}

func TestSearchEmptyEncodesEmptyArrays(t *testing.T) {
	rec := do(t, NewAPI(fakeCatalog{}, fakeCovers{}), "GET", "/api/search")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"artists":[],"albums":[],"tracks":[]}` {
		t.Fatalf("body = %s", got)
	}
}

func TestSearchRejectsLongQuery(t *testing.T) {
	h := NewAPI(fakeCatalog{}, fakeCovers{})
	wantError(t, do(t, h, "GET", "/api/search?q="+strings.Repeat("a", 201)), 400, "Recherche trop longue.")
	if rec := do(t, h, "GET", "/api/search?q="+strings.Repeat("é", 200)); rec.Code != 200 {
		t.Fatalf("200 runes should pass, got %d", rec.Code)
	}
}

func TestArtist(t *testing.T) {
	cat := fakeCatalog{artist: func(_ context.Context, id string) (store.Artist, []store.Album, error) {
		if id != artistID {
			t.Errorf("id = %q", id)
		}
		return store.Artist{ID: artistID, Name: "Les Exemples", SortName: "Exemples, Les", Country: "FR"},
			[]store.Album{{ID: albumID, Title: "Premier", SecondaryTypes: []string{"Live"}}}, nil
	}}
	rec := do(t, NewAPI(cat, fakeCovers{}), "GET", "/api/artists/"+artistID)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Artist map[string]any   `json:"artist"`
		Albums []map[string]any `json:"albums"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Artist["sortName"] != "Exemples, Les" || got.Artist["name"] != "Les Exemples" {
		t.Fatalf("artist = %v", got.Artist)
	}
	if len(got.Albums) != 1 || got.Albums[0]["title"] != "Premier" {
		t.Fatalf("albums = %v", got.Albums)
	}
	if strings.Contains(rec.Body.String(), "FetchedAt") {
		t.Fatal("store fields leaked")
	}
}

func TestArtistWithoutAlbumsEncodesEmptyArray(t *testing.T) {
	cat := fakeCatalog{artist: func(context.Context, string) (store.Artist, []store.Album, error) {
		return store.Artist{ID: artistID}, nil, nil
	}}
	rec := do(t, NewAPI(cat, fakeCovers{}), "GET", "/api/artists/"+artistID)
	if !strings.Contains(rec.Body.String(), `"albums":[]`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestAlbum(t *testing.T) {
	cat := fakeCatalog{album: func(_ context.Context, id string) (store.Album, error) {
		return store.Album{
			ID: albumID, Title: "Premier", ArtistID: artistID, ArtistName: "Les Exemples", PrimaryType: "Album",
			Tracks: []store.Track{
				{RecordingID: "r1", Title: "Intro", Disc: 1, Position: 1, LengthMs: 1000},
				{RecordingID: "r2", Title: "Suite", Disc: 1, Position: 2, LengthMs: 2000},
			},
		}, nil
	}}
	rec := do(t, NewAPI(cat, fakeCovers{}), "GET", "/api/albums/"+albumID)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Title          string           `json:"title"`
		SecondaryTypes []string         `json:"secondaryTypes"`
		Tracks         []map[string]any `json:"tracks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Premier" || got.SecondaryTypes == nil || len(got.Tracks) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got.Tracks[1]["recordingId"] != "r2" || got.Tracks[1]["position"] != float64(2) ||
		got.Tracks[1]["disc"] != float64(1) || got.Tracks[1]["lengthMs"] != float64(2000) {
		t.Fatalf("track = %v", got.Tracks[1])
	}
}

func TestRejectsInvalidMBID(t *testing.T) {
	fail := func(context.Context, string) (store.Album, error) {
		t.Error("catalog must not be called")
		return store.Album{}, nil
	}
	h := NewAPI(fakeCatalog{album: fail}, fakeCovers{get: func(context.Context, string, int) (string, error) {
		t.Error("covers must not be called")
		return "", nil
	}})
	for _, p := range []string{
		"/api/albums/not-a-uuid",
		"/api/albums/..%2F..%2Fetc",
		"/api/albums/" + strings.ToUpper(albumID),
		"/api/artists/xyz",
		"/api/covers/..%2F..%2Fetc",
		"/api/covers/" + albumID + "x",
	} {
		wantError(t, do(t, h, "GET", p), 400, "Identifiant invalide.")
	}
}

func TestNotFoundMapsTo404(t *testing.T) {
	for _, e := range []error{store.ErrNotFound, musicbrainz.ErrNotFound} {
		cat := fakeCatalog{album: func(context.Context, string) (store.Album, error) { return store.Album{}, e }}
		wantError(t, do(t, NewAPI(cat, fakeCovers{}), "GET", "/api/albums/"+albumID), 404, "Introuvable.")
	}
}

func TestUnavailableMapsTo502(t *testing.T) {
	cat := fakeCatalog{search: func(context.Context, string) (catalog.SearchResult, error) {
		return catalog.SearchResult{}, errors.Join(errors.New("boom"), musicbrainz.ErrUnavailable)
	}}
	wantError(t, do(t, NewAPI(cat, fakeCovers{}), "GET", "/api/search?q=x"), 502,
		"MusicBrainz est momentanément indisponible, réessaie dans un instant.")
}

func TestOtherErrorIs500WithoutDetail(t *testing.T) {
	cat := fakeCatalog{album: func(context.Context, string) (store.Album, error) {
		return store.Album{}, errors.New("secret sqlite detail")
	}}
	rec := do(t, NewAPI(cat, fakeCovers{}), "GET", "/api/albums/"+albumID)
	wantError(t, rec, 500, "Erreur interne.")
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("internal detail leaked")
	}
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	h := NewAPI(fakeCatalog{}, fakeCovers{})
	wantError(t, do(t, h, "GET", "/api/nope"), 404, "Introuvable.")
	wantError(t, do(t, h, "GET", "/api/albums/"), 404, "Introuvable.")
}

func TestCoverServesImageWithCacheHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.jpg")
	data := []byte("\xff\xd8\xff fake jpeg")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var size int
	h := NewAPI(fakeCatalog{}, fakeCovers{gotSize: &size, get: func(context.Context, string, int) (string, error) {
		return path, nil
	}})
	rec := do(t, h, "GET", "/api/covers/"+albumID+"?size=500")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if size != 500 {
		t.Fatalf("size = %d", size)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=604800" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	if rec.Body.String() != string(data) {
		t.Fatal("body mismatch")
	}
	rec = do(t, h, "GET", "/api/covers/"+albumID+"?size=abc")
	if rec.Code != 200 {
		t.Fatalf("invalid size should still serve, got %d", rec.Code)
	}
}

func TestMissingCoverIs404(t *testing.T) {
	h := NewAPI(fakeCatalog{}, fakeCovers{get: func(context.Context, string, int) (string, error) {
		return "", catalog.ErrNoCover
	}})
	wantError(t, do(t, h, "GET", "/api/covers/"+albumID), 404, "Introuvable.")
}

func TestCoverUnavailableMapsTo502(t *testing.T) {
	h := NewAPI(fakeCatalog{}, fakeCovers{get: func(context.Context, string, int) (string, error) {
		return "", fmt.Errorf("%w: unexpected status 503", catalog.ErrCoverUnavailable)
	}})
	wantError(t, do(t, h, "GET", "/api/covers/"+albumID), 502, "Les pochettes sont momentanément indisponibles.")
}

func TestCoverFileVanishedIs404(t *testing.T) {
	h := NewAPI(fakeCatalog{}, fakeCovers{get: func(context.Context, string, int) (string, error) {
		return filepath.Join(t.TempDir(), "gone.jpg"), nil
	}})
	wantError(t, do(t, h, "GET", "/api/covers/"+albumID), 404, "Introuvable.")
}

func TestCatalogDeadlineMapsTo502(t *testing.T) {
	var deadline time.Time
	cat := fakeCatalog{album: func(ctx context.Context, _ string) (store.Album, error) {
		deadline, _ = ctx.Deadline()
		return store.Album{}, fmt.Errorf("waiting for MusicBrainz: %w", context.DeadlineExceeded)
	}}
	start := time.Now()
	rec := do(t, NewAPI(cat, fakeCovers{}), "GET", "/api/albums/"+albumID)
	wantError(t, rec, 502, "MusicBrainz est momentanément indisponible, réessaie dans un instant.")
	if got := deadline.Sub(start); got < 50*time.Second || got > 61*time.Second {
		t.Errorf("catalog call deadline = %v from now, want about 60s", got)
	}
}
