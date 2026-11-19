// Package server assembles the top-level HTTP routes of MusicHub.
package server

import "net/http"

// contentSecurityPolicy only allows what the built UI needs: its own scripts
// and styles (inline styles are set by the components), images from the same
// origin plus data: URIs, and no framing.
const contentSecurityPolicy = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'"

// secure adds the security headers to every response.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// sameSiteWrites refuses the API writes a browser sends on behalf of another
// site (a form or script on a page elsewhere targeting MusicHub). Browsers
// mark such requests with Sec-Fetch-Site: cross-site; requests without the
// header (command-line tools, older browsers) go through.
func sameSiteWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead &&
			r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"Requête refusée."}` + "\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// New routes /healthz, sends /api/ to api and everything else to ui.
func New(api http.Handler, ui http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	mux.Handle("/api/", sameSiteWrites(api))
	mux.Handle("/", ui)
	return secure(mux)
}
