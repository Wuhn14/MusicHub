// Package library keeps the database in sync with the music folder.
package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/store"
)

// missingKeepDays is how long a vanished file stays in the database, so a
// temporarily unmounted disk does not lose identifications.
const missingKeepDays = 30

var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true,
	".oga": true, ".opus": true, ".wav": true, ".aif": true, ".aiff": true,
}

// Prober reads the technical properties and tags of an audio file.
// *media.Tools implements it.
type Prober interface {
	Probe(ctx context.Context, path string) (media.Probe, error)
}

// Report summarises one scan.
type Report struct {
	Seen       int // audio files found on disk
	Added      int // new files recorded
	Updated    int // known files whose size or date changed
	Unreadable int // files that are not readable audio, found during this scan
	Errors     int // files skipped on a transient probe error, retried next scan
	Missing    int // files newly flagged as missing
	// Skipped counts the folders and files that could not be read (most
	// often for lack of permission). What they hold is never flagged as
	// missing.
	Skipped int
}

// availabilityChecker is implemented by probers that can tell up front that
// their executable is missing (*media.Tools does).
type availabilityChecker interface{ FFprobeAvailable() bool }

// Scanner walks the music folder and records audio files in the store.
type Scanner struct {
	st       *store.Store
	tools    Prober
	musicDir string
	now      func() time.Time

	// OnFileAdded, when set, is called for every file added or modified that
	// ends up pending identification.
	OnFileAdded func(store.File)
	// IncomingDir is the folder uploaded files wait in. The scan never walks
	// it, so the uploaded files still there are never flagged as missing.
	IncomingDir string

	// Test seams: the folder walk and the readability check of a file.
	walk     func(root string, fn fs.WalkDirFunc) error
	readable func(path string) error
}

// NewScanner builds a scanner for musicDir.
func NewScanner(st *store.Store, tools Prober, musicDir string, now func() time.Time) *Scanner {
	return &Scanner{st: st, tools: tools, musicDir: musicDir, now: now, walk: filepath.WalkDir, readable: checkReadable}
}

// checkReadable opens the file and closes it at once: a file the server may
// not read fails with fs.ErrPermission.
func checkReadable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// systemDirs are folders that NAS systems create inside shared folders
// (thumbnails, recycle bins, snapshots). Like hidden folders, they are never
// scanned. Names are compared without case.
var systemDirs = map[string]bool{
	"@eadir": true, "#recycle": true, "#snapshot": true, ".@__thumb": true, "@recycle": true, "lost+found": true,
}

// ignoredName reports whether a file or folder is hidden by its name.
func ignoredName(name string, dir bool) bool {
	return strings.HasPrefix(name, ".") || (dir && systemDirs[strings.ToLower(name)])
}

// Scan walks the music folder once. Files whose size and modification time are
// unchanged are not probed again. A missing folder is an error and nothing is
// created.
func (s *Scanner) Scan(ctx context.Context) (Report, error) {
	var rep Report
	root, err := filepath.Abs(s.musicDir)
	if err != nil {
		return rep, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return rep, fmt.Errorf("dossier de musique inaccessible (%s) : %w", s.musicDir, err)
	}
	if !info.IsDir() {
		return rep, fmt.Errorf("le dossier de musique n'est pas un dossier : %s", s.musicDir)
	}

	if c, ok := s.tools.(availabilityChecker); ok && !c.FFprobeAvailable() {
		return rep, errors.New("ffprobe est introuvable : l'analyse des fichiers est impossible")
	}

	started := s.now()
	// Paths that could not be read: what they hold is not flagged missing.
	var excluded []string
	var seenUnchanged []string

	flush := func() error {
		if len(seenUnchanged) == 0 {
			return nil
		}
		err := s.st.MarkSeen(ctx, seenUnchanged, started)
		seenUnchanged = seenUnchanged[:0]
		return err
	}

	walkErr := s.walk(root, func(path string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if path == root {
				return err
			}
			// An unreadable subfolder or file: keep going, but do not flag
			// what it holds as missing at the end.
			slog.Warn("élément illisible pendant le scan, ignoré", "path", path, "err", err)
			excluded = append(excluded, path)
			rep.Skipped++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path != root && ignoredName(d.Name(), d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !audioExts[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		fi, err := os.Stat(path) // follows a symlinked file
		if err != nil {
			slog.Warn("fichier inaccessible pendant le scan, ignoré", "path", path, "err", err)
			excluded = append(excluded, path)
			rep.Skipped++
			return nil
		}
		if fi.IsDir() {
			return nil
		}
		rep.Seen++
		return s.handleFile(ctx, path, fi, started, &rep, &excluded, &seenUnchanged, flush)
	})
	if walkErr != nil {
		return rep, walkErr
	}
	if err := flush(); err != nil {
		return rep, err
	}
	n, err := s.st.MarkMissing(ctx, started, s.now(), s.IncomingDir, excluded...)
	if err != nil {
		return rep, err
	}
	rep.Missing = n
	if _, err := s.st.PurgeMissing(ctx, s.now().AddDate(0, 0, -missingKeepDays)); err != nil {
		return rep, err
	}
	return rep, nil
}

func (s *Scanner) handleFile(ctx context.Context, path string, fi fs.FileInfo, started time.Time,
	rep *Report, excluded *[]string, seenUnchanged *[]string, flush func() error) error {
	existing, err := s.st.GetFileByPath(ctx, path)
	known := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if known && existing.Size == fi.Size() && existing.Mtime.Equal(fi.ModTime()) {
		*seenUnchanged = append(*seenUnchanged, existing.ID)
		if len(*seenUnchanged) >= 500 {
			return flush()
		}
		return nil
	}

	f := store.File{Path: path, Size: fi.Size(), Mtime: fi.ModTime().UTC(), LastSeen: started, AddedAt: started}
	// A file the server may not read is recorded as unreadable, like a
	// file that is not audio: it is probed again once its size or date
	// changes (a fixed permission alone changes neither: rescan by touching
	// the file).
	var pr media.Probe
	perr := s.readable(path)
	if errors.Is(perr, fs.ErrPermission) {
		perr = fmt.Errorf("%w: %w", media.ErrNotAudio, perr)
	} else if perr == nil {
		pr, perr = s.tools.Probe(ctx, path)
	}
	if perr != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if !errors.Is(perr, media.ErrNotAudio) {
			// Transient (missing tool, timeout, slow disk): record nothing so
			// the file is retried, and do not flag anything as missing.
			slog.Warn("analyse impossible, nouvel essai au prochain scan", "path", path, "err", perr)
			rep.Errors++
			*excluded = append(*excluded, path)
			return nil
		}
		f.Status = store.FileUnreadable
		rep.Unreadable++
	} else {
		f.Status = store.FilePending
		f.Format, f.Codec, f.BitRate, f.SampleRate = pr.Format, pr.Codec, pr.BitRate, pr.SampleRate
		f.DurationMs, f.Lossless, f.Tags = pr.DurationMs, pr.Lossless, pr.Tags
		f.Disc, f.Position = pr.Tags.Disc, pr.Tags.Track
	}
	stored, err := s.st.UpsertScannedFile(ctx, f)
	if err != nil {
		return err
	}
	if known {
		rep.Updated++
	} else {
		rep.Added++
	}
	if stored.Status == store.FilePending && s.OnFileAdded != nil {
		s.OnFileAdded(stored)
	}
	return nil
}
