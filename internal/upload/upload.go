// Package upload receives audio files sent from the browser: it checks the
// extension, writes the body to the incoming folder under a name of its own,
// verifies that the result is readable audio and records it as a pending
// uploaded file.
package upload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
)

// Errors returned by Receive.
var (
	ErrUnsupportedFormat = errors.New("upload: unsupported file extension")
	ErrEmpty             = errors.New("upload: empty file")
	ErrNotAudio          = errors.New("upload: file is not readable audio")
	ErrTooLarge          = errors.New("upload: file too large")
)

// partSuffix marks a file still being written.
const partSuffix = ".part"

// defaultMaxBytes is used when the settings cannot be read.
const defaultMaxBytes = 2 << 30

var allowedExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true,
	".oga": true, ".opus": true, ".wav": true, ".aif": true, ".aiff": true,
}

// Prober reads the technical properties and tags of an audio file.
// *media.Tools implements it.
type Prober interface {
	Probe(ctx context.Context, path string) (media.Probe, error)
}

// SettingsGetter gives the current settings. *settings.Service implements it.
type SettingsGetter interface {
	Get(ctx context.Context) (settings.Settings, error)
}

// Receiver stores uploaded files.
type Receiver struct {
	st         *store.Store
	tools      Prober
	dir        string
	settings   SettingsGetter
	onAccepted func(store.File)
	now        func() time.Time
}

// New returns a Receiver writing into incomingDir. onAccepted, when not nil,
// is called with each accepted file (to queue it for identification).
func New(st *store.Store, tools Prober, incomingDir string, settings SettingsGetter, onAccepted func(store.File)) *Receiver {
	if abs, err := filepath.Abs(incomingDir); err == nil {
		incomingDir = abs
	}
	return &Receiver{st: st, tools: tools, dir: incomingDir, settings: settings, onAccepted: onAccepted, now: time.Now}
}

// MaxBytes is the largest accepted file size.
func (r *Receiver) MaxBytes(ctx context.Context) int64 {
	if r.settings != nil {
		if s, err := r.settings.Get(ctx); err == nil && s.MaxUploadBytes > 0 {
			return s.MaxUploadBytes
		}
	}
	return defaultMaxBytes
}

// Extension returns the lowercased extension of a client-supplied file name
// (the last path segment only, whatever separator it uses), and whether it is
// an accepted audio extension. Nothing else of the name is ever used.
func Extension(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext, allowedExts[ext]
}

// baseName is the last segment of a client-supplied file name, whatever
// separator it uses, cut to 255 bytes. It is only ever displayed.
func baseName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToValidUTF8(name, "")
	for len(name) > 255 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

// CleanPartFiles removes the leftovers of interrupted uploads from dir
// (creating it when needed) and returns how many it removed.
func CleanPartFiles(dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), partSuffix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			slog.Warn("upload: removing leftover file", "name", e.Name(), "err", err)
			continue
		}
		n++
	}
	return n, nil
}

func randomName() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(b)
}

// Receive stores the content of r as an uploaded file. name is only used for
// its extension. On any error nothing is left on disk or in the database.
func (r *Receiver) Receive(ctx context.Context, name string, body io.Reader) (store.File, error) {
	ext, ok := Extension(name)
	if !ok {
		return store.File{}, ErrUnsupportedFormat
	}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return store.File{}, err
	}
	base := filepath.Join(r.dir, randomName()+ext)
	part := base + partSuffix
	out, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return store.File{}, err
	}
	// keep reports whether the part file must survive; every early return
	// removes it.
	keep := false
	defer func() {
		if !keep {
			out.Close()
			os.Remove(part)
		}
	}()

	limit := r.MaxBytes(ctx)
	n, err := io.Copy(out, io.LimitReader(&ctxReader{ctx, body}, limit+1))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return store.File{}, ErrTooLarge
		}
		return store.File{}, err
	}
	switch {
	case n > limit:
		return store.File{}, ErrTooLarge
	case n == 0:
		return store.File{}, ErrEmpty
	}
	if err := out.Close(); err != nil {
		return store.File{}, err
	}

	pr, err := r.tools.Probe(ctx, part)
	if err != nil {
		if errors.Is(err, media.ErrNotAudio) {
			return store.File{}, ErrNotAudio
		}
		return store.File{}, fmt.Errorf("upload: probing file: %w", err)
	}
	if err := os.Rename(part, base); err != nil {
		return store.File{}, err
	}
	keep = true
	now := r.now().UTC()
	f, err := r.st.InsertUploadedFile(ctx, store.File{
		Path: base, Size: n, Mtime: now, Format: pr.Format, Codec: pr.Codec, BitRate: pr.BitRate,
		SampleRate: pr.SampleRate, DurationMs: pr.DurationMs, Lossless: pr.Lossless, Tags: pr.Tags,
		Disc: pr.Tags.Disc, Position: pr.Tags.Track, AddedAt: now, LastSeen: now,
		UploadName: baseName(name),
	})
	if err != nil {
		os.Remove(base)
		return store.File{}, err
	}
	if r.onAccepted != nil {
		r.onAccepted(f)
	}
	return f, nil
}

// ctxReader fails reads once ctx is done, so a stalled client cannot keep an
// upload alive after the request was cancelled.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
