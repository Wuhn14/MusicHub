package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/identify"
	"github.com/Wuhn14/MusicHub/internal/store"
	"github.com/Wuhn14/MusicHub/internal/upload"
)

// Limits of the "À identifier" routes.
const (
	// maxIdentifyList is how many files GET /api/identify returns at most.
	maxIdentifyList = 100
	// maxIdentifyBody bounds the JSON bodies of choose and local.
	maxIdentifyBody = 16 << 10
	// maxLocalText is the longest accepted text field of a local track.
	maxLocalText = 300
	// maxCoverBytes is the largest accepted cover image.
	maxCoverBytes = 5 << 20
)

// fileIDPattern matches the ids of stored files and local tracks.
var fileIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Messages of the "À identifier" routes.
const (
	msgNotPending     = "Ce fichier n'est plus à identifier."
	msgTitleArtist    = "Le titre et l'artiste sont obligatoires."
	msgDroppedDelete  = "Ce fichier est dans ton dossier musique : supprime-le directement sur le disque."
	msgBadCover       = "Image non prise en charge (JPEG, PNG ou WebP)."
	msgNotLocal       = "Ce fichier n'est pas un titre local."
	msgNoRecording    = "Cet enregistrement est introuvable sur MusicBrainz."
	msgBadRequest     = "Requête invalide."
	msgBadID          = "Identifiant invalide."
	msgCoverTooLarge  = "Image trop volumineuse (5 Mo au maximum)."
	msgTextTooLong    = "Texte trop long (300 caractères au maximum)."
	msgBadDisc        = "Numéro de disque invalide (de 1 à 99)."
	msgBadPosition    = "Numéro de piste invalide (de 1 à 999)."
	msgBadYear        = "Année invalide (de 1000 à 9999)."
	msgDeleteFailed   = "Le fichier n'a pas pu être supprimé du disque."
	msgCoverSaveError = "La pochette n'a pas pu être enregistrée."
)

// IdentifyActions carries out the user's choices (*identify.Identifier in
// production). Both place the file once it is identified or local.
type IdentifyActions interface {
	Choose(ctx context.Context, fileID, recordingID, albumID string) (identify.Result, error)
	CreateLocal(ctx context.Context, fileID string, lt store.LocalTrack) (store.LocalTrack, store.File, error)
}

// IdentifyStore reads the files waiting for the user and manages local
// tracks (*store.Store in production).
type IdentifyStore interface {
	CountPending(ctx context.Context, jobKind string) (awaiting, queued int, err error)
	ListAwaiting(ctx context.Context, jobKind string, limit int) ([]store.File, error)
	ListCandidates(ctx context.Context, fileID string) ([]store.Candidate, error)
	GetFile(ctx context.Context, id string) (store.File, error)
	GetLocalTrack(ctx context.Context, id string) (store.LocalTrack, error)
	SetLocalTrackCover(ctx context.Context, id, coverPath string) error
	DeletePendingUpload(ctx context.Context, id string) (store.File, error)
}

type identifyTagsJSON struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumArtist string `json:"albumArtist"`
	Year        string `json:"year"`
	Track       int    `json:"track"`
	Disc        int    `json:"disc"`
}

type candidateJSON struct {
	RecordingID string `json:"recordingId"`
	AlbumID     string `json:"albumId"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	LengthMs    int    `json:"lengthMs"`
	Score       int    `json:"score"`
	Source      string `json:"source"`
}

type identifyFileJSON struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Origin     string           `json:"origin"`
	Format     string           `json:"format"`
	DurationMs int              `json:"durationMs"`
	Tags       identifyTagsJSON `json:"tags"`
	Candidates []candidateJSON  `json:"candidates"`
}

// yearOf keeps the leading four-digit year of a tag date.
func yearOf(date string) string {
	date = strings.TrimSpace(date)
	if len(date) >= 4 && strings.Trim(date[:4], "0123456789") == "" {
		return date[:4]
	}
	return ""
}

// identifyList answers GET /api/identify: the files waiting for the user
// (oldest first, at most maxIdentifyList), how many there are in all and
// how many are still being identified automatically.
func (a *api) identifyList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	awaiting, queued, err := a.idStore.CountPending(ctx, identify.JobKind)
	if err != nil {
		fail(w, r, err)
		return
	}
	files, err := a.idStore.ListAwaiting(ctx, identify.JobKind, maxIdentifyList)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := struct {
		Files  []identifyFileJSON `json:"files"`
		Total  int                `json:"total"`
		Queued int                `json:"queued"`
	}{Files: make([]identifyFileJSON, 0, len(files)), Total: awaiting, Queued: queued}
	for _, f := range files {
		cands, err := a.idStore.ListCandidates(ctx, f.ID)
		if err != nil {
			fail(w, r, err)
			return
		}
		name := f.UploadName
		if name == "" {
			name = filepath.Base(f.Path)
		}
		item := identifyFileJSON{
			ID: f.ID, Name: name, Origin: f.Origin, Format: f.Format, DurationMs: f.DurationMs,
			Tags: identifyTagsJSON{
				Title: f.Tags.Title, Artist: f.Tags.Artist, Album: f.Tags.Album, AlbumArtist: f.Tags.AlbumArtist,
				Year: yearOf(f.Tags.Date), Track: f.Tags.Track, Disc: f.Tags.Disc,
			},
			Candidates: make([]candidateJSON, 0, len(cands)),
		}
		for _, c := range cands {
			item.Candidates = append(item.Candidates, candidateJSON{
				RecordingID: c.RecordingID, AlbumID: c.AlbumID, Title: c.Title, Artist: c.Artist,
				Album: c.Album, LengthMs: c.LengthMs, Score: c.Score, Source: c.Source,
			})
		}
		out.Files = append(out.Files, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// identifyCount answers GET /api/identify/count (the menu badge).
func (a *api) identifyCount(w http.ResponseWriter, r *http.Request) {
	awaiting, queued, err := a.idStore.CountPending(r.Context(), identify.JobKind)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": awaiting, "queued": queued})
}

// decodeBody reads exactly one JSON object with known fields into v. The
// body must be declared as JSON.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if !requireJSON(w, r) {
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxIdentifyBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return false
	}
	return true
}

// pathFileID validates the {fileId} path value.
func pathFileID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("fileId")
	if !fileIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, msgBadID)
		return "", false
	}
	return id, true
}

// failIdentify maps the errors of the identification actions.
func failIdentify(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotPending), errors.Is(err, identify.ErrSkipped):
		writeError(w, http.StatusConflict, msgNotPending)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Introuvable.")
	default:
		fail(w, r, err)
	}
}

// identifyChoose answers POST /api/identify/{fileId}/choose
// {"recordingId": mbid, "albumId": mbid (optional)}.
func (a *api) identifyChoose(w http.ResponseWriter, r *http.Request) {
	id, ok := pathFileID(w, r)
	if !ok {
		return
	}
	var in struct {
		RecordingID string `json:"recordingId"`
		AlbumID     string `json:"albumId"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if !mbidPattern.MatchString(in.RecordingID) || (in.AlbumID != "" && !mbidPattern.MatchString(in.AlbumID)) {
		writeError(w, http.StatusBadRequest, msgBadID)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), catalogTimeout)
	defer cancel()
	res, err := a.idActions.Choose(ctx, id, in.RecordingID, in.AlbumID)
	if err != nil {
		if errors.Is(err, musicbrainz.ErrNotFound) {
			writeError(w, http.StatusNotFound, msgNoRecording)
			return
		}
		failIdentify(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": res.Status, "recordingId": res.RecordingID, "albumId": res.AlbumID, "duplicate": res.Duplicate,
	})
}

type localTrackJSON struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumArtist string `json:"albumArtist"`
	Disc        int    `json:"disc"`
	Position    int    `json:"position"`
	Year        int    `json:"year"`
}

// validate trims the fields and returns a French message for the first
// problem, or "".
func (l *localTrackJSON) validate() string {
	for _, p := range []*string{&l.Title, &l.Artist, &l.Album, &l.AlbumArtist} {
		*p = strings.TrimSpace(*p)
		if !utf8.ValidString(*p) || strings.ContainsFunc(*p, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return msgBadRequest
		}
		if utf8.RuneCountInString(*p) > maxLocalText {
			return msgTextTooLong
		}
	}
	switch {
	case l.Title == "" || l.Artist == "":
		return msgTitleArtist
	case l.Disc < 0 || l.Disc > 99:
		return msgBadDisc
	case l.Position < 0 || l.Position > 999:
		return msgBadPosition
	case l.Year != 0 && (l.Year < 1000 || l.Year > 9999):
		return msgBadYear
	}
	return ""
}

// identifyLocal answers POST /api/identify/{fileId}/local: the file becomes a
// local track described by the body.
func (a *api) identifyLocal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathFileID(w, r)
	if !ok {
		return
	}
	var in localTrackJSON
	if !decodeBody(w, r, &in) {
		return
	}
	if msg := in.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	lt, f, err := a.idActions.CreateLocal(r.Context(), id, store.LocalTrack{
		Title: in.Title, Artist: in.Artist, Album: in.Album, AlbumArtist: in.AlbumArtist,
		Disc: in.Disc, Position: in.Position, Year: in.Year,
	})
	if err != nil {
		failIdentify(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"localTrackId": lt.ID, "status": f.Status})
}

// imageExt recognises a JPEG, PNG or WebP image by its first bytes and
// returns the extension to store it under ("" for anything else).
func imageExt(head []byte) string {
	switch {
	case len(head) >= 3 && bytes.Equal(head[:3], []byte{0xFF, 0xD8, 0xFF}):
		return ".jpg"
	case len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}):
		return ".png"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return ".webp"
	}
	return ""
}

var imageTypes = map[string]string{".jpg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}

// identifyCover answers POST /api/identify/{fileId}/cover: the raw body is
// the cover image of the file's local track (at most 5 MiB, JPEG, PNG or
// WebP recognised by its content, whatever Content-Type says).
func (a *api) identifyCover(w http.ResponseWriter, r *http.Request) {
	id, ok := pathFileID(w, r)
	if !ok {
		return
	}
	f, err := a.idStore.GetFile(r.Context(), id)
	if err != nil {
		failIdentify(w, r, err)
		return
	}
	if f.Status != store.FileLocal || f.LocalTrackID == "" {
		writeError(w, http.StatusConflict, msgNotLocal)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCoverBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, msgCoverTooLarge)
			return
		}
		writeError(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	ext := imageExt(data)
	if ext == "" {
		writeError(w, http.StatusUnsupportedMediaType, msgBadCover)
		return
	}
	name, err := a.saveLocalCover(f.LocalTrackID, ext, data)
	if err != nil {
		slog.Error("local cover not saved", "track", f.LocalTrackID, "err", err)
		writeError(w, http.StatusInternalServerError, msgCoverSaveError)
		return
	}
	if err := a.idStore.SetLocalTrackCover(r.Context(), f.LocalTrackID, name); err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"coverUrl": "/api/local-covers/" + f.LocalTrackID})
}

// saveLocalCover writes the cover of a local track as <id><ext> in the local
// covers folder (through a temporary file, so a reader never sees half an
// image) and removes a previous cover of another type. It returns the base
// name.
func (a *api) saveLocalCover(trackID, ext string, data []byte) (string, error) {
	if err := os.MkdirAll(a.coversDir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(a.coversDir, ".cover-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	name := trackID + ext
	if err := os.Rename(tmpPath, filepath.Join(a.coversDir, name)); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	for other := range imageTypes {
		if other != ext {
			if err := os.Remove(filepath.Join(a.coversDir, trackID+other)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("old local cover not removed", "track", trackID, "err", err)
			}
		}
	}
	return name, nil
}

// identifyDelete answers DELETE /api/identify/{fileId}: an uploaded file
// still waiting in the incoming folder is deleted, from the disk first, then
// from the database. A file in the music folder (dropped, or anything outside
// the incoming folder) is never deleted by MusicHub.
func (a *api) identifyDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathFileID(w, r)
	if !ok {
		return
	}
	f, err := a.idStore.GetFile(r.Context(), id)
	if err != nil {
		failIdentify(w, r, err)
		return
	}
	switch {
	case f.Origin != store.OriginUploaded || !a.inIncoming(f.Path):
		writeError(w, http.StatusConflict, msgDroppedDelete)
		return
	case f.Status != store.FilePending:
		writeError(w, http.StatusConflict, msgNotPending)
		return
	}
	// Take the file out of the way first: if that fails, nothing changed.
	tomb := f.Path + upload.DeletingSuffix
	if err := os.Rename(f.Path, tomb); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Error("uploaded file not removed from disk", "file", f.ID, "path", f.Path, "err", err)
		writeError(w, http.StatusInternalServerError, msgDeleteFailed)
		return
	}
	if _, err := a.idStore.DeletePendingUpload(r.Context(), id); err != nil {
		// The file changed meanwhile (identified, for instance): put it back.
		if rerr := os.Rename(tomb, f.Path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			slog.Error("uploaded file not restored after a refused delete", "file", f.ID, "path", f.Path, "err", rerr)
		}
		if errors.Is(err, store.ErrNotUploaded) {
			writeError(w, http.StatusConflict, msgDroppedDelete)
			return
		}
		failIdentify(w, r, err)
		return
	}
	if err := os.Remove(tomb); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("deleted upload left on disk", "path", tomb, "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// inIncoming reports whether path is inside the incoming folder.
func (a *api) inIncoming(path string) bool {
	if a.incomingDir == "" {
		return false
	}
	rel, err := filepath.Rel(a.incomingDir, path)
	return err == nil && rel != "." && !filepath.IsAbs(rel) && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// localCover answers GET /api/local-covers/{id}: the cover uploaded for a
// local track. The browser keeps it privately and checks it again before
// reuse, since a new upload replaces it under the same URL.
func (a *api) localCover(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !fileIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, msgBadID)
		return
	}
	lt, err := a.idStore.GetLocalTrack(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ext := filepath.Ext(lt.CoverPath)
	ctype, known := imageTypes[ext]
	if lt.CoverPath == "" || !known {
		writeError(w, http.StatusNotFound, "Introuvable.")
		return
	}
	file, err := os.Open(filepath.Join(a.coversDir, lt.ID+ext))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Introuvable.")
			return
		}
		fail(w, r, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", info.ModTime(), file)
}
