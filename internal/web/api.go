// Package web serves the JSON API used by the MusicHub front end.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/Wuhn14/MusicHub/internal/catalog"
	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/store"
)

// maxQueryRunes is the longest accepted search query.
const maxQueryRunes = 200

// catalogTimeout bounds one catalog call. MusicBrainz is rate limited to one
// request per second, so a heavy page can legitimately take a while, but it
// must not hold a connection forever.
const catalogTimeout = 60 * time.Second

var mbidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// CatalogReader is the part of the catalog the API needs.
type CatalogReader interface {
	Search(ctx context.Context, q string) (catalog.SearchResult, error)
	Artist(ctx context.Context, id string) (store.Artist, []store.Album, error)
	Album(ctx context.Context, id string) (store.Album, error)
}

// CoverGetter returns the path of a cached cover image.
type CoverGetter interface {
	Get(ctx context.Context, albumID string, size int) (string, error)
}

// Deps are the services the API routes depend on. A nil field makes the
// routes that need it answer 503, so a partially wired server still starts.
type Deps struct {
	Catalog  CatalogReader
	Covers   CoverGetter
	Settings SettingsService
	Tools    ToolStatusReader
	Library  LibraryReader
}

type api struct {
	cat      CatalogReader
	covers   CoverGetter
	settings SettingsService
	tools    ToolStatusReader
	lib      LibraryReader
}

// NewAPI returns the handler serving every /api route.
func NewAPI(d Deps) http.Handler {
	a := &api{cat: d.Catalog, covers: d.Covers, settings: d.Settings, tools: d.Tools, lib: d.Library}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/search", requires(d.Catalog != nil, a.search))
	mux.HandleFunc("GET /api/artists/{id}", requires(d.Catalog != nil, a.artist))
	mux.HandleFunc("GET /api/albums/{id}", requires(d.Catalog != nil, a.album))
	mux.HandleFunc("GET /api/covers/{id}", requires(d.Covers != nil, a.cover))
	mux.HandleFunc("GET /api/settings", requires(d.Settings != nil, a.getSettings))
	mux.HandleFunc("PUT /api/settings", requires(d.Settings != nil, a.putSettings))
	mux.HandleFunc("GET /api/system", requires(d.Tools != nil, a.system))
	mux.HandleFunc("GET /api/library/albums", requires(d.Library != nil, a.libAlbums))
	mux.HandleFunc("GET /api/library/albums/{key}", requires(d.Library != nil, a.libAlbum))
	mux.HandleFunc("GET /api/library/artists", requires(d.Library != nil, a.libArtists))
	mux.HandleFunc("GET /api/library/tracks", requires(d.Library != nil, a.libTracks))
	mux.HandleFunc("GET /api/library/status", requires(d.Library != nil, a.libStatus))
	mux.HandleFunc("POST /api/library/scan", requires(d.Library != nil, a.libScan))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Introuvable.")
	})
	return mux
}

// requires returns h when its dependency is wired, and a 503 handler otherwise.
func requires(ok bool, h http.HandlerFunc) http.HandlerFunc {
	if ok {
		return h
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusServiceUnavailable, "Fonction indisponible.")
	}
}

// JSON response shapes (camelCase, never serialising store structs directly).

type artistJSON struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sortName"`
	Disambiguation string `json:"disambiguation"`
	Country        string `json:"country"`
}

type searchArtistJSON struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Disambiguation string `json:"disambiguation"`
	Country        string `json:"country"`
}

type albumSummaryJSON struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	ArtistID         string   `json:"artistId"`
	ArtistName       string   `json:"artistName"`
	PrimaryType      string   `json:"primaryType"`
	SecondaryTypes   []string `json:"secondaryTypes"`
	FirstReleaseDate string   `json:"firstReleaseDate"`
}

type trackHitJSON struct {
	RecordingID string `json:"recordingId"`
	Title       string `json:"title"`
	LengthMs    int    `json:"lengthMs"`
	AlbumID     string `json:"albumId"`
	AlbumTitle  string `json:"albumTitle"`
	ArtistName  string `json:"artistName"`
}

type trackJSON struct {
	RecordingID string `json:"recordingId"`
	Title       string `json:"title"`
	Disc        int    `json:"disc"`
	Position    int    `json:"position"`
	LengthMs    int    `json:"lengthMs"`
}

type albumJSON struct {
	albumSummaryJSON
	Tracks []trackJSON `json:"tracks"`
}

func toAlbumSummary(a store.Album) albumSummaryJSON {
	sec := a.SecondaryTypes
	if sec == nil {
		sec = []string{}
	}
	return albumSummaryJSON{
		ID: a.ID, Title: a.Title, ArtistID: a.ArtistID, ArtistName: a.ArtistName,
		PrimaryType: a.PrimaryType, SecondaryTypes: sec, FirstReleaseDate: a.FirstReleaseDate,
	}
}

func toAlbumSummaries(albums []store.Album) []albumSummaryJSON {
	out := make([]albumSummaryJSON, 0, len(albums))
	for _, a := range albums {
		out = append(out, toAlbumSummary(a))
	}
	return out
}

func toArtist(a store.Artist) artistJSON {
	return artistJSON{ID: a.ID, Name: a.Name, SortName: a.SortName, Disambiguation: a.Disambiguation, Country: a.Country}
}

func (a *api) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if utf8.RuneCountInString(q) > maxQueryRunes {
		writeError(w, http.StatusBadRequest, "Recherche trop longue.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), catalogTimeout)
	defer cancel()
	res, err := a.cat.Search(ctx, q)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := struct {
		Artists []searchArtistJSON `json:"artists"`
		Albums  []albumSummaryJSON `json:"albums"`
		Tracks  []trackHitJSON     `json:"tracks"`
	}{
		Artists: make([]searchArtistJSON, 0, len(res.Artists)),
		Albums:  toAlbumSummaries(res.Albums),
		Tracks:  make([]trackHitJSON, 0, len(res.Tracks)),
	}
	for _, ar := range res.Artists {
		out.Artists = append(out.Artists, searchArtistJSON{ID: ar.ID, Name: ar.Name, Disambiguation: ar.Disambiguation, Country: ar.Country})
	}
	for _, t := range res.Tracks {
		out.Tracks = append(out.Tracks, trackHitJSON{
			RecordingID: t.RecordingID, Title: t.Title, LengthMs: t.LengthMs,
			AlbumID: t.AlbumID, AlbumTitle: t.AlbumTitle, ArtistName: t.ArtistName,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) artist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !mbidPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "Identifiant invalide.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), catalogTimeout)
	defer cancel()
	artist, albums, err := a.cat.Artist(ctx, id)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Artist artistJSON         `json:"artist"`
		Albums []albumSummaryJSON `json:"albums"`
	}{toArtist(artist), toAlbumSummaries(albums)})
}

func (a *api) album(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !mbidPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "Identifiant invalide.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), catalogTimeout)
	defer cancel()
	album, err := a.cat.Album(ctx, id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := albumJSON{albumSummaryJSON: toAlbumSummary(album), Tracks: make([]trackJSON, 0, len(album.Tracks))}
	for _, t := range album.Tracks {
		out.Tracks = append(out.Tracks, trackJSON{
			RecordingID: t.RecordingID, Title: t.Title, Disc: t.Disc, Position: t.Position, LengthMs: t.LengthMs,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) cover(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !mbidPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "Identifiant invalide.")
		return
	}
	// An unparsable size becomes 0, which Covers treats as the default.
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	path, err := a.covers.Get(r.Context(), id, size)
	if err != nil {
		fail(w, r, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		// The cached file vanished between the lookup and the open.
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Introuvable.")
			return
		}
		fail(w, r, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=604800")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// fail maps an error to an HTTP status and a French message. Unexpected
// errors are logged but never shown to the client.
func fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, musicbrainz.ErrNotFound), errors.Is(err, catalog.ErrNoCover):
		writeError(w, http.StatusNotFound, "Introuvable.")
	case errors.Is(err, catalog.ErrCoverUnavailable):
		slog.Warn("cover art unavailable", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusBadGateway, "Les pochettes sont momentanément indisponibles.")
	case errors.Is(err, musicbrainz.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusBadGateway, "MusicBrainz est momentanément indisponible, réessaie dans un instant.")
	default:
		if r.Context().Err() == nil {
			slog.Error("api request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		}
		writeError(w, http.StatusInternalServerError, "Erreur interne.")
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("api: writing response failed", "err", err)
	}
}
