package web

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/Wuhn14/MusicHub/internal/library"
)

// LibraryReader reads the local library and drives its scanner.
type LibraryReader interface {
	Albums(ctx context.Context, sort string) ([]library.LibAlbum, error)
	Artists(ctx context.Context) ([]library.LibArtist, error)
	Tracks(ctx context.Context, sort, q string) ([]library.LibTrack, error)
	Album(ctx context.Context, key string) (library.LibAlbum, []library.LibTrack, error)
	Trigger()
	Status() library.RunnerStatus
}

type libAlbumJSON struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Year       string `json:"year"`
	CoverURL   string `json:"coverUrl"`
	AlbumID    string `json:"albumId"`
	TrackCount int    `json:"trackCount"`
	DurationMs int    `json:"durationMs"`
	Local      bool   `json:"local"`
}

type libArtistJSON struct {
	Name       string `json:"name"`
	AlbumCount int    `json:"albumCount"`
	TrackCount int    `json:"trackCount"`
}

type libTrackJSON struct {
	FileID      string `json:"fileId"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumKey    string `json:"albumKey"`
	Disc        int    `json:"disc"`
	Position    int    `json:"position"`
	DurationMs  int    `json:"durationMs"`
	RecordingID string `json:"recordingId"`
	Local       bool   `json:"local"`
	Status      string `json:"status"`
	CoverURL    string `json:"coverUrl"`
}

func toLibAlbum(a library.LibAlbum) libAlbumJSON {
	return libAlbumJSON{Key: a.Key, Title: a.Title, Artist: a.Artist, Year: a.Year, CoverURL: a.CoverURL,
		AlbumID: a.AlbumID, TrackCount: a.TrackCount, DurationMs: a.DurationMs, Local: a.Local}
}

func toLibTracks(ts []library.LibTrack) []libTrackJSON {
	out := make([]libTrackJSON, 0, len(ts))
	for _, t := range ts {
		out = append(out, libTrackJSON{FileID: t.FileID, Title: t.Title, Artist: t.Artist, Album: t.Album,
			AlbumKey: t.AlbumKey, Disc: t.Disc, Position: t.Position, DurationMs: t.DurationMs,
			RecordingID: t.RecordingID, Local: t.Local, Status: t.Status, CoverURL: t.CoverURL})
	}
	return out
}

// sortParam validates the sort query parameter; unknown values are rejected.
func sortParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	s := r.URL.Query().Get("sort")
	switch s {
	case "", library.SortAdded, library.SortName, library.SortYear:
		return s, true
	}
	writeError(w, http.StatusBadRequest, "Tri invalide.")
	return "", false
}

func (a *api) libAlbums(w http.ResponseWriter, r *http.Request) {
	s, ok := sortParam(w, r)
	if !ok {
		return
	}
	albums, err := a.lib.Albums(r.Context(), s)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := make([]libAlbumJSON, 0, len(albums))
	for _, al := range albums {
		out = append(out, toLibAlbum(al))
	}
	writeJSON(w, http.StatusOK, struct {
		Albums []libAlbumJSON `json:"albums"`
	}{out})
}

func (a *api) libArtists(w http.ResponseWriter, r *http.Request) {
	artists, err := a.lib.Artists(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	out := make([]libArtistJSON, 0, len(artists))
	for _, ar := range artists {
		out = append(out, libArtistJSON{Name: ar.Name, AlbumCount: ar.AlbumCount, TrackCount: ar.TrackCount})
	}
	writeJSON(w, http.StatusOK, struct {
		Artists []libArtistJSON `json:"artists"`
	}{out})
}

func (a *api) libTracks(w http.ResponseWriter, r *http.Request) {
	s, ok := sortParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query().Get("q")
	if utf8.RuneCountInString(q) > maxQueryRunes {
		writeError(w, http.StatusBadRequest, "Recherche trop longue.")
		return
	}
	tracks, err := a.lib.Tracks(r.Context(), s, q)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Tracks []libTrackJSON `json:"tracks"`
	}{toLibTracks(tracks)})
}

func (a *api) libAlbum(w http.ResponseWriter, r *http.Request) {
	al, tracks, err := a.lib.Album(r.Context(), r.PathValue("key"))
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		libAlbumJSON
		Tracks []libTrackJSON `json:"tracks"`
	}{toLibAlbum(al), toLibTracks(tracks)})
}

func (a *api) libScan(w http.ResponseWriter, _ *http.Request) {
	a.lib.Trigger()
	writeJSON(w, http.StatusAccepted, struct{}{})
}

type scanReportJSON struct {
	Seen       int `json:"seen"`
	Added      int `json:"added"`
	Updated    int `json:"updated"`
	Unreadable int `json:"unreadable"`
	Missing    int `json:"missing"`
	Errors     int `json:"errors"`
}

func (a *api) libStatus(w http.ResponseWriter, _ *http.Request) {
	st := a.lib.Status()
	lastRun := ""
	if !st.LastRun.IsZero() {
		lastRun = st.LastRun.UTC().Format(time.RFC3339Nano)
	}
	rep := st.LastReport
	writeJSON(w, http.StatusOK, struct {
		Running    bool           `json:"running"`
		LastRun    string         `json:"lastRun"`
		LastError  string         `json:"lastError"`
		LastReport scanReportJSON `json:"lastReport"`
	}{st.Running, lastRun, st.LastError, scanReportJSON{rep.Seen, rep.Added, rep.Updated, rep.Unreadable, rep.Missing, rep.Errors}})
}
