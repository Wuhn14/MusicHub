package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	h := New(http.NotFoundHandler(), http.NotFoundHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"ok":true}` {
		t.Errorf("body = %q", got)
	}
}

func TestRoutesApiAndUI(t *testing.T) {
	marker := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(name))
		})
	}
	h := New(marker("api"), marker("ui"))
	for path, want := range map[string]string{"/api/x": "api", "/artist/123": "ui"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if got := rec.Body.String(); got != want {
			t.Errorf("%s reached %q, want %q", path, got, want)
		}
	}
}
