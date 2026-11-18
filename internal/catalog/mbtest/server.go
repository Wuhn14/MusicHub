// Package mbtest provides a fake MusicBrainz web service for tests.
//
// New starts an httptest server that answers the subset of the MusicBrainz
// JSON API used by MusicHub (search, artist and release group lookups,
// release browsing and release lookup with tracks). All data is invented.
// Point a client at Server.URL (the fake serves paths at its root, e.g.
// /artist, not /ws/2/artist).
//
// Fixtures: the small JSON files in testdata/ are embedded; the artist
// ArtistMany has ManyAlbums generated release groups to exercise
// pagination, and ArtistHuge has HugeAlbums of them to exercise the page cap.
// ArtistLumen has a single release group, AlbumTwoDisc. Server also counts
// requests, records them (path, query, User-Agent, arrival time) and can
// answer an error status a given number of times.
// It is shared by the catalog service tests and by the cmd/fakemb
// development binary.
package mbtest

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed testdata/*.json
var fixtures embed.FS

// Identifiers of the invented fixture data.
const (
	// ArtistLumen is an artist with a lookup fixture and one release group,
	// AlbumTwoDisc.
	ArtistLumen = "00000000-0000-4000-8000-0000000000a1"
	// ArtistMany has ManyAlbums generated release groups.
	ArtistMany = "00000000-0000-4000-8000-0000000000a2"
	// ArtistHuge has HugeAlbums generated release groups, more than the client
	// is willing to browse.
	ArtistHuge = "00000000-0000-4000-8000-0000000000a3"
	// AlbumTwoDisc is a release group with five releases; its reference
	// release is ReleaseReference (official, earliest, two discs, three
	// tracks).
	AlbumTwoDisc     = "00000000-0000-4000-8000-0000000000b1"
	ReleaseReference = "00000000-0000-4000-8000-0000000000c1"
	// RecordingGlassTide is a recording with a lookup fixture: second disc,
	// third track of a release of AlbumTwoDisc.
	RecordingGlassTide = "00000000-0000-4000-8000-0000000000d1"
	// ManyAlbums is the number of release groups of ArtistMany.
	ManyAlbums = 230
	// HugeAlbums is the number of release groups of ArtistHuge.
	HugeAlbums = 700
)

// Request describes one request received by the fake.
type Request struct {
	Path      string
	Query     url.Values
	UserAgent string
	At        time.Time
}

// Server is a fake MusicBrainz web service.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	reqs     []Request
	failNext int
	failCode int
}

// New starts a fake server. The caller must Close it.
func New() *Server {
	s := &Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Requests returns how many requests were received (503s included).
func (s *Server) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

// FailNext makes the next n requests answer 503.
func (s *Server) FailNext(n int) {
	s.FailNextWith(http.StatusServiceUnavailable, n)
}

// FailNextWith makes the next n requests answer the given status code.
func (s *Server) FailNextWith(status, n int) {
	s.mu.Lock()
	s.failNext = n
	s.failCode = status
	s.mu.Unlock()
}

// Last returns the most recent request, or a zero Request if none.
func (s *Server) Last() Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return Request{}
	}
	return s.reqs[len(s.reqs)-1]
}

// All returns a copy of every received request, oldest first.
func (s *Server) All() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Times returns the arrival time of every request, oldest first.
func (s *Server) Times() []time.Time {
	all := s.All()
	out := make([]time.Time, len(all))
	for i, r := range all {
		out[i] = r.At
	}
	return out
}

var (
	pathRE     = regexp.MustCompile(`^/([a-z-]+)(?:/([^/]+))?$`)
	fileNameRE = regexp.MustCompile(`^[a-z-]+_[0-9a-zA-Z.-]+\.json$`)
)

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	s.reqs = append(s.reqs, Request{Path: r.URL.Path, Query: q, UserAgent: r.UserAgent(), At: time.Now()})
	fail, code := s.failNext > 0, s.failCode
	if fail {
		s.failNext--
	}
	s.mu.Unlock()
	if fail {
		http.Error(w, `{"error":"busy"}`, code)
		return
	}

	m := pathRE.FindStringSubmatch(r.URL.Path)
	if m == nil {
		notFound(w)
		return
	}
	kind, id := m[1], m[2]
	switch {
	case id == "" && q.Get("query") != "" && (kind == "artist" || kind == "release-group" || kind == "recording"):
		if kind == "recording" && strings.Contains(q.Get("query"), "recording:") {
			// Fielded query from FindRecordings.
			serveFile(w, "recording_find.json")
			return
		}
		serveFile(w, kind+"_search.json")
	case id == "" && kind == "release-group" && q.Get("artist") != "":
		browseReleaseGroups(w, q)
	case id == "" && kind == "release" && q.Get("release-group") != "":
		serveFile(w, "releases_"+q.Get("release-group")+".json")
	case id == "":
		notFound(w)
	case kind == "artist" && id == ArtistMany:
		writeJSON(w, map[string]any{"id": ArtistMany, "name": "Many Albums Ensemble", "sort-name": "Ensemble, Many Albums"})
	case kind == "artist" && id == ArtistHuge:
		writeJSON(w, map[string]any{"id": ArtistHuge, "name": "Huge Catalogue Orchestra", "sort-name": "Orchestra, Huge Catalogue"})
	case kind == "artist" || kind == "release-group" || kind == "release" || kind == "recording":
		serveFile(w, kind+"_"+id+".json")
	default:
		notFound(w)
	}
}

func notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":"Not Found"}`))
}

func serveFile(w http.ResponseWriter, name string) {
	if !fileNameRE.MatchString(name) {
		notFound(w)
		return
	}
	b, err := fixtures.ReadFile("testdata/" + name)
	if err != nil {
		notFound(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func browseReleaseGroups(w http.ResponseWriter, q url.Values) {
	total := 0
	switch q.Get("artist") {
	case ArtistMany:
		total = ManyAlbums
	case ArtistHuge:
		total = HugeAlbums
	case ArtistLumen:
		// Browse results carry no secondary types here, so the album lands in
		// the "Albums" group of the artist page.
		writeJSON(w, map[string]any{"release-group-count": 1, "release-group-offset": 0, "release-groups": []map[string]any{{
			"id": AlbumTwoDisc, "title": "Paper Lanterns", "primary-type": "Album", "first-release-date": "2003-05-12",
		}}})
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	groups := []map[string]any{}
	for i := offset; i < total && i < offset+limit; i++ {
		groups = append(groups, map[string]any{
			"id":                 fmt.Sprintf("00000000-0000-4000-9000-%012d", i),
			"title":              fmt.Sprintf("Generated Album %03d", i),
			"primary-type":       "Album",
			"first-release-date": "2001",
		})
	}
	writeJSON(w, map[string]any{"release-group-count": total, "release-group-offset": offset, "release-groups": groups})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
