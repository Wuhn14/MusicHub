// Package stream serves audio files to the player: the original bytes with
// HTTP Range support, or an MP3 converted on the fly with ffmpeg.
package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Wuhn14/MusicHub/internal/store"
)

// FileGetter looks a file up by id.
type FileGetter interface {
	GetFile(ctx context.Context, id string) (store.File, error)
}

// Transcoder converts a file to MP3 into w, starting at startMs.
type Transcoder interface {
	Transcode(ctx context.Context, path string, startMs, kbps int, w io.Writer) error
}

// maxStartSeconds caps start when the duration is unknown, so the millisecond
// conversion cannot overflow.
const maxStartSeconds = 1e7

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

var contentTypes = map[string]string{
	".mp3": "audio/mpeg", ".flac": "audio/flac",
	".m4a": "audio/mp4", ".aac": "audio/mp4",
	".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg",
	".wav": "audio/wav", ".aif": "audio/aiff", ".aiff": "audio/aiff",
}

// Server is the HTTP handler for /api/stream/{fileId}.
type Server struct {
	files FileGetter
	tools Transcoder
	max   func() int

	mu       sync.Mutex
	inflight int
}

// New returns a Server. maxTranscodes is read on every request, so a changed
// setting applies to the next conversion without a restart.
func New(files FileGetter, tools Transcoder, maxTranscodes func() int) *Server {
	return &Server{files: files, tools: tools, max: maxTranscodes}
}

// InFlight reports how many conversions are running.
func (s *Server) InFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight
}

func (s *Server) acquire() bool {
	limit := s.max()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight >= limit {
		return false
	}
	s.inflight++
	return true
}

func (s *Server) release() {
	s.mu.Lock()
	s.inflight--
	s.mu.Unlock()
}

func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Del("X-Content-Duration")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ServeHTTP serves GET and HEAD /api/stream/{fileId}?quality=&start=.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("fileId")
	if !idPattern.MatchString(id) {
		fail(w, http.StatusBadRequest, "Identifiant invalide.")
		return
	}
	q := r.URL.Query()
	kbps := 0
	switch q.Get("quality") {
	case "", "original":
	case "320", "192", "128":
		kbps, _ = strconv.Atoi(q.Get("quality"))
	default:
		fail(w, http.StatusBadRequest, "Qualité inconnue.")
		return
	}
	startSec := 0.0
	if kbps > 0 && q.Has("start") {
		v, err := strconv.ParseFloat(q.Get("start"), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			fail(w, http.StatusBadRequest, "Paramètre start invalide.")
			return
		}
		startSec = v
	}

	f, err := s.files.GetFile(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "Introuvable.")
		return
	}
	if err != nil {
		slog.Error("stream: loading file", "err", err)
		fail(w, http.StatusInternalServerError, "Erreur interne.")
		return
	}
	switch f.Status {
	case store.FileMissing:
		fail(w, http.StatusNotFound, "Introuvable.")
		return
	case store.FileUnreadable:
		fail(w, http.StatusUnprocessableEntity, "Ce fichier est illisible.")
		return
	}
	// Only the path recorded by the scanner is ever opened.
	file, err := os.Open(f.Path)
	if err != nil {
		fail(w, http.StatusNotFound, "Introuvable.")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		fail(w, http.StatusNotFound, "Introuvable.")
		return
	}

	if kbps == 0 {
		ct := contentTypes[strings.ToLower(filepath.Ext(f.Path))]
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeContent(w, r, "", info.ModTime(), file)
		return
	}
	file.Close() // ffmpeg reads the path itself

	if startSec*1000 >= float64(f.DurationMs) && f.DurationMs > 0 || startSec > maxStartSeconds {
		fail(w, http.StatusRequestedRangeNotSatisfiable, "Position au-delà de la fin du titre.")
		return
	}
	startMs := int(startSec * 1000)
	setHeaders := func() {
		h := w.Header()
		h.Set("Content-Type", "audio/mpeg")
		h.Set("Cache-Control", "no-store")
		if f.DurationMs > 0 {
			h.Set("X-Content-Duration", strconv.FormatFloat(float64(f.DurationMs-startMs)/1000, 'f', 1, 64))
		}
	}
	if r.Method == http.MethodHead {
		// Headers only: no conversion is started and no slot is taken.
		setHeaders()
		w.WriteHeader(http.StatusOK)
		return
	}
	if !s.acquire() {
		fail(w, http.StatusServiceUnavailable, "Trop de conversions en cours, réessaie dans un instant.")
		return
	}
	defer s.release()

	setHeaders()
	fw := &flushWriter{w: w, rc: http.NewResponseController(w)}
	err = s.tools.Transcode(r.Context(), f.Path, startMs, kbps, fw)
	switch {
	case err == nil && !fw.wrote:
		w.WriteHeader(http.StatusOK)
	case err != nil && r.Context().Err() == nil:
		slog.Warn("stream: conversion failed", "err", err)
		if !fw.wrote {
			fail(w, http.StatusInternalServerError, "Conversion impossible.")
		}
	}
}

// flushWriter flushes after every chunk so audio starts playing promptly.
type flushWriter struct {
	w     http.ResponseWriter
	rc    *http.ResponseController
	wrote bool
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if n > 0 {
		fw.wrote = true
		_ = fw.rc.Flush()
	}
	return n, err
}
