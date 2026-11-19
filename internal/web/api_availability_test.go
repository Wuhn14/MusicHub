package web

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/identify"
	"github.com/Wuhn14/MusicHub/internal/store"
)

type fakeIdentify struct{ s identify.WorkerStatus }

func (f fakeIdentify) Status() identify.WorkerStatus { return f.s }

const (
	availArtist = "00000000-0000-4000-8000-0000000000a1"
	availAlbum  = "00000000-0000-4000-8000-0000000000b1"
	availOther  = "00000000-0000-4000-8000-0000000000b2"
	availRec1   = "00000000-0000-4000-8000-0000000000e1"
	availRec2   = "00000000-0000-4000-8000-0000000000e2"
)

func newAvailabilityAPI(t *testing.T) (http.Handler, map[string]string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	for _, al := range []store.Album{
		{ID: availAlbum, Title: "Paper Lanterns", ArtistID: availArtist, FetchedAt: now, Tracks: []store.Track{
			{RecordingID: availRec1, Title: "One", Disc: 1, Position: 1},
			{RecordingID: availRec2, Title: "Two", Disc: 1, Position: 2},
		}},
		{ID: availOther, Title: "Best Of", ArtistID: availArtist, FetchedAt: now, Tracks: []store.Track{
			{RecordingID: availRec2, Title: "Two", Disc: 1, Position: 1},
		}},
	} {
		if err := st.UpsertAlbum(ctx, al); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{}
	for _, f := range []struct{ path, rec, album string }{
		{"/m/1.flac", availRec1, availAlbum},
		{"/m/2.flac", availRec2, availOther}, // owned through the compilation
	} {
		row, err := st.UpsertScannedFile(ctx, store.File{Path: f.path, LastSeen: now})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.LinkFile(ctx, row.ID, f.rec, f.album, 1, 1); err != nil {
			t.Fatal(err)
		}
		files[f.rec] = row.ID
	}
	h := NewAPI(Deps{Availability: st, Identify: fakeIdentify{identify.WorkerStatus{Queued: 3, Done: 7, Failed: 1}}})
	return h, files
}

func TestAPIAvailability(t *testing.T) {
	h, files := newAvailabilityAPI(t)

	rec := do(t, h, http.MethodGet, "/api/library/availability?album="+availAlbum)
	if rec.Code != http.StatusOK {
		t.Fatalf("album status = %d (%s)", rec.Code, rec.Body)
	}
	var byAlbum struct {
		Tracks map[string]string `json:"tracks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &byAlbum); err != nil {
		t.Fatal(err)
	}
	if len(byAlbum.Tracks) != 2 || byAlbum.Tracks[availRec1] != files[availRec1] || byAlbum.Tracks[availRec2] != files[availRec2] {
		t.Errorf("album availability = %v, want %v", byAlbum.Tracks, files)
	}

	rec = do(t, h, http.MethodGet, "/api/library/availability?artist="+availArtist)
	var byArtist struct {
		Albums map[string]int `json:"albums"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &byArtist); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("artist = %d %s, %v", rec.Code, rec.Body, err)
	}
	if byArtist.Albums[availAlbum] != 2 || byArtist.Albums[availOther] != 1 {
		t.Errorf("artist availability = %v", byArtist.Albums)
	}

	// Unknown ids answer empty objects, not null.
	rec = do(t, h, http.MethodGet, "/api/library/availability?album=00000000-0000-4000-8000-0000000000ff")
	if body := rec.Body.String(); rec.Code != http.StatusOK || body != "{\"tracks\":{}}\n" {
		t.Errorf("unknown album = %d %q", rec.Code, body)
	}
	rec = do(t, h, http.MethodGet, "/api/library/availability?artist=00000000-0000-4000-8000-0000000000ff")
	if body := rec.Body.String(); rec.Code != http.StatusOK || body != "{\"albums\":{}}\n" {
		t.Errorf("unknown artist = %d %q", rec.Code, body)
	}
}

func TestAPIAvailabilityErrors(t *testing.T) {
	h, _ := newAvailabilityAPI(t)
	wantError(t, do(t, h, http.MethodGet, "/api/library/availability?album=not-a-uuid"), http.StatusBadRequest, "Identifiant invalide.")
	wantError(t, do(t, h, http.MethodGet, "/api/library/availability?artist=../x"), http.StatusBadRequest, "Identifiant invalide.")
	wantError(t, do(t, h, http.MethodGet, "/api/library/availability"), http.StatusBadRequest, "Indique un album ou un artiste.")
	wantError(t, do(t, h, http.MethodGet, "/api/library/availability?album="+availAlbum+"&artist="+availArtist),
		http.StatusBadRequest, "Indique un album ou un artiste.")

	unwired := NewAPI(Deps{})
	wantError(t, do(t, unwired, http.MethodGet, "/api/library/availability?album="+availAlbum), http.StatusServiceUnavailable, "Fonction indisponible.")
	wantError(t, do(t, unwired, http.MethodGet, "/api/identify/status"), http.StatusServiceUnavailable, "Fonction indisponible.")
}

func TestAPIIdentifyStatus(t *testing.T) {
	h, _ := newAvailabilityAPI(t)
	rec := do(t, h, http.MethodGet, "/api/identify/status")
	if body := rec.Body.String(); rec.Code != http.StatusOK || body != "{\"queued\":3,\"done\":7,\"failed\":1}\n" {
		t.Errorf("identify status = %d %q", rec.Code, body)
	}
}
