package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
)

// maxSegmentBytes is the longest file or folder name the organizer writes.
const maxSegmentBytes = 120

// maxCollisions bounds the " (2)", " (3)"… suffixes tried for one file.
const maxCollisions = 1000

// recordingTimeout bounds the MusicBrainz lookup of a recording's title.
const recordingTimeout = 30 * time.Second

// unknownTitle names a file whose title is unknown.
const unknownTitle = "Titre inconnu"

// TagWriter rewrites the tags of an audio file (*media.Tools in production).
type TagWriter interface {
	WriteTags(ctx context.Context, path string, t media.Tags) error
}

// SettingsGetter reads the user settings (*settings.Service in production).
type SettingsGetter interface {
	Get(ctx context.Context) (settings.Settings, error)
}

// RecordingGetter looks a recording up on MusicBrainz (*musicbrainz.Client
// in production).
type RecordingGetter interface {
	GetRecording(ctx context.Context, id string) (musicbrainz.RecordingHit, error)
}

// renameFile moves a file; tests replace it to simulate a move between two
// disks, where renaming fails.
var renameFile = os.Rename

// Organizer files identified and local tracks into the music folder:
// <music>/<Album artist>/<Album> (<year>)/<NN> - <Title>.<ext>.
type Organizer struct {
	st       *store.Store
	tools    TagWriter
	musicDir string
	settings SettingsGetter
	now      func() time.Time

	// Recordings, when set, gives the title and artist credit of an
	// identified recording; without it (or when MusicBrainz cannot be
	// reached) they come from the catalog album and the file's tags.
	Recordings RecordingGetter

	// mu serializes placements, so two files never pick the same free name.
	mu sync.Mutex
}

// NewOrganizer returns an organizer moving files into musicDir. tools may be
// nil (tags are then never written).
func NewOrganizer(st *store.Store, tools TagWriter, musicDir string, settings SettingsGetter) *Organizer {
	if abs, err := filepath.Abs(musicDir); err == nil {
		musicDir = abs
	}
	return &Organizer{st: st, tools: tools, musicDir: musicDir, settings: settings, now: time.Now}
}

// placement is what the organizer knows of a track: where it goes and the
// tags it gets.
type placement struct {
	title, artist, albumArtist, album, year string
	disc, position                          int
	tags                                    media.Tags
	// titleFromAlbum tells that the title is the album's track title.
	titleFromAlbum bool
}

// Place moves an identified or local file into the music folder and writes
// its tags, according to the settings: an uploaded file is always moved and
// gets its tags when "write tags into uploaded files" is on; a dropped file
// is moved only when "organize dropped files" is on, and gets its tags when
// "write tags into dropped files" is on. The file keeps its id and origin;
// its stored path follows the move. Any other file is returned unchanged.
//
// The file is never lost: an existing file is never overwritten, a move
// between two disks copies, checks the copy and only then removes the
// source, and on any failure the file stays where it was (still playable)
// and an error is returned. A failure to write the tags is only logged.
func (o *Organizer) Place(ctx context.Context, fileID string) (store.File, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	f, err := o.st.GetFile(ctx, fileID)
	if err != nil {
		return store.File{}, err
	}
	if f.Status != store.FileIdentified && f.Status != store.FileLocal {
		return f, nil
	}
	set, err := o.settings.Get(ctx)
	if err != nil {
		return f, fmt.Errorf("reading settings: %w", err)
	}
	move := f.Origin == store.OriginUploaded || set.OrganizeDropped
	writeTags := set.WriteTagsDropped
	if f.Origin == store.OriginUploaded {
		writeTags = set.WriteTagsUploaded
	}
	writeTags = writeTags && o.tools != nil
	if !move && !writeTags {
		return f, nil
	}
	p, err := o.describe(ctx, f)
	if err != nil {
		return f, err
	}

	changed := false
	if move {
		dst, err := o.moveInto(ctx, f, p)
		if err != nil {
			return f, err
		}
		changed = dst != f.Path
		if changed {
			slog.Info("fichier rangé", "file", f.ID, "from", f.Path, "to", dst)
			f.Path = dst
		}
	}

	var written *media.Tags
	if writeTags {
		if err := o.tools.WriteTags(ctx, f.Path, p.tags); err != nil {
			slog.Warn("tags non écrits, le fichier est gardé tel quel", "file", f.ID, "path", f.Path, "err", err)
		} else {
			written = &p.tags
			changed = true
		}
	}
	if !changed {
		return f, nil
	}
	info, err := os.Stat(f.Path)
	if err != nil {
		return f, fmt.Errorf("reading placed file: %w", err)
	}
	if err := o.st.FinishPlacing(ctx, f.ID, info.Size(), info.ModTime(), written, o.now()); err != nil {
		return f, err
	}
	return o.st.GetFile(ctx, f.ID)
}

// PlaceStranded places the uploaded files that were identified or made local
// but are still in the incoming folder (MusicHub stopped before it could move
// them, or the move failed). Failures are logged; it returns how many files
// were moved out.
func (o *Organizer) PlaceStranded(ctx context.Context, incomingDir string) int {
	prefix := strings.TrimRight(incomingDir, `/\`) + string(filepath.Separator)
	moved := 0
	for _, status := range []string{store.FileIdentified, store.FileLocal} {
		files, err := o.st.ListFiles(ctx, store.FileFilter{Status: status, Origin: store.OriginUploaded})
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("rangement : fichiers envoyés illisibles", "err", err)
			}
			return moved
		}
		for _, f := range files {
			if !strings.HasPrefix(f.Path, prefix) {
				continue
			}
			if ctx.Err() != nil {
				return moved
			}
			placed, err := o.Place(ctx, f.ID)
			if err != nil {
				slog.Error("rangement impossible, le fichier reste dans le dossier d'envoi", "file", f.ID, "err", err)
				continue
			}
			if !strings.HasPrefix(placed.Path, prefix) {
				moved++
			}
		}
	}
	return moved
}

// describe gathers the title, artists, album and tags of a file from the
// catalog (identified files) or from its local track.
func (o *Organizer) describe(ctx context.Context, f store.File) (placement, error) {
	var p placement
	switch f.Status {
	case store.FileLocal:
		lt, err := o.st.GetLocalTrack(ctx, f.LocalTrackID)
		if err != nil {
			return p, fmt.Errorf("reading local track: %w", err)
		}
		p.title, p.artist = strings.TrimSpace(lt.Title), strings.TrimSpace(lt.Artist)
		p.albumArtist, p.album = strings.TrimSpace(lt.AlbumArtist), strings.TrimSpace(lt.Album)
		if lt.Year > 0 {
			p.year = strconv.Itoa(lt.Year)
		}
		p.disc, p.position = lt.Disc, lt.Position
	default:
		p.title, p.artist = strings.TrimSpace(f.Tags.Title), strings.TrimSpace(f.Tags.Artist)
		p.disc, p.position = f.Disc, f.Position
		if f.AlbumID != "" {
			al, err := o.st.GetAlbum(ctx, f.AlbumID)
			switch {
			case err == nil:
				p.album, p.albumArtist = strings.TrimSpace(al.Title), strings.TrimSpace(al.ArtistName)
				p.year = yearFromDate(al.FirstReleaseDate)
				for _, t := range al.Tracks {
					if t.RecordingID == f.RecordingID && strings.TrimSpace(t.Title) != "" {
						p.title, p.titleFromAlbum = strings.TrimSpace(t.Title), true
						break
					}
				}
			case errors.Is(err, store.ErrNotFound):
				slog.Warn("rangement : album absent du catalogue, rangé sans album", "file", f.ID, "album", f.AlbumID)
			default:
				return p, fmt.Errorf("reading album: %w", err)
			}
		}
		if o.Recordings != nil && f.RecordingID != "" {
			rctx, cancel := context.WithTimeout(ctx, recordingTimeout)
			hit, err := o.Recordings.GetRecording(rctx, f.RecordingID)
			cancel()
			switch {
			case err != nil:
				slog.Warn("rangement : enregistrement illisible sur MusicBrainz, tags du fichier gardés", "file", f.ID, "err", err)
			default:
				if t := strings.TrimSpace(hit.Title); t != "" && !p.titleFromAlbum {
					p.title = t
				}
				if a := strings.TrimSpace(hit.ArtistName); a != "" {
					p.artist = a
				}
			}
		}
		p.tags.MBRecordingID = f.RecordingID
		if p.album != "" {
			p.tags.MBReleaseGroupID = f.AlbumID
		}
	}
	if p.artist == "" {
		p.artist = p.albumArtist
	}
	if p.albumArtist == "" && p.album != "" {
		p.albumArtist = p.artist
	}
	p.tags.Title, p.tags.Artist, p.tags.AlbumArtist = p.title, p.artist, p.albumArtist
	p.tags.Album, p.tags.Date = p.album, p.year
	p.tags.Track, p.tags.Disc = p.position, p.disc
	return p, nil
}

// target returns the folder and the name parts of a file's place in the
// music folder.
func (o *Organizer) target(p placement, ext string) (dir, stem string) {
	title := p.title
	if title == "" {
		title = unknownTitle
	}
	if p.album == "" {
		artist := p.artist
		if artist == "" {
			artist = unknownArtist
		}
		return filepath.Join(o.musicDir, sanitizeSegment(artist, maxSegmentBytes), noAlbumTitle), title
	}
	albumDir := sanitizeSegment(p.album, maxSegmentBytes)
	if p.year != "" {
		suffix := " (" + p.year + ")"
		albumDir = sanitizeSegment(p.album, maxSegmentBytes-len(suffix)) + suffix
	}
	stem = title
	if p.position > 0 {
		num := fmt.Sprintf("%02d", p.position)
		if p.disc > 1 {
			num = fmt.Sprintf("%d-%02d", p.disc, p.position)
		}
		stem = num + " - " + title
	}
	return filepath.Join(o.musicDir, sanitizeSegment(p.albumArtist, maxSegmentBytes), albumDir), stem
}

// fileName builds a sanitized file name; n > 1 adds the " (n)" collision
// suffix. The whole name stays within maxSegmentBytes.
func fileName(stem string, n int, ext string) string {
	suffix := ""
	if n > 1 {
		suffix = " (" + strconv.Itoa(n) + ")"
	}
	return sanitizeSegment(stem, maxSegmentBytes-len(suffix)-len(ext)) + suffix + ext
}

// moveInto moves the file to its place and records the new path. It returns
// the path the file ends up at (its current one when it is already in
// place).
func (o *Organizer) moveInto(ctx context.Context, f store.File, p placement) (string, error) {
	ext := strings.ToLower(filepath.Ext(f.Path))
	dir, stem := o.target(p, ext)
	if inPlace(f.Path, dir, stem, ext) {
		return f.Path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating folder: %w", err)
	}
	for n := 1; n <= maxCollisions; n++ {
		dst := filepath.Join(dir, fileName(stem, n, ext))
		if _, err := os.Lstat(dst); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		// Record the destination first: a scan that finds the file there
		// then knows it, instead of adding it a second time.
		if err := o.st.SetFilePath(ctx, f.ID, dst); errors.Is(err, store.ErrPathTaken) {
			continue
		} else if err != nil {
			return "", err
		}
		if err := moveFile(f.Path, dst); err != nil {
			if rerr := o.st.SetFilePath(context.WithoutCancel(ctx), f.ID, f.Path); rerr != nil {
				slog.Error("rangement : chemin d'origine non restauré en base", "file", f.ID, "path", f.Path, "err", rerr)
			}
			return "", fmt.Errorf("moving file: %w", err)
		}
		return dst, nil
	}
	return "", fmt.Errorf("no free name for %s in %s", stem, dir)
}

// inPlace reports whether path is already the place computed for the file,
// possibly with a collision suffix.
func inPlace(path, dir, stem, ext string) bool {
	if filepath.Dir(path) != dir {
		return false
	}
	base := filepath.Base(path)
	if base == fileName(stem, 1, ext) {
		return true
	}
	prefix := sanitizeSegment(stem, maxSegmentBytes-len(ext)-len(" (1000)"))
	rest, ok := strings.CutPrefix(base, prefix)
	if !ok {
		return false
	}
	rest, ok = strings.CutSuffix(rest, ext)
	if !ok || !strings.HasPrefix(rest, " (") || !strings.HasSuffix(rest, ")") {
		return false
	}
	n, err := strconv.Atoi(rest[2 : len(rest)-1])
	return err == nil && n > 1
}

// moveFile moves src to dst, which must not exist. A rename is tried first;
// when it fails (another disk), the file is copied next to dst, synced,
// checked and renamed into place, and only then is src removed. On failure
// src is left untouched and no partial file remains.
func moveFile(src, dst string) error {
	rerr := renameFile(src, dst)
	if rerr == nil {
		return nil
	}
	if _, err := os.Lstat(src); err != nil {
		return rerr
	}
	slog.Debug("rangement : renommage impossible, copie", "from", src, "to", dst, "err", rerr)
	if err := copyInto(src, dst); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		// The copy is complete and recorded; the leftover source is harmless.
		slog.Warn("rangement : fichier d'origine non supprimé après copie", "path", src, "err", err)
	}
	return nil
}

// copyInto copies src to dst through a hidden temporary file in dst's folder
// (the scanner skips hidden files) and refuses to overwrite dst.
func copyInto(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".musichub-move-*"+filepath.Ext(dst))
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()
	n, err := io.Copy(tmp, in)
	if err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if n != info.Size() {
		return fmt.Errorf("copy is %d bytes, source is %d", n, info.Size())
	}
	got, err := os.Stat(tmpPath)
	if err != nil {
		return err
	}
	if got.Size() != info.Size() {
		return fmt.Errorf("copy is %d bytes on disk, source is %d", got.Size(), info.Size())
	}
	_ = os.Chmod(tmpPath, info.Mode().Perm())
	_ = os.Chtimes(tmpPath, info.ModTime(), info.ModTime())
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s: %w", dst, fs.ErrExist)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	ok = true
	return nil
}

// windowsReserved are the device names Windows refuses as file names, with
// or without an extension.
var windowsReserved = func() map[string]bool {
	m := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}
	for i := 1; i <= 9; i++ {
		m["COM"+strconv.Itoa(i)] = true
		m["LPT"+strconv.Itoa(i)] = true
	}
	return m
}()

// sanitizeSegment makes s safe as one file or folder name on Windows and
// Unix: forbidden and control characters become "_", a leading dot (hidden
// file) becomes "_", trailing spaces and dots are removed, a Windows device
// name gets "_" appended, the result is at most limit bytes (cut on a UTF-8
// boundary) and never empty.
func sanitizeSegment(s string, limit int) string {
	if limit < 1 {
		limit = 1
	}
	s = strings.ToValidUTF8(s, "_")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, ".") {
		s = "_" + s[1:]
	}
	clean := func(v string) string {
		v = truncateUTF8(v, limit)
		v = strings.TrimRight(v, " .")
		if v == "" {
			v = "_"
		}
		return v
	}
	s = clean(s)
	base, rest, _ := strings.Cut(s, ".")
	if windowsReserved[strings.ToUpper(strings.TrimRight(base, " "))] {
		if rest != "" || strings.Contains(s, ".") {
			s = base + "_." + rest
		} else {
			s = base + "_"
		}
		s = clean(s)
	}
	return s
}

// truncateUTF8 cuts s to at most n bytes without splitting a character.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
