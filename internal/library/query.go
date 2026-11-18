package library

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Wuhn14/MusicHub/internal/store"
	"github.com/Wuhn14/MusicHub/internal/textfold"
)

// ErrNotFound is returned when an album key matches nothing in the library.
var ErrNotFound = store.ErrNotFound

// Display fallbacks for files without usable tags, and album key parts.
const (
	noAlbumTitle    = "Titres sans album"
	unknownArtist   = "Artiste inconnu"
	keyPrefixMB     = "mb:"
	keyPrefixTag    = "tag:"
	keyTagSeparator = "\x00"
)

// Sort orders accepted by Albums and Tracks.
const (
	SortAdded = "added"
	SortName  = "name"
	SortYear  = "year"
)

// LibAlbum is an album of the library: a group of playable files.
type LibAlbum struct {
	Key        string
	Title      string
	Artist     string
	Year       string
	CoverURL   string
	AlbumID    string // MusicBrainz release group, empty when not identified
	TrackCount int
	DurationMs int
	Local      bool // at least one track comes from a local identification
	AddedAt    time.Time
}

// LibArtist is an album artist of the library.
type LibArtist struct {
	Name       string
	AlbumCount int
	TrackCount int
}

// LibTrack is one playable file as listed in the library.
type LibTrack struct {
	FileID, Title, Artist, Album, AlbumKey string
	Disc, Position, DurationMs             int
	RecordingID                            string
	Local                                  bool
	Status                                 string
	CoverURL                               string
	Year                                   string
	AddedAt                                time.Time
}

// Query reads the library: the scanned files grouped into albums.
type Query struct {
	st *store.Store
}

// NewQuery returns a Query over the given store.
func NewQuery(st *store.Store) *Query { return &Query{st: st} }

// encodeKey and decodeKey implement the opaque, URL-safe album key.
func encodeKey(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }

func decodeKey(key string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		return "", fmt.Errorf("%w: invalid album key", ErrNotFound)
	}
	s := string(b)
	if !strings.HasPrefix(s, keyPrefixMB) && !strings.HasPrefix(s, keyPrefixTag) {
		return "", fmt.Errorf("%w: invalid album key", ErrNotFound)
	}
	return s, nil
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// fold lower-cases s, trims it and strips accents, so searches ignore both.
func fold(s string) string { return textfold.Fold(s) }

func yearFromDate(d string) string {
	d = strings.TrimSpace(d)
	if len(d) >= 4 && strings.Trim(d[:4], "0123456789") == "" {
		return d[:4]
	}
	return ""
}

func playable(f store.File) bool {
	switch f.Status {
	case store.FilePending, store.FileIdentified, store.FileLocal:
		return true
	}
	return false
}

type group struct {
	album  LibAlbum
	tracks []LibTrack
	years  []string // distinct non-empty tag years, in first-seen order
	counts map[string]int
}

// load groups every playable file into albums, keyed by album key.
func (q *Query) load(ctx context.Context) (map[string]*group, error) {
	files, err := q.st.ListFiles(ctx, store.FileFilter{})
	if err != nil {
		return nil, err
	}
	groups := map[string]*group{}
	catalogAlbums := map[string]*store.Album{}
	for _, f := range files {
		if !playable(f) {
			continue
		}
		// A freshly uploaded file waits in "À identifier" until it is
		// identified; only then does it join the library.
		if f.Origin == store.OriginUploaded && f.Status == store.FilePending {
			continue
		}
		var (
			key, title, artist, year, cover, albumID string
			cat                                      *store.Album
		)
		if f.Status == store.FileIdentified && f.AlbumID != "" {
			albumID = f.AlbumID
			c, seen := catalogAlbums[albumID]
			if !seen {
				al, err := q.st.GetAlbum(ctx, albumID)
				switch {
				case err == nil:
					c = &al
				case !errors.Is(err, store.ErrNotFound):
					slog.Warn("album du catalogue illisible, retour aux tags", "album", albumID, "err", err)
				}
				catalogAlbums[albumID] = c
			}
			cat = c
		}
		if cat != nil {
			key = encodeKey(keyPrefixMB + albumID)
			title, artist = cat.Title, cat.ArtistName
			year = yearFromDate(cat.FirstReleaseDate)
			cover = "/api/covers/" + albumID
		} else {
			albumID = ""
			albumArtist := strings.TrimSpace(f.Tags.AlbumArtist)
			if albumArtist == "" {
				albumArtist = strings.TrimSpace(f.Tags.Artist)
			}
			album := strings.TrimSpace(f.Tags.Album)
			key = encodeKey(keyPrefixTag + norm(albumArtist) + keyTagSeparator + norm(album))
			title, artist, year = album, albumArtist, yearFromDate(f.Tags.Date)
		}
		g := groups[key]
		if g == nil {
			if title == "" {
				title = noAlbumTitle
			}
			if artist == "" {
				artist = unknownArtist
			}
			g = &group{album: LibAlbum{Key: key, Title: title, Artist: artist, Year: year, CoverURL: cover, AlbumID: albumID}, counts: map[string]int{}}
			groups[key] = g
		}
		a := &g.album
		if cat == nil {
			// Tagged albums: fill gaps from later files, and vote on the year.
			if a.Title == noAlbumTitle && title != "" {
				a.Title = title
			}
			if a.Artist == unknownArtist && artist != "" {
				a.Artist = artist
			}
			if year != "" {
				if g.counts[year] == 0 {
					g.years = append(g.years, year)
				}
				g.counts[year]++
			}
		}
		a.TrackCount++
		a.DurationMs += f.DurationMs
		a.Local = a.Local || f.Status == store.FileLocal
		if f.AddedAt.After(a.AddedAt) {
			a.AddedAt = f.AddedAt
		}
		g.tracks = append(g.tracks, trackOf(f, g.album, cat))
	}
	for _, g := range groups {
		// Most common tag year; ties go to the earliest.
		if g.album.AlbumID == "" {
			best := ""
			for _, y := range g.years {
				if best == "" || g.counts[y] > g.counts[best] || (g.counts[y] == g.counts[best] && y < best) {
					best = y
				}
			}
			g.album.Year = best
		}
		for i := range g.tracks {
			g.tracks[i].Album, g.tracks[i].Year = g.album.Title, g.album.Year
		}
	}
	return groups, nil
}

// trackOf builds the list entry of a file. Tags give the title and artist
// (a local track will override them once local identification exists); a
// missing title falls back to the catalog track, then to the file name.
func trackOf(f store.File, al LibAlbum, cat *store.Album) LibTrack {
	t := LibTrack{
		FileID: f.ID, Title: strings.TrimSpace(f.Tags.Title), Album: al.Title, AlbumKey: al.Key,
		Disc: f.Disc, Position: f.Position, DurationMs: f.DurationMs, RecordingID: f.RecordingID,
		Local: f.Status == store.FileLocal, Status: f.Status, CoverURL: al.CoverURL,
		Year: al.Year, AddedAt: f.AddedAt,
	}
	if t.Disc == 0 {
		t.Disc = f.Tags.Disc
	}
	if t.Position == 0 {
		t.Position = f.Tags.Track
	}
	if t.Title == "" && cat != nil && f.RecordingID != "" {
		for _, ct := range cat.Tracks {
			if ct.RecordingID == f.RecordingID {
				t.Title = ct.Title
				break
			}
		}
	}
	if t.Title == "" {
		base := filepath.Base(f.Path)
		t.Title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	t.Artist = strings.TrimSpace(f.Tags.Artist)
	if t.Artist == "" {
		t.Artist = strings.TrimSpace(f.Tags.AlbumArtist)
	}
	if t.Artist == "" {
		t.Artist = al.Artist
	}
	return t
}

func yearDesc(a, b string) bool {
	if (a == "") != (b == "") {
		return b == "" // unknown years go last
	}
	return a > b
}

func sortAlbums(albums []LibAlbum, by string) {
	var less func(a, b LibAlbum) bool
	switch by {
	case SortName:
		less = func(a, b LibAlbum) bool { return norm(a.Title) < norm(b.Title) }
	case SortYear:
		less = func(a, b LibAlbum) bool { return yearDesc(a.Year, b.Year) }
	default:
		less = func(a, b LibAlbum) bool { return a.AddedAt.After(b.AddedAt) }
	}
	sort.Slice(albums, func(i, j int) bool {
		a, b := albums[i], albums[j]
		if less(a, b) {
			return true
		}
		if less(b, a) {
			return false
		}
		if na, nb := norm(a.Title), norm(b.Title); na != nb {
			return na < nb
		}
		return a.Key < b.Key
	})
}

// Albums lists the albums, sorted by "added" (newest first, the default),
// "name" or "year" (newest first, unknown years last).
func (q *Query) Albums(ctx context.Context, sortBy string) ([]LibAlbum, error) {
	groups, err := q.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LibAlbum, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.album)
	}
	sortAlbums(out, sortBy)
	return out, nil
}

// Artists lists one entry per album artist, sorted by name.
func (q *Query) Artists(ctx context.Context) ([]LibArtist, error) {
	groups, err := q.load(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]*LibArtist{}
	for _, g := range groups {
		k := norm(g.album.Artist)
		a := byName[k]
		if a == nil {
			a = &LibArtist{Name: g.album.Artist}
			byName[k] = a
		}
		a.AlbumCount++
		a.TrackCount += g.album.TrackCount
	}
	out := make([]LibArtist, 0, len(byName))
	for _, a := range byName {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if ni, nj := norm(out[i].Name), norm(out[j].Name); ni != nj {
			return ni < nj
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Tracks lists every playable file, optionally keeping those whose title,
// artist or album contains query (case-insensitive).
func (q *Query) Tracks(ctx context.Context, sortBy, query string) ([]LibTrack, error) {
	groups, err := q.load(ctx)
	if err != nil {
		return nil, err
	}
	needle := fold(query)
	out := []LibTrack{}
	for _, g := range groups {
		for _, t := range g.tracks {
			if needle == "" || strings.Contains(fold(t.Title), needle) ||
				strings.Contains(fold(t.Artist), needle) || strings.Contains(fold(t.Album), needle) {
				out = append(out, t)
			}
		}
	}
	var less func(a, b LibTrack) bool
	switch sortBy {
	case SortName:
		less = func(a, b LibTrack) bool { return norm(a.Title) < norm(b.Title) }
	case SortYear:
		less = func(a, b LibTrack) bool { return yearDesc(a.Year, b.Year) }
	default:
		less = func(a, b LibTrack) bool { return a.AddedAt.After(b.AddedAt) }
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if less(a, b) {
			return true
		}
		if less(b, a) {
			return false
		}
		if a.AlbumKey != b.AlbumKey {
			return a.AlbumKey < b.AlbumKey
		}
		return trackBefore(a, b)
	})
	return out, nil
}

func trackBefore(a, b LibTrack) bool {
	if a.Disc != b.Disc {
		return a.Disc < b.Disc
	}
	if a.Position != b.Position {
		return a.Position < b.Position
	}
	if na, nb := norm(a.Title), norm(b.Title); na != nb {
		return na < nb
	}
	return a.FileID < b.FileID
}

// Album returns the album with the given key and its tracks in disc and
// track order. An invalid or unknown key yields ErrNotFound.
func (q *Query) Album(ctx context.Context, key string) (LibAlbum, []LibTrack, error) {
	if _, err := decodeKey(key); err != nil {
		return LibAlbum{}, nil, err
	}
	groups, err := q.load(ctx)
	if err != nil {
		return LibAlbum{}, nil, err
	}
	g := groups[key]
	if g == nil {
		return LibAlbum{}, nil, ErrNotFound
	}
	tracks := append([]LibTrack(nil), g.tracks...)
	sort.Slice(tracks, func(i, j int) bool { return trackBefore(tracks[i], tracks[j]) })
	return g.album, tracks, nil
}

// Service combines the query side and the scan runner; it is what the API
// consumes.
type Service struct {
	*Query
	*Runner
}
