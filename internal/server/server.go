// Package server assembles the top-level HTTP routes of MusicHub.
package server

import "net/http"

// New routes /healthz, sends /api/ to api and everything else to ui.
func New(api http.Handler, ui http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	mux.Handle("/api/", api)
	mux.Handle("/", ui)
	return mux
}
