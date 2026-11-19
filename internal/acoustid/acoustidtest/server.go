// Package acoustidtest provides a fake AcoustID web service for tests.
//
// New starts an httptest server answering POST /v2/lookup (and /lookup) with
// invented results keyed by the fingerprint string; Handler gives the same
// behaviour as an http.Handler so the cmd/fakemb development binary can mount
// it. A lookup made with the key BadKey is refused like an invalid API key
// (error code 4). Server records the requests and can answer an error status
// a given number of times. All data is invented.
package acoustidtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// BadKey is the API key the fake refuses with error code 4 and HTTP 400,
	// like the real service; BadKey200 gets the same error body with HTTP 200.
	BadKey    = "invalid-key"
	BadKey200 = "invalid-key-200"

	// FingerprintGlassTide matches two results (lowest score listed first, to
	// check sorting): the recording Glass Tide with 0.97 and an unrelated
	// recording with 0.55.
	FingerprintGlassTide = "AQAD-glass-tide"
	// FingerprintNoRecording matches a result that carries no recording.
	FingerprintNoRecording = "AQAD-no-recording"

	// RecordingGlassTide is the same recording as in mbtest, so the two fakes
	// can be combined.
	RecordingGlassTide = "00000000-0000-4000-8000-0000000000d1"
	// ReleaseGroupLanterns is the release group of RecordingGlassTide in mbtest.
	ReleaseGroupLanterns = "00000000-0000-4000-8000-0000000000b1"
)

// Request describes one request received by the fake.
type Request struct {
	Path string
	Form url.Values
	At   time.Time
}

// Server is a fake AcoustID web service.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	reqs     []Request
	failNext int
}

// New starts a fake server. The caller must Close it. Use Server.URL+"/v2"
// as the client base URL.
func New() *Server {
	s := &Server{}
	s.Server = httptest.NewServer(s.Handler())
	return s
}

// Handler returns the fake as an http.Handler. A server created by New uses
// it too. It can be used without New (the zero Server is ready).
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.handle) }

// Requests returns how many requests were received (503s included).
func (s *Server) Requests() int { return len(s.All()) }

// All returns a copy of every received request, oldest first.
func (s *Server) All() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Last returns the most recent request, or a zero Request if none.
func (s *Server) Last() Request {
	all := s.All()
	if len(all) == 0 {
		return Request{}
	}
	return all[len(all)-1]
}

// FailNext makes the next n requests answer 503.
func (s *Server) FailNext(n int) {
	s.mu.Lock()
	s.failNext = n
	s.mu.Unlock()
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	s.reqs = append(s.reqs, Request{Path: r.URL.Path, Form: r.PostForm, At: time.Now()})
	fail := s.failNext > 0
	if fail {
		s.failNext--
	}
	s.mu.Unlock()
	if fail {
		http.Error(w, `{"status":"error","error":{"code":5,"message":"busy"}}`, http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/lookup") {
		http.NotFound(w, r)
		return
	}
	switch r.PostForm.Get("client") {
	case BadKey, BadKey200:
		status := http.StatusBadRequest
		if r.PostForm.Get("client") == BadKey200 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": map[string]any{"code": 4, "message": "invalid API key"}})
		return
	}
	results := []map[string]any{}
	switch r.PostForm.Get("fingerprint") {
	case FingerprintGlassTide:
		results = []map[string]any{
			{"id": "00000000-0000-4000-8000-00000000f002", "score": 0.55, "recordings": []map[string]any{{
				"id": "00000000-0000-4000-8000-0000000000d2", "title": "Orphan Song", "duration": 180,
				"artists": []map[string]any{{"id": "00000000-0000-4000-8000-0000000000a4", "name": "Mira Vale"}},
			}}},
			{"id": "00000000-0000-4000-8000-00000000f001", "score": 0.97, "recordings": []map[string]any{{
				"id": RecordingGlassTide, "title": "Glass Tide", "duration": 201,
				"artists": []map[string]any{{"id": "00000000-0000-4000-8000-0000000000a1", "name": "Lumen Harbor"}},
				"releasegroups": []map[string]any{{
					"id": ReleaseGroupLanterns, "title": "Paper Lanterns", "type": "Album", "secondarytypes": []string{"Compilation"},
				}},
			}}},
		}
	case FingerprintNoRecording:
		results = []map[string]any{{"id": "00000000-0000-4000-8000-00000000f003", "score": 0.99}}
	}
	writeJSON(w, map[string]any{"status": "ok", "results": results})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
