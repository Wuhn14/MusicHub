package library

import (
	"bytes"
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
	// The MusicBrainz lookup can be slow: done before taking the lock.
	rec := o.recording(ctx, fileID)
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
	p, err := o.describe(ctx, f, rec)
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
	if err := o.st.FinishPlacing(context.WithoutCancel(ctx), f.ID, info.Size(), info.ModTime(), written, o.now()); err != nil {
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

// recording returns the MusicBrainz recording an identified file is linked
// to: the one the caller already fetched (musicbrainz.WithRecording), or a
// lookup through Recordings. nil when unknown; the tags then name the file.
func (o *Organizer) recording(ctx context.Context, fileID string) *musicbrainz.RecordingHit {
	f, err := o.st.GetFile(ctx, fileID)
	if err != nil || f.Status != store.FileIdentified || f.RecordingID == "" {
		return nil
	}
	if hit, ok := musicbrainz.RecordingFrom(ctx, f.RecordingID); ok {
		return &hit
	}
	if o.Recordings == nil {
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, recordingTimeout)
	defer cancel()
	hit, err := o.Recordings.GetRecording(rctx, f.RecordingID)
	if err != nil {
		slog.Warn("rangement : enregistrement illisible sur MusicBrainz, tags du fichier gardés", "file", f.ID, "err", err)
		return nil
	}
	return &hit
}

// describe gathers the title, artists, album and tags of a file from the
// catalog (identified files), the recording rec (when not nil) or its local
// track.
func (o *Organizer) describe(ctx context.Context, f store.File, rec *musicbrainz.RecordingHit) (placement, error) {
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
		if rec != nil && rec.RecordingID == f.RecordingID {
			if t := strings.TrimSpace(rec.Title); t != "" && !p.titleFromAlbum {
				p.title = t
			}
			if a := strings.TrimSpace(rec.ArtistName); a != "" {
				p.artist = a
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
		// Record the destination first, with the source: a scan that finds
		// the file there then knows it instead of adding it a second time,
		// and a stop during the move is repaired at the next start
		// (RepairPlacing).
		if err := o.st.BeginPlacing(ctx, f.ID, dst, f.Path); errors.Is(err, store.ErrPathTaken) {
			continue
		} else if err != nil {
			return "", err
		}
		err := moveFile(f.Path, dst)
		if err == nil {
			return dst, nil
		}
		if rerr := o.st.SetFilePath(context.WithoutCancel(ctx), f.ID, f.Path); rerr != nil {
			slog.Error("rangement : chemin d'origine non restauré en base", "file", f.ID, "path", f.Path, "err", rerr)
			return "", fmt.Errorf("moving file: %w", err)
		}
		if errors.Is(err, fs.ErrExist) {
			// The destination appeared meanwhile: try the next name.
			continue
		}
		return "", fmt.Errorf("moving file: %w", err)
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

// Hooks replaced by tests to simulate other filesystems.
var (
	// linkFile gives a file a second name, failing when newname exists.
	linkFile = os.Link
	// renameFile moves a file (replacing newname on some systems).
	renameFile = os.Rename
	// removeFile deletes a name (a read-only source folder makes it fail).
	removeFile = os.Remove
)

// errSourceKept reports a move undone because the source could not be removed:
// keeping both names would let the next scan add the source a second time.
var errSourceKept = errors.New("source could not be removed, move undone")

// placeNoReplace gives src the name dst without ever replacing an existing
// dst (an error matching fs.ErrExist then). A hard link does it atomically;
// where links are not supported, dst is checked right before a rename. Any
// other failure (another disk, for instance) is returned as is. When src
// cannot be removed after the link, the link is undone and errSourceKept is
// returned, so the file stays where it was.
func placeNoReplace(src, dst string) error {
	lerr := linkFile(src, dst)
	if lerr == nil {
		if err := removeFile(src); err != nil {
			undoLink(src, dst)
			return fmt.Errorf("%w: %v", errSourceKept, err)
		}
		return nil
	}
	if errors.Is(lerr, fs.ErrExist) {
		return lerr
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s: %w", dst, fs.ErrExist)
	}
	return renameFile(src, dst)
}

// moveFile moves src to dst without replacing an existing dst (an error
// matching fs.ErrExist). When the file cannot be renamed (another disk), it
// is copied next to dst, synced, checked and given its name, and only then
// is src removed. On failure src is left untouched and no partial file
// remains.
func moveFile(src, dst string) error {
	merr := placeNoReplace(src, dst)
	if merr == nil || errors.Is(merr, fs.ErrExist) {
		return merr
	}
	if _, err := os.Lstat(src); err != nil {
		return merr
	}
	slog.Debug("rangement : renommage impossible, copie", "from", src, "to", dst, "err", merr)
	if err := copyInto(src, dst); err != nil {
		return err
	}
	if err := removeFile(src); err != nil {
		// Both copies would stay in the library: drop ours, keep the source.
		undoCopy(src, dst)
		return fmt.Errorf("%w: %v", errSourceKept, err)
	}
	return nil
}

// undoLink removes dst only when it is another name of src, the link just made.
func undoLink(src, dst string) {
	si, serr := os.Stat(src)
	di, derr := os.Stat(dst)
	if serr != nil || derr != nil || !os.SameFile(si, di) {
		slog.Error("rangement : lien non annulé, vérifie les deux noms", "source", src, "destination", dst)
		return
	}
	if err := removeFile(dst); err != nil {
		slog.Error("rangement : lien non annulé", "destination", dst, "err", err)
	}
}

// undoCopy removes dst only when its bytes are those of src, the copy just made.
func undoCopy(src, dst string) {
	same, err := sameContent(src, dst)
	if err != nil || !same {
		slog.Error("rangement : copie non annulée, vérifie les deux fichiers", "source", src, "destination", dst, "err", err)
		return
	}
	if err := removeFile(dst); err != nil {
		slog.Error("rangement : copie non annulée", "destination", dst, "err", err)
	}
}

// tempPrefixes start the names of the temporary files the organizer and the
// tag writer create next to their destination while they work.
var tempPrefixes = []string{".musichub-move-", ".musichub-tags-"}

// copyInto copies src to dst through a hidden temporary file in dst's folder
// (the scanner skips hidden files) and never replaces dst.
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
	tmp, err := os.CreateTemp(filepath.Dir(dst), tempPrefixes[0]+"*"+filepath.Ext(dst))
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpPath) // already gone once it took dst's name
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
	return placeNoReplace(tmpPath, dst)
}

// RepairPlacing settles the moves a stop interrupted. Call it at startup,
// before the first scan. When only the destination exists, the row keeps
// it. When both exist, the source is dropped only if the destination is
// provably the same file (another name of it, or identical bytes); otherwise
// the row goes back to the source, both files are kept, and PlaceStranded
// moves it again. Errors on one row are logged and the next rows are still
// repaired. The temporary files left in the music folder are removed later
// by RemoveTemps, which walks the whole folder.
func (o *Organizer) RepairPlacing(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	files, err := o.st.ListPlacing(ctx)
	if err != nil {
		slog.Error("rangements interrompus illisibles", "err", err)
	}
	for _, f := range files {
		if err := o.repairOne(ctx, f); err != nil {
			slog.Error("rangement interrompu non réparé", "file", f.ID, "path", f.Path, "from", f.PlacingFrom, "err", err)
		}
	}
	return err
}

// RemoveTemps deletes the temporary files a stop left in the music folder
// and returns how many it removed. The walk of a large folder can take a
// while, so it runs in the background after startup: the folder is walked
// without the lock, then the leftovers are removed while no placing runs
// (every temporary file of a placing that ran meanwhile is gone by then).
func (o *Organizer) RemoveTemps(ctx context.Context) (int, error) {
	found, err := findTemps(ctx, o.musicDir)
	if err != nil || len(found) == 0 {
		return 0, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for _, path := range found {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if rerr := os.Remove(path); rerr == nil {
			n++
		} else if !errors.Is(rerr, fs.ErrNotExist) {
			slog.Warn("fichier temporaire non supprimé", "path", path, "err", rerr)
		}
	}
	if n > 0 {
		slog.Info("fichiers temporaires de rangement supprimés", "count", n)
	}
	return n, nil
}

// repairOne settles one interrupted move (see RepairPlacing).
func (o *Organizer) repairOne(ctx context.Context, f store.File) error {
	dstInfo, derr := os.Stat(f.Path)
	srcInfo, serr := os.Stat(f.PlacingFrom)
	if serr != nil && !errors.Is(serr, fs.ErrNotExist) {
		return serr
	}
	if derr != nil && !errors.Is(derr, fs.ErrNotExist) {
		return derr
	}
	srcExists, dstExists := serr == nil, derr == nil
	keepDst := func() error {
		return o.st.FinishPlacing(ctx, f.ID, dstInfo.Size(), dstInfo.ModTime(), nil, o.now())
	}
	switch {
	case dstExists && !srcExists:
		if err := keepDst(); err != nil {
			return err
		}
		slog.Info("rangement interrompu terminé", "file", f.ID, "path", f.Path)
	case dstExists && srcExists:
		same := os.SameFile(srcInfo, dstInfo)
		if !same {
			var err error
			if same, err = sameContent(f.PlacingFrom, f.Path); err != nil {
				return err
			}
		}
		if !same {
			// Another file took the destination's name: it is not ours.
			if err := o.st.SetFilePath(ctx, f.ID, f.PlacingFrom); err != nil {
				return err
			}
			slog.Warn("rangement interrompu : la destination est un autre fichier, les deux sont gardés", "file", f.ID, "path", f.PlacingFrom, "other", f.Path)
			return nil
		}
		if err := keepDst(); err != nil {
			return err
		}
		// The move was complete except for removing the source.
		if err := os.Remove(f.PlacingFrom); err != nil {
			slog.Warn("rangement interrompu : ancien fichier non supprimé", "path", f.PlacingFrom, "err", err)
		}
		slog.Info("rangement interrompu terminé", "file", f.ID, "path", f.Path)
	case srcExists:
		if err := o.st.SetFilePath(ctx, f.ID, f.PlacingFrom); err != nil {
			return err
		}
		slog.Info("rangement interrompu annulé, le fichier sera rangé à nouveau", "file", f.ID, "path", f.PlacingFrom)
	default:
		slog.Error("rangement interrompu : fichier introuvable", "file", f.ID, "path", f.Path, "from", f.PlacingFrom)
	}
	return nil
}

// sameContent reports whether two files hold the same bytes, reading both
// in chunks.
func sameContent(a, b string) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()
	ia, err := fa.Stat()
	if err != nil {
		return false, err
	}
	ib, err := fb.Stat()
	if err != nil {
		return false, err
	}
	if ia.Size() != ib.Size() {
		return false, nil
	}
	const chunk = 64 << 10
	bufA, bufB := make([]byte, chunk), make([]byte, chunk)
	for {
		na, errA := io.ReadFull(fa, bufA)
		nb, errB := io.ReadFull(fb, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		endA := errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF)
		endB := errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF)
		if errA != nil && !endA {
			return false, errA
		}
		if errB != nil && !endB {
			return false, errB
		}
		if endA || endB {
			return endA && endB, nil
		}
	}
}

// findTemps lists the temporary files of the organizer and the tag writer
// under root.
func findTemps(ctx context.Context, root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if path == root {
				return err
			}
			return nil // an unreadable folder is skipped
		}
		if d.IsDir() {
			return nil
		}
		for _, p := range tempPrefixes {
			if strings.HasPrefix(d.Name(), p) {
				found = append(found, path)
				break
			}
		}
		return nil
	})
	return found, err
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
