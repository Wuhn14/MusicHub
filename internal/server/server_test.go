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

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := New(http.NotFoundHandler(), http.NotFoundHandler())
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'",
	}
	for _, path := range []string{"/healthz", "/api/x", "/artist/123"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		for name, value := range want {
			if got := rec.Header().Get(name); got != value {
				t.Errorf("%s: %s = %q, want %q", path, name, got, value)
			}
		}
	}
}

func TestRefusesCrossSiteWrites(t *testing.T) {
	reached := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached++ })
	h := New(api, http.NotFoundHandler())
	for _, tc := range []struct {
		method, site string
		want         int
	}{
		{"POST", "cross-site", http.StatusForbidden},
		{"PUT", "cross-site", http.StatusForbidden},
		{"DELETE", "cross-site", http.StatusForbidden},
		{"GET", "cross-site", http.StatusOK}, // reads stay open (covers, streams)
		{"HEAD", "cross-site", http.StatusOK},
		{"POST", "same-origin", http.StatusOK},
		{"POST", "same-site", http.StatusOK},
		{"POST", "none", http.StatusOK},
		{"POST", "", http.StatusOK}, // tools that send no such header
	} {
		reached = 0
		req := httptest.NewRequest(tc.method, "/api/settings", nil)
		if tc.site != "" {
			req.Header.Set("Sec-Fetch-Site", tc.site)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s with %q: status %d, want %d", tc.method, tc.site, rec.Code, tc.want)
		}
		if tc.want == http.StatusForbidden {
			if reached != 0 {
				t.Errorf("%s with %q reached the API", tc.method, tc.site)
			}
			if got := rec.Body.String(); got != "{\"error\":\"Requête refusée.\"}\n" {
				t.Errorf("body = %q", got)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
		}
	}
}
