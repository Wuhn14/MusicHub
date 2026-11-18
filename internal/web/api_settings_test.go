package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
)

type fakeTools struct{ st []media.ToolStatus }

func (f fakeTools) Status(context.Context) []media.ToolStatus { return f.st }

func newSettingsAPI(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewAPI(Deps{Settings: settings.New(st)})
}

func send(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAPIMasksAcoustIDKey(t *testing.T) {
	h := newSettingsAPI(t)
	got := decode(t, send(t, h, "GET", "/api/settings", ""))
	if got["acoustidKey"] != "" || got["acoustidKeySet"] != false || got["scanIntervalMinutes"] != float64(15) ||
		got["duplicates"] != "best" || got["writeTagsUploaded"] != true || got["maxTranscodes"] != float64(3) ||
		got["maxUploadBytes"] != float64(2<<30) {
		t.Fatalf("defaults = %v", got)
	}
	rec := send(t, h, "PUT", "/api/settings", `{"acoustidKey":"abcd1234efgh5678"}`)
	got = decode(t, rec)
	if got["acoustidKey"] != "••••5678" || got["acoustidKeySet"] != true {
		t.Fatalf("PUT response = %v", got)
	}
	if strings.Contains(rec.Body.String(), "abcd1234") {
		t.Fatal("full key leaked in PUT response")
	}
	rec = send(t, h, "GET", "/api/settings", "")
	if strings.Contains(rec.Body.String(), "abcd1234") || decode(t, rec)["acoustidKey"] != "••••5678" {
		t.Fatalf("GET = %s", rec.Body)
	}
}

func TestAPIKeepsKeyWhenAbsent(t *testing.T) {
	h := newSettingsAPI(t)
	decode(t, send(t, h, "PUT", "/api/settings", `{"acoustidKey":"abcd1234efgh5678"}`))
	got := decode(t, send(t, h, "PUT", "/api/settings", `{"scanIntervalMinutes":30,"duplicates":"keep-both"}`))
	if got["acoustidKey"] != "••••5678" || got["scanIntervalMinutes"] != float64(30) || got["duplicates"] != "keep-both" {
		t.Fatalf("after partial PUT = %v", got)
	}
	got = decode(t, send(t, h, "PUT", "/api/settings", `{"acoustidKey":""}`))
	if got["acoustidKey"] != "" || got["acoustidKeySet"] != false {
		t.Fatalf("after delete = %v", got)
	}
}

func TestAPISettingsRejectsBadInput(t *testing.T) {
	h := newSettingsAPI(t)
	wantError(t, send(t, h, "PUT", "/api/settings", `{"maxTranscodes":99}`), 400,
		"réglage invalide : le nombre de conversions simultanées doit être compris entre 1 et 16")
	wantError(t, send(t, h, "PUT", "/api/settings", `{"nope":1}`), 400, "Requête invalide.")
	wantError(t, send(t, h, "PUT", "/api/settings", `{`), 400, "Requête invalide.")
	wantError(t, send(t, h, "PUT", "/api/settings", `{"maxTranscodes":"x"}`), 400, "Requête invalide.")
	wantError(t, send(t, h, "PUT", "/api/settings", `{"maxTranscodes":2}{"maxTranscodes":3}`), 400, "Requête invalide.")
	big := `{"acoustidKey":"` + strings.Repeat("a", 70<<10) + `"}`
	wantError(t, send(t, h, "PUT", "/api/settings", big), 400, "Requête invalide.")
}

func TestSystemReportsTools(t *testing.T) {
	h := NewAPI(Deps{Tools: fakeTools{st: []media.ToolStatus{
		{Name: "ffmpeg", Path: "/usr/bin/ffmpeg", Version: "7.0", Available: true},
		{Name: "fpcalc", Error: "introuvable"},
	}}})
	rec := send(t, h, "GET", "/api/system", "")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 2 || got.Tools[0]["name"] != "ffmpeg" || got.Tools[0]["available"] != true ||
		got.Tools[0]["version"] != "7.0" || got.Tools[0]["path"] != "/usr/bin/ffmpeg" ||
		got.Tools[1]["available"] != false || got.Tools[1]["error"] != "introuvable" {
		t.Fatalf("tools = %v", got.Tools)
	}
}

func TestNilDepIs503(t *testing.T) {
	h := NewAPI(Deps{})
	for _, r := range [][2]string{
		{"GET", "/api/settings"}, {"PUT", "/api/settings"}, {"GET", "/api/system"},
		{"GET", "/api/search?q=x"}, {"GET", "/api/artists/" + artistID},
		{"GET", "/api/albums/" + albumID}, {"GET", "/api/covers/" + albumID},
	} {
		wantError(t, send(t, h, r[0], r[1], "{}"), 503, "Fonction indisponible.")
	}
}
