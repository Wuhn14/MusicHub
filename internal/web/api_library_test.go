package web

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/library"
	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/store"
)

type fakeRunner struct {
	triggered int
	status    library.RunnerStatus
}

func (f *fakeRunner) Trigger()                     { f.triggered++ }
func (f *fakeRunner) Status() library.RunnerStatus { return f.status }

type fakeLibrary struct {
	*library.Query
	*fakeRunner
}

func newLibraryAPI(t *testing.T) (http.Handler, *fakeRunner) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	for _, f := range []store.File{
		{Path: "/m/1.mp3", Status: store.FilePending, Tags: media.Tags{Title: "One", Artist: "Test Band", Album: "Demo", Track: 1}, DurationMs: 61000},
		{Path: "/m/2.mp3", Status: store.FileLocal, Tags: media.Tags{Title: "Two", Artist: "Test Band", Album: "Demo", Track: 2}},
		{Path: "/m/3.mp3", Status: store.FileHidden, Tags: media.Tags{Title: "Hidden"}},
	} {
		f.LastSeen, f.Mtime = now, now
		if _, err := st.UpsertScannedFile(t.Context(), f); err != nil {
			t.Fatal(err)
		}
	}
	r := &fakeRunner{}
	return NewAPI(Deps{Library: fakeLibrary{library.NewQuery(st), r}}), r
}

func TestAPILibraryRoutes(t *testing.T) {
	h, _ := newLibraryAPI(t)
	albums := decode(t, send(t, h, "GET", "/api/library/albums?sort=name", ""))["albums"].([]any)
	if len(albums) != 1 {
		t.Fatalf("albums = %v", albums)
	}
	al := albums[0].(map[string]any)
	if al["title"] != "Demo" || al["artist"] != "Test Band" || al["trackCount"] != float64(2) || al["local"] != true || al["coverUrl"] != "" {
		t.Fatalf("album = %v", al)
	}
	detail := decode(t, send(t, h, "GET", "/api/library/albums/"+al["key"].(string), ""))
	tracks := detail["tracks"].([]any)
	if detail["title"] != "Demo" || len(tracks) != 2 || tracks[0].(map[string]any)["title"] != "One" ||
		tracks[0].(map[string]any)["durationMs"] != float64(61000) || tracks[1].(map[string]any)["local"] != true {
		t.Fatalf("detail = %v", detail)
	}
	artists := decode(t, send(t, h, "GET", "/api/library/artists", ""))["artists"].([]any)
	if len(artists) != 1 || artists[0].(map[string]any)["trackCount"] != float64(2) {
		t.Fatalf("artists = %v", artists)
	}
	got := decode(t, send(t, h, "GET", "/api/library/tracks?q=tw", ""))["tracks"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["title"] != "Two" {
		t.Fatalf("tracks = %v", got)
	}
	if rec := send(t, h, "GET", "/api/library/tracks?q=nothing", ""); rec.Body.String() != "{\"tracks\":[]}\n" {
		t.Errorf("empty tracks = %q", rec.Body)
	}
}

func TestAPILibraryErrors(t *testing.T) {
	h, _ := newLibraryAPI(t)
	for target, want := range map[string]int{
		"/api/library/albums/bm90LWEta2V5": http.StatusNotFound,
		"/api/library/albums/%21%21":       http.StatusNotFound,
		"/api/library/albums?sort=bogus":   http.StatusBadRequest,
		"/api/library/tracks?sort=bogus":   http.StatusBadRequest,
	} {
		if rec := send(t, h, "GET", target, ""); rec.Code != want {
			t.Errorf("GET %s = %d, want %d", target, rec.Code, want)
		}
	}
	nolib := NewAPI(Deps{})
	if rec := send(t, nolib, "GET", "/api/library/albums", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil library = %d", rec.Code)
	}
}

func TestAPILibraryScanAndStatus(t *testing.T) {
	h, r := newLibraryAPI(t)
	rec := send(t, h, "POST", "/api/library/scan", "")
	if rec.Code != http.StatusAccepted || rec.Body.String() != "{}\n" || r.triggered != 1 {
		t.Fatalf("scan = %d %q triggered=%d", rec.Code, rec.Body, r.triggered)
	}
	r.status = library.RunnerStatus{Running: true, LastError: "boom",
		LastRun: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC), LastReport: library.Report{Seen: 12, Unreadable: 2, Skipped: 3}}
	st := decode(t, send(t, h, "GET", "/api/library/status", ""))
	rep := st["lastReport"].(map[string]any)
	if st["running"] != true || st["lastError"] != "boom" || st["lastRun"] != "2026-04-01T12:00:00Z" ||
		rep["seen"] != float64(12) || rep["unreadable"] != float64(2) || rep["errors"] != float64(0) || rep["skipped"] != float64(3) {
		t.Fatalf("status = %v", st)
	}
	if rec := send(t, h, "GET", "/api/library/scan", ""); rec.Code == http.StatusAccepted {
		t.Errorf("GET must not trigger a scan: %d", rec.Code)
	}
}
