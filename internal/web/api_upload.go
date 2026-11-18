package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wuhn14/MusicHub/internal/store"
	"github.com/Wuhn14/MusicHub/internal/upload"
)

// UploadReceiver stores one uploaded file (*upload.Receiver in production).
type UploadReceiver interface {
	Receive(ctx context.Context, name string, r io.Reader) (store.File, error)
	MaxBytes(ctx context.Context) int64
}

// FileReader reads stored files and the catalog albums they are linked to
// (*store.Store in production).
type FileReader interface {
	GetFile(ctx context.Context, id string) (store.File, error)
	GetAlbum(ctx context.Context, id string) (store.Album, error)
	GetLocalTrack(ctx context.Context, id string) (store.LocalTrack, error)
}

type uploadedFileJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMs int    `json:"durationMs"`
	Format     string `json:"format"`
}

// formatBytes writes a size in Mo or Go with a French decimal comma.
func formatBytes(n int64) string {
	const mo, gigabyte = 1 << 20, 1 << 30
	var v float64
	var unit string
	if n >= gigabyte {
		v, unit = float64(n)/gigabyte, "Go"
	} else {
		v, unit = float64(n)/mo, "Mo"
	}
	s := strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
	return strings.Replace(s, ".", ",", 1) + " " + unit
}

// displayName is the client's file name without any directory part.
func displayName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// upload answers POST /api/uploads: one raw file per request, its name in the
// URI-encoded X-File-Name header.
func (a *api) upload(w http.ResponseWriter, r *http.Request) {
	name, err := url.PathUnescape(r.Header.Get("X-File-Name"))
	if err != nil || strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "Nom de fichier invalide.")
		return
	}
	if r.ContentLength == 0 {
		writeError(w, http.StatusBadRequest, "Le fichier est vide.")
		return
	}
	limit := a.uploads.MaxBytes(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	f, err := a.uploads.Receive(r.Context(), name, r.Body)
	switch {
	case err == nil:
	case errors.Is(err, upload.ErrUnsupportedFormat):
		writeError(w, http.StatusUnsupportedMediaType, "Format non pris en charge.")
		return
	case errors.Is(err, upload.ErrNotAudio):
		writeError(w, http.StatusUnsupportedMediaType, "Ce fichier n'est pas un fichier audio lisible.")
		return
	case errors.Is(err, upload.ErrEmpty):
		writeError(w, http.StatusBadRequest, "Le fichier est vide.")
		return
	case errors.Is(err, upload.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Fichier trop volumineux (maximum : %s).", formatBytes(limit)))
		return
	case r.Context().Err() != nil:
		// The client went away; nothing is left behind and nobody listens.
		slog.Debug("upload interrupted", "err", err)
		return
	default:
		slog.Error("upload failed", "err", err)
		writeError(w, http.StatusInternalServerError, "Analyse du fichier impossible.")
		return
	}
	writeJSON(w, http.StatusCreated, uploadedFileJSON{
		ID: f.ID, Name: displayName(name), Status: f.Status, DurationMs: f.DurationMs, Format: f.Format,
	})
}

type fileStatusJSON struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

// fileStatus answers GET /api/files/{id}: the status of a file and, once it
// is identified, what it was identified as.
func (a *api) fileStatus(w http.ResponseWriter, r *http.Request) {
	f, err := a.files.GetFile(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := fileStatusJSON{ID: f.ID, Status: f.Status}
	if f.Status == store.FileIdentified || f.Status == store.FileLocal {
		out.Title, out.Artist, out.Album = f.Tags.Title, f.Tags.Artist, f.Tags.Album
		if f.Status == store.FileLocal {
			if lt, err := a.files.GetLocalTrack(r.Context(), f.LocalTrackID); err == nil {
				out.Title, out.Artist, out.Album = lt.Title, lt.Artist, lt.Album
			}
		}
		if f.AlbumID != "" {
			if al, err := a.files.GetAlbum(r.Context(), f.AlbumID); err == nil {
				out.Album = al.Title
				if al.ArtistName != "" {
					out.Artist = al.ArtistName
				}
				for _, t := range al.Tracks {
					if t.RecordingID == f.RecordingID && t.Title != "" {
						out.Title = t.Title
					}
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
