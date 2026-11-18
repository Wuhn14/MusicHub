package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wuhn14/MusicHub/internal/store"
	"github.com/Wuhn14/MusicHub/internal/upload"
)

type fakeUpload struct {
	err     error
	gotName string
	max     int64
}

func (f *fakeUpload) MaxBytes(context.Context) int64 { return f.max }
func (f *fakeUpload) Receive(_ context.Context, name string, r io.Reader) (store.File, error) {
	f.gotName = name
	if _, err := io.Copy(io.Discard, r); err != nil {
		return store.File{}, upload.ErrTooLarge
	}
	if f.err != nil {
		return store.File{}, f.err
	}
	return store.File{ID: "abc", Status: store.FilePending, DurationMs: 1234, Format: "mp3"}, nil
}

func postUpload(h http.Handler, name, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/uploads", strings.NewReader(body))
	if name != "" {
		req.Header.Set("X-File-Name", name)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAPIUploadStatusCodes(t *testing.T) {
	fu := &fakeUpload{max: 2 << 30}
	h := NewAPI(Deps{Upload: fu})

	rec := postUpload(h, "Ma%20chanson%20%C3%A9t%C3%A9.mp3", "data")
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"id":"abc"`) || !strings.Contains(rec.Body.String(), `"name":"Ma chanson été.mp3"`) {
		t.Errorf("201: %d %s", rec.Code, rec.Body)
	}
	if fu.gotName != "Ma chanson été.mp3" {
		t.Errorf("decoded name %q", fu.gotName)
	}
	if rec := postUpload(h, "%zz.mp3", "data"); rec.Code != 400 {
		t.Errorf("bad escape: %d", rec.Code)
	}
	if rec := postUpload(h, "", "data"); rec.Code != 400 {
		t.Errorf("missing name: %d", rec.Code)
	}
	if rec := postUpload(h, "a.mp3", ""); rec.Code != 400 {
		t.Errorf("empty body: %d", rec.Code)
	}
	fu.err = upload.ErrUnsupportedFormat
	if rec := postUpload(h, "a.txt", "x"); rec.Code != 415 || !strings.Contains(rec.Body.String(), "Format non pris en charge.") {
		t.Errorf("415: %d %s", rec.Code, rec.Body)
	}
	fu.err = upload.ErrNotAudio
	if rec := postUpload(h, "a.mp3", "x"); rec.Code != 415 || !strings.Contains(rec.Body.String(), "Ce fichier n'est pas un fichier audio lisible.") {
		t.Errorf("not audio: %d %s", rec.Code, rec.Body)
	}
	fu.err = errors.New("boom")
	if rec := postUpload(h, "a.mp3", "x"); rec.Code != 500 || !strings.Contains(rec.Body.String(), "Analyse du fichier impossible.") {
		t.Errorf("500: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, NewAPI(Deps{}), "POST", "/api/uploads", "x"); rec.Code != 503 {
		t.Errorf("unwired: %d", rec.Code)
	}
}

func TestAPIUploadTooLarge(t *testing.T) {
	fu := &fakeUpload{max: 2 << 30, err: upload.ErrTooLarge}
	h := NewAPI(Deps{Upload: fu})
	rec := postUpload(h, "a.mp3", "x")
	if rec.Code != 413 || !strings.Contains(rec.Body.String(), "Fichier trop volumineux (maximum : 2 Go).") {
		t.Errorf("413: %d %s", rec.Code, rec.Body)
	}
	fu.max = 1536 << 20
	if rec := postUpload(h, "a.mp3", "x"); !strings.Contains(rec.Body.String(), "(maximum : 1,5 Go).") {
		t.Errorf("decimal comma: %s", rec.Body)
	}
	fu.max = 10 << 20
	if rec := postUpload(h, "a.mp3", "x"); !strings.Contains(rec.Body.String(), "(maximum : 10 Mo).") {
		t.Errorf("Mo: %s", rec.Body)
	}
	// The body is cut at the limit.
	fu.err, fu.max = nil, 3
	if rec := postUpload(h, "a.mp3", "12345"); rec.Code != 413 {
		t.Errorf("oversized body: %d", rec.Code)
	}
}

func TestAPIFileStatus(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f, err := st.InsertUploadedFile(context.Background(), store.File{Path: "/incoming/a.mp3"})
	if err != nil {
		t.Fatal(err)
	}
	h := NewAPI(Deps{Files: st})
	m := decode(t, send(t, h, "GET", "/api/files/"+f.ID, ""))
	if m["status"] != "pending" || m["id"] != f.ID {
		t.Errorf("status body: %v", m)
	}
	if rec := send(t, h, "GET", "/api/files/nope", ""); rec.Code != 404 {
		t.Errorf("unknown: %d", rec.Code)
	}
}
