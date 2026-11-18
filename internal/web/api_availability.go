package web

import (
	"context"
	"net/http"

	"github.com/Wuhn14/MusicHub/internal/identify"
)

// AvailabilityReader tells which catalog recordings the library can play
// (store.Store in production).
type AvailabilityReader interface {
	AlbumAvailability(ctx context.Context, albumID string) (map[string]string, error)
	ArtistAvailability(ctx context.Context, artistID string) (map[string]int, error)
}

// IdentifyStatusReader reports the state of the identification queue.
type IdentifyStatusReader interface {
	Status() identify.WorkerStatus
}

// availability answers GET /api/library/availability with exactly one of
// album=<mbid> ({"tracks": {recordingId: fileId}}) or artist=<mbid>
// ({"albums": {albumId: count}}).
func (a *api) availability(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	album, artist := q.Get("album"), q.Get("artist")
	switch {
	case album != "" && artist == "":
		if !mbidPattern.MatchString(album) {
			writeError(w, http.StatusBadRequest, "Identifiant invalide.")
			return
		}
		tracks, err := a.avail.AlbumAvailability(r.Context(), album)
		if err != nil {
			fail(w, r, err)
			return
		}
		if tracks == nil {
			tracks = map[string]string{}
		}
		writeJSON(w, http.StatusOK, struct {
			Tracks map[string]string `json:"tracks"`
		}{tracks})
	case artist != "" && album == "":
		if !mbidPattern.MatchString(artist) {
			writeError(w, http.StatusBadRequest, "Identifiant invalide.")
			return
		}
		albums, err := a.avail.ArtistAvailability(r.Context(), artist)
		if err != nil {
			fail(w, r, err)
			return
		}
		if albums == nil {
			albums = map[string]int{}
		}
		writeJSON(w, http.StatusOK, struct {
			Albums map[string]int `json:"albums"`
		}{albums})
	default:
		writeError(w, http.StatusBadRequest, "Indique un album ou un artiste.")
	}
}

func (a *api) identifyStatus(w http.ResponseWriter, _ *http.Request) {
	s := a.identify.Status()
	writeJSON(w, http.StatusOK, struct {
		Queued int `json:"queued"`
		Done   int `json:"done"`
		Failed int `json:"failed"`
	}{s.Queued, s.Done, s.Failed})
}
