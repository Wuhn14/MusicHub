// Package catalog serves artists, albums and search results from the local
// database, going to MusicBrainz only when an entry is missing or too old.
package catalog

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/store"
)

const (
	// cacheTTL is how long a stored entry is considered fresh.
	cacheTTL = 7 * 24 * time.Hour

	searchArtistLimit = 10
	searchAlbumLimit  = 20
	searchTrackLimit  = 20
)

// SearchResult groups the hits of one search.
type SearchResult struct {
	Artists []store.Artist
	Albums  []store.Album
	Tracks  []musicbrainz.TrackHit
}

// remote is the part of the MusicBrainz client the catalog uses.
type remote interface {
	SearchArtists(ctx context.Context, q string, limit int) ([]store.Artist, error)
	SearchAlbums(ctx context.Context, q string, limit int) ([]store.Album, error)
	SearchTracks(ctx context.Context, q string, limit int) ([]musicbrainz.TrackHit, error)
	GetArtist(ctx context.Context, id string) (store.Artist, []store.Album, error)
	GetAlbum(ctx context.Context, id string) (store.Album, error)
}

// local is the part of the store the catalog uses.
type local interface {
	GetArtist(ctx context.Context, id string) (store.Artist, []store.Album, error)
	UpsertArtist(ctx context.Context, a store.Artist, albums []store.Album) error
	GetAlbum(ctx context.Context, id string) (store.Album, error)
	UpsertAlbum(ctx context.Context, al store.Album) error
}

// Catalog is a read-through cache over MusicBrainz.
type Catalog struct {
	db  local
	mb  remote
	now func() time.Time
}

// New returns a catalog backed by st and mb. now is the clock used to date
// cache entries (time.Now in production).
func New(st *store.Store, mb *musicbrainz.Client, now func() time.Time) *Catalog {
	return &Catalog{db: st, mb: mb, now: now}
}

func (c *Catalog) fresh(fetchedAt time.Time) bool {
	return c.now().Sub(fetchedAt) < cacheTTL
}

// Search looks the text up on MusicBrainz. Results are not cached. The three
// searches run one after the other because of the rate limit.
func (c *Catalog) Search(ctx context.Context, q string) (SearchResult, error) {
	var res SearchResult
	q = strings.TrimSpace(q)
	if q == "" {
		return res, nil
	}
	var err error
	if res.Artists, err = c.mb.SearchArtists(ctx, q, searchArtistLimit); err != nil {
		return SearchResult{}, err
	}
	if res.Albums, err = c.mb.SearchAlbums(ctx, q, searchAlbumLimit); err != nil {
		return SearchResult{}, err
	}
	if res.Tracks, err = c.mb.SearchTracks(ctx, q, searchTrackLimit); err != nil {
		return SearchResult{}, err
	}
	return res, nil
}

// Artist returns an artist and its albums (without tracks).
func (c *Catalog) Artist(ctx context.Context, id string) (store.Artist, []store.Album, error) {
	a, albums, err := c.db.GetArtist(ctx, id)
	stored := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Artist{}, nil, err
	}
	if stored && c.fresh(a.FetchedAt) {
		return a, albums, nil
	}

	fa, falbums, err := c.mb.GetArtist(ctx, id)
	if err != nil {
		if stored && !errors.Is(err, musicbrainz.ErrNotFound) {
			return a, albums, nil // stale but better than nothing
		}
		return store.Artist{}, nil, err
	}
	now := c.now()
	fa.FetchedAt = now
	for i := range falbums {
		falbums[i].FetchedAt = now
	}
	if err := c.db.UpsertArtist(ctx, fa, falbums); err != nil {
		return store.Artist{}, nil, err
	}
	return c.db.GetArtist(ctx, id)
}

// Album returns an album with its tracks. A stored album without tracks is a
// stub left by Artist and is fetched in full.
func (c *Catalog) Album(ctx context.Context, id string) (store.Album, error) {
	al, err := c.db.GetAlbum(ctx, id)
	stored := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Album{}, err
	}
	if stored && len(al.Tracks) > 0 && c.fresh(al.FetchedAt) {
		return al, nil
	}

	fa, err := c.mb.GetAlbum(ctx, id)
	if err != nil {
		if stored && !errors.Is(err, musicbrainz.ErrNotFound) {
			return al, nil // stale (or stub) but better than nothing
		}
		return store.Album{}, err
	}
	fa.FetchedAt = c.now()
	if err := c.db.UpsertAlbum(ctx, fa); err != nil {
		return store.Album{}, err
	}
	return c.db.GetAlbum(ctx, id)
}
