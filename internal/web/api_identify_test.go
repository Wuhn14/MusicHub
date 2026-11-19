package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/catalog"
	"github.com/Wuhn14/MusicHub/internal/catalog/mbtest"
	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/identify"
	"github.com/Wuhn14/MusicHub/internal/library"
	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
)

// identifyEnv is the real identification stack (store, identifier against
// the fake MusicBrainz, organizer) behind the API.
type identifyEnv struct {
	st       *store.Store
	h        http.Handler
	music    string
	incoming string
	covers   string
	n        int
}

func newIdentifyEnv(t *testing.T) *identifyEnv {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	mb := mbtest.New()
	t.Cleanup(mb.Close)
	mbc := musicbrainz.New(mb.URL, "MusicHub-test/0", 0)
	e := &identifyEnv{
		st: st, music: filepath.Join(root, "music"), incoming: filepath.Join(root, "incoming"),
		covers: filepath.Join(root, "local-covers"),
	}
	for _, d := range []string{e.music, e.incoming} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	set := settings.New(st)
	org := library.NewOrganizer(st, nil, e.music, set)
	id := identify.New(identify.Deps{
		Store: st, MB: mbc, Catalog: catalog.New(st, mbc, time.Now), Settings: set, Placer: org,
	})
	e.h = NewAPI(Deps{IdentifyActions: id, IdentifyStore: st, LocalCoversDir: e.covers, Files: st, IncomingDir: e.incoming})
	return e
}

// upload records a pending uploaded file in the incoming folder.
func (e *identifyEnv) upload(t *testing.T, tags media.Tags) store.File {
	t.Helper()
	e.n++
	path := filepath.Join(e.incoming, strings.Repeat("0", 31)+string(rune('0'+e.n))+".mp3")
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := e.st.InsertUploadedFile(context.Background(), store.File{
		Path: path, Size: 5, DurationMs: 201000, Format: "mp3", Tags: tags, UploadName: "Ma chanson.mp3",
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (e *identifyEnv) file(t *testing.T, id string) store.File {
	t.Helper()
	f, err := e.st.GetFile(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestChooseCandidateIdentifiesAndPlaces(t *testing.T) {
	e := newIdentifyEnv(t)
	ctx := context.Background()
	f := e.upload(t, media.Tags{Title: "Glass Tide", Artist: "Lumen Harbor", Date: "2003-05-12"})
	if err := e.st.SetCandidates(ctx, f.ID, []store.Candidate{{
		RecordingID: mbtest.RecordingGlassTide, AlbumID: mbtest.AlbumTwoDisc, Title: "Glass Tide",
		Artist: "Lumen Harbor", Album: "Paper Lanterns", LengthMs: 201000, Score: 97, Source: store.SourceSearch,
	}}); err != nil {
		t.Fatal(err)
	}

	m := decode(t, send(t, e.h, "GET", "/api/identify", ""))
	files, _ := m["files"].([]any)
	if len(files) != 1 || m["total"] != float64(1) {
		t.Fatalf("list = %v", m)
	}
	item := files[0].(map[string]any)
	tags := item["tags"].(map[string]any)
	cands := item["candidates"].([]any)
	if item["id"] != f.ID || item["name"] != "Ma chanson.mp3" || item["origin"] != "uploaded" || item["durationMs"] != float64(201000) ||
		tags["title"] != "Glass Tide" || tags["year"] != "2003" || len(cands) != 1 || cands[0].(map[string]any)["score"] != float64(97) {
		t.Errorf("item = %v", item)
	}
	if c := decode(t, send(t, e.h, "GET", "/api/identify/count", "")); c["count"] != float64(1) {
		t.Errorf("count = %v", c)
	}

	wantError(t, send(t, e.h, "POST", "/api/identify/"+f.ID+"/choose", `{"recordingId":"nope"}`), 400, "Identifiant invalide.")
	wantError(t, send(t, e.h, "POST", "/api/identify/"+f.ID+"/choose", `{"recordingId":"`+mbtest.RecordingGlassTide+`","albumId":"x"}`), 400, "Identifiant invalide.")
	wantError(t, send(t, e.h, "POST", "/api/identify/"+f.ID+"/choose", `{"recording":1}`), 400, "Requête invalide.")
	wantError(t, send(t, e.h, "POST", "/api/identify/"+f.ID+"/choose", `{"recordingId":"00000000-0000-4000-8000-0000000000ff"}`),
		404, "Cet enregistrement est introuvable sur MusicBrainz.")

	rec := send(t, e.h, "POST", "/api/identify/"+f.ID+"/choose", `{"recordingId":"`+mbtest.RecordingGlassTide+`"}`)
	if rec.Code != 200 {
		t.Fatalf("choose = %d %s", rec.Code, rec.Body)
	}
	if out := decode(t, rec); out["status"] != "identified" || out["albumId"] != mbtest.AlbumTwoDisc {
		t.Errorf("choose body = %v", out)
	}
	got := e.file(t, f.ID)
	if got.Status != store.FileIdentified || got.RecordingID != mbtest.RecordingGlassTide || got.Origin != store.OriginUploaded {
		t.Errorf("file = %+v", got)
	}
	if !strings.HasPrefix(got.Path, e.music+string(filepath.Separator)) || !strings.HasSuffix(got.Path, "Glass Tide.mp3") {
		t.Errorf("not placed: %q", got.Path)
	}
	if _, err := os.Stat(f.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("still in incoming: %v", err)
	}
	if b, _ := os.ReadFile(got.Path); string(b) != "audio" {
		t.Errorf("content = %q", b)
	}

	wantError(t, send(t, e.h, "POST", "/api/identify/"+f.ID+"/choose", `{"recordingId":"`+mbtest.RecordingGlassTide+`"}`),
		409, "Ce fichier n'est plus à identifier.")
	if c := decode(t, send(t, e.h, "GET", "/api/identify/count", "")); c["count"] != float64(0) {
		t.Errorf("count after = %v", c)
	}
	// The upload page now reads what the file became.
	if s := decode(t, send(t, e.h, "GET", "/api/files/"+f.ID, "")); s["status"] != "identified" || s["title"] != "Glass Tide" {
		t.Errorf("file status = %v", s)
	}
}

func TestLocalTrackRequiresTitleAndArtist(t *testing.T) {
	e := newIdentifyEnv(t)
	f := e.upload(t, media.Tags{})
	url := "/api/identify/" + f.ID + "/local"
	wantError(t, send(t, e.h, "POST", url, `{"artist":"Moi"}`), 400, "Le titre et l'artiste sont obligatoires.")
	wantError(t, send(t, e.h, "POST", url, `{"title":"Démo","artist":"   "}`), 400, "Le titre et l'artiste sont obligatoires.")
	wantError(t, send(t, e.h, "POST", url, `{"title":"Démo","artist":"Moi","year":99}`), 400, "Année invalide (de 1000 à 9999).")
	wantError(t, send(t, e.h, "POST", url, `{"title":"Démo","artist":"Moi","disc":-1}`), 400, "Numéro de disque invalide (de 1 à 99).")
	wantError(t, send(t, e.h, "POST", url, `{"title":"Démo","artist":"Moi","position":1000}`), 400, "Numéro de piste invalide (de 1 à 999).")
	wantError(t, send(t, e.h, "POST", url, `{"title":"`+strings.Repeat("a", 301)+`","artist":"Moi"}`), 400, "Texte trop long (300 caractères au maximum).")
	wantError(t, send(t, e.h, "POST", url, `{"title":"Démo","artist":"Moi","extra":1}`), 400, "Requête invalide.")
	if g := e.file(t, f.ID); g.Status != store.FilePending {
		t.Fatalf("rejected input changed the file: %+v", g)
	}

	rec := send(t, e.h, "POST", url, `{"title":" Démo ","artist":"Moi","album":"Maison","albumArtist":"","disc":1,"position":3,"year":2024}`)
	if rec.Code != 201 {
		t.Fatalf("local = %d %s", rec.Code, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	g := e.file(t, f.ID)
	if out["status"] != "local" || g.Status != store.FileLocal || g.LocalTrackID != out["localTrackId"] {
		t.Fatalf("out %v, file %+v", out, g)
	}
	want := filepath.Join(e.music, "Moi", "Maison (2024)", "03 - Démo.mp3")
	if g.Path != want {
		t.Errorf("path = %q, want %q", g.Path, want)
	}
	lt, _ := e.st.GetLocalTrack(context.Background(), g.LocalTrackID)
	if lt.Title != "Démo" || lt.Year != 2024 || lt.Position != 3 {
		t.Errorf("local track = %+v", lt)
	}
	wantError(t, send(t, e.h, "POST", url, `{"title":"Démo","artist":"Moi"}`), 409, "Ce fichier n'est plus à identifier.")
	wantError(t, send(t, e.h, "POST", "/api/identify/"+strings.Repeat("f", 32)+"/local", `{"title":"a","artist":"b"}`), 404, "Introuvable.")
}

func TestDeleteDroppedIs409(t *testing.T) {
	e := newIdentifyEnv(t)
	ctx := context.Background()
	dropPath := filepath.Join(e.music, "à moi.mp3")
	os.WriteFile(dropPath, []byte("mine"), 0o644)
	dropped, err := e.st.UpsertScannedFile(ctx, store.File{Path: dropPath, Status: store.FilePending, LastSeen: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, send(t, e.h, "DELETE", "/api/identify/"+dropped.ID, ""), 409,
		"Ce fichier est dans ton dossier musique : supprime-le directement sur le disque.")
	if _, err := os.Stat(dropPath); err != nil {
		t.Errorf("dropped file touched: %v", err)
	}

	up := e.upload(t, media.Tags{})
	if rec := send(t, e.h, "DELETE", "/api/identify/"+up.ID, ""); rec.Code != 200 {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(up.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("uploaded file still on disk: %v", err)
	}
	if _, err := e.st.GetFile(ctx, up.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("row kept: %v", err)
	}
	wantError(t, send(t, e.h, "DELETE", "/api/identify/"+up.ID, ""), 404, "Introuvable.")
	wantError(t, send(t, e.h, "DELETE", "/api/identify/zz", ""), 400, "Identifiant invalide.")

	// An uploaded file outside the incoming folder is never deleted.
	outside := filepath.Join(e.music, "envoyé.mp3")
	os.WriteFile(outside, []byte("kept"), 0o644)
	stray, err := e.st.InsertUploadedFile(ctx, store.File{Path: outside})
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, send(t, e.h, "DELETE", "/api/identify/"+stray.ID, ""), 409,
		"Ce fichier est dans ton dossier musique : supprime-le directement sur le disque.")
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("file outside incoming removed: %v", err)
	}
	if _, err := e.st.GetFile(ctx, stray.ID); err != nil {
		t.Errorf("row removed: %v", err)
	}

	// An uploaded file that is no longer pending stays.
	done := e.upload(t, media.Tags{})
	e.st.SetFileStatus(ctx, done.ID, store.FileIdentified)
	wantError(t, send(t, e.h, "DELETE", "/api/identify/"+done.ID, ""), 409, "Ce fichier n'est plus à identifier.")
	if _, err := os.Stat(done.Path); err != nil {
		t.Errorf("identified upload removed: %v", err)
	}
}

var pngHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}

func postCover(h http.Handler, fileID, ctype string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/identify/"+fileID+"/cover", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCoverRejectsNonImage(t *testing.T) {
	e := newIdentifyEnv(t)
	f := e.upload(t, media.Tags{})
	// Only a local track can have a cover.
	wantError(t, postCover(e.h, f.ID, "image/png", pngHeader), 409, "Ce fichier n'est pas un titre local.")
	if rec := send(t, e.h, "POST", "/api/identify/"+f.ID+"/local", `{"title":"T","artist":"A"}`); rec.Code != 201 {
		t.Fatalf("local = %d", rec.Code)
	}
	lt := e.file(t, f.ID).LocalTrackID

	// The declared type is ignored: the content decides.
	wantError(t, postCover(e.h, f.ID, "image/jpeg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")), 415, "Image non prise en charge (JPEG, PNG ou WebP).")
	wantError(t, postCover(e.h, f.ID, "image/gif", []byte("GIF89a......")), 415, "Image non prise en charge (JPEG, PNG ou WebP).")
	big := append(append([]byte{}, pngHeader...), make([]byte, 5<<20)...)
	wantError(t, postCover(e.h, f.ID, "image/png", big), 413, "Image trop volumineuse (5 Mo au maximum).")
	if _, err := e.st.GetLocalTrack(context.Background(), lt); err != nil {
		t.Fatal(err)
	}
	if rec := send(t, e.h, "GET", "/api/local-covers/"+lt, ""); rec.Code != 404 {
		t.Errorf("cover before upload = %d", rec.Code)
	}

	rec := postCover(e.h, f.ID, "text/plain", pngHeader)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "/api/local-covers/"+lt) {
		t.Fatalf("png = %d %s", rec.Code, rec.Body)
	}
	got := send(t, e.h, "GET", "/api/local-covers/"+lt, "")
	if got.Code != 200 || got.Header().Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(got.Header().Get("Cache-Control"), "private") || got.Body.Len() != len(pngHeader) {
		t.Errorf("serve = %d %v", got.Code, got.Header())
	}

	// A WebP replaces the PNG.
	webp := []byte("RIFF\x10\x00\x00\x00WEBPVP8 ")
	if rec := postCover(e.h, f.ID, "application/octet-stream", webp); rec.Code != 200 {
		t.Fatalf("webp = %d", rec.Code)
	}
	if got := send(t, e.h, "GET", "/api/local-covers/"+lt, ""); got.Header().Get("Content-Type") != "image/webp" {
		t.Errorf("after replace = %v", got.Header())
	}
	if _, err := os.Stat(filepath.Join(e.covers, lt+".png")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old png kept: %v", err)
	}
	if rec := send(t, e.h, "GET", "/api/local-covers/x.png", ""); rec.Code != 400 {
		t.Errorf("bad id = %d", rec.Code)
	}
}

func TestIdentifyRoutesNeedDeps(t *testing.T) {
	h := NewAPI(Deps{})
	for _, r := range [][2]string{
		{"GET", "/api/identify"}, {"GET", "/api/identify/count"},
		{"POST", "/api/identify/" + strings.Repeat("a", 32) + "/choose"},
		{"POST", "/api/identify/" + strings.Repeat("a", 32) + "/local"},
		{"POST", "/api/identify/" + strings.Repeat("a", 32) + "/cover"},
		{"DELETE", "/api/identify/" + strings.Repeat("a", 32)},
		{"GET", "/api/local-covers/" + strings.Repeat("a", 32)},
	} {
		if rec := send(t, h, r[0], r[1], ""); rec.Code != 503 {
			t.Errorf("%s %s = %d", r[0], r[1], rec.Code)
		}
	}
}
