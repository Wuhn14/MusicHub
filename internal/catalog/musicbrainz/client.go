// Package musicbrainz is a small client for the MusicBrainz web service
// (JSON, version 2). It spaces requests to respect the public rate limit and
// retries when the service is overloaded.
package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wuhn14/MusicHub/internal/store"
)

var (
	// ErrUnavailable means MusicBrainz is unreachable or still overloaded
	// after the retries.
	ErrUnavailable = errors.New("musicbrainz: service unavailable")
	// ErrNotFound means MusicBrainz answered 404.
	ErrNotFound = errors.New("musicbrainz: not found")
)

const (
	maxRetries = 3
	pageSize   = 100
	// maxArtistPages caps the browsing of an artist's release groups, so an
	// artist with a huge catalogue cannot turn one page view into dozens of
	// rate-limited requests.
	maxArtistPages = 5
	// maxDrain bounds how much of an error body is read (and discarded) so
	// the connection can be reused.
	maxDrain = 1 << 20
)

// TrackHit is a recording found by SearchTracks, with the album it appears on.
type TrackHit struct {
	store.Track
	AlbumID, AlbumTitle, ArtistName string
}

// Client talks to one MusicBrainz server. It is safe for concurrent use; all
// callers share one rate limiter.
type Client struct {
	baseURL     string
	userAgent   string
	minInterval time.Duration
	http        *http.Client
	retryBase   time.Duration // first retry wait, doubled each time (1s in production)

	mu   sync.Mutex
	next time.Time // earliest start of the next request
}

// New returns a client for the web service at baseURL (for example
// https://musicbrainz.org/ws/2). Request starts are at least minInterval
// apart (1s in production).
func New(baseURL, userAgent string, minInterval time.Duration) *Client {
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		userAgent:   userAgent,
		minInterval: minInterval,
		http:        &http.Client{Timeout: 30 * time.Second},
		retryBase:   time.Second,
	}
}

// wait blocks until the caller may send a request, or ctx ends.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	start := c.next
	if start.Before(now) {
		start = now
	}
	c.next = start.Add(c.minInterval)
	c.mu.Unlock()
	return sleep(ctx, time.Until(start))
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// get sends GET path?params&fmt=json and decodes the JSON answer into out.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("fmt", "json")
	u := c.baseURL + path + "?" + params.Encode()

	for attempt := 0; ; attempt++ {
		if err := c.wait(ctx); err != nil {
			return err
		}
		retry, err := c.do(ctx, u, out)
		if err == nil {
			return nil
		}
		if !retry {
			return err
		}
		if attempt >= maxRetries {
			return fmt.Errorf("%w: still overloaded after %d retries", ErrUnavailable, maxRetries)
		}
		if err := sleep(ctx, c.retryBase<<attempt); err != nil {
			return err
		}
	}
}

// do performs one request. The bool tells whether the failure is a server
// error (5xx) or a 429 that should be retried.
func (c *Client) do(ctx context.Context, u string, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		_, _ = io.CopyN(io.Discard, resp.Body, maxDrain)
		return true, ErrUnavailable
	case resp.StatusCode == http.StatusNotFound:
		_, _ = io.CopyN(io.Discard, resp.Body, maxDrain)
		return false, ErrNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		_, _ = io.CopyN(io.Discard, resp.Body, maxDrain)
		return false, fmt.Errorf("musicbrainz: unexpected status %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, fmt.Errorf("musicbrainz: decoding response: %w", err)
	}
	return false, nil
}

// --- JSON shapes ---

type jsonCredit struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
	Artist     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

type jsonArtist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sort-name"`
	Disambiguation string `json:"disambiguation"`
	Country        string `json:"country"`
}

type jsonReleaseGroup struct {
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	PrimaryType      string       `json:"primary-type"`
	SecondaryTypes   []string     `json:"secondary-types"`
	FirstReleaseDate string       `json:"first-release-date"`
	ArtistCredit     []jsonCredit `json:"artist-credit"`
}

func (a jsonArtist) store() store.Artist {
	return store.Artist{ID: a.ID, Name: a.Name, SortName: a.SortName, Disambiguation: a.Disambiguation, Country: a.Country}
}

// firstArtist returns the ID and name of the first credited artist.
func firstArtist(credits []jsonCredit) (id, name string) {
	if len(credits) == 0 {
		return "", ""
	}
	id, name = credits[0].Artist.ID, credits[0].Artist.Name
	if name == "" {
		name = credits[0].Name
	}
	return id, name
}

// joinCredits renders the credited names with their join phrases.
func joinCredits(credits []jsonCredit) string {
	var b strings.Builder
	for _, c := range credits {
		name := c.Name
		if name == "" {
			name = c.Artist.Name
		}
		b.WriteString(name)
		b.WriteString(c.JoinPhrase)
	}
	return b.String()
}

func (g jsonReleaseGroup) store() store.Album {
	id, name := firstArtist(g.ArtistCredit)
	return store.Album{
		ID: g.ID, Title: g.Title, ArtistID: id, ArtistName: name,
		PrimaryType: g.PrimaryType, SecondaryTypes: g.SecondaryTypes, FirstReleaseDate: g.FirstReleaseDate,
	}
}

func searchParams(q string, limit int) url.Values {
	return url.Values{"query": {escapeLucene(q)}, "limit": {strconv.Itoa(limit)}}
}

// --- Search ---

// SearchArtists searches artists by name. The text is matched literally.
func (c *Client) SearchArtists(ctx context.Context, q string, limit int) ([]store.Artist, error) {
	var res struct {
		Artists []jsonArtist `json:"artists"`
	}
	if err := c.get(ctx, "/artist", searchParams(q, limit), &res); err != nil {
		return nil, err
	}
	out := make([]store.Artist, 0, len(res.Artists))
	for _, a := range res.Artists {
		out = append(out, a.store())
	}
	return out, nil
}

// SearchAlbums searches release groups. The text is matched literally.
func (c *Client) SearchAlbums(ctx context.Context, q string, limit int) ([]store.Album, error) {
	var res struct {
		ReleaseGroups []jsonReleaseGroup `json:"release-groups"`
	}
	if err := c.get(ctx, "/release-group", searchParams(q, limit), &res); err != nil {
		return nil, err
	}
	out := make([]store.Album, 0, len(res.ReleaseGroups))
	for _, g := range res.ReleaseGroups {
		out = append(out, g.store())
	}
	return out, nil
}

// SearchTracks searches recordings. The text is matched literally. The album
// of a hit is the release group of its first release, if any.
func (c *Client) SearchTracks(ctx context.Context, q string, limit int) ([]TrackHit, error) {
	var res struct {
		Recordings []struct {
			ID           string       `json:"id"`
			Title        string       `json:"title"`
			Length       int          `json:"length"`
			ArtistCredit []jsonCredit `json:"artist-credit"`
			Releases     []struct {
				ReleaseGroup struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"release-group"`
				// In a recording search the medium lists only the matching
				// track; track-offset is its zero-based index on the medium.
				Media []struct {
					Position    int  `json:"position"`
					TrackOffset *int `json:"track-offset"`
				} `json:"media"`
			} `json:"releases"`
		} `json:"recordings"`
	}
	if err := c.get(ctx, "/recording", searchParams(q, limit), &res); err != nil {
		return nil, err
	}
	out := make([]TrackHit, 0, len(res.Recordings))
	for _, r := range res.Recordings {
		h := TrackHit{
			Track:      store.Track{RecordingID: r.ID, Title: r.Title, LengthMs: r.Length},
			ArtistName: joinCredits(r.ArtistCredit),
		}
		if len(r.Releases) > 0 {
			rel := r.Releases[0]
			h.AlbumID, h.AlbumTitle = rel.ReleaseGroup.ID, rel.ReleaseGroup.Title
			if len(rel.Media) > 0 && rel.Media[0].TrackOffset != nil {
				h.Disc = rel.Media[0].Position
				h.Position = *rel.Media[0].TrackOffset + 1
			}
		}
		out = append(out, h)
	}
	return out, nil
}

// --- Lookups ---

// GetArtist returns an artist and its albums, EPs and singles (without
// tracks), following the pagination of the browse request. At most
// maxArtistPages pages are read; the rest is dropped and the cut is logged.
func (c *Client) GetArtist(ctx context.Context, id string) (store.Artist, []store.Album, error) {
	var ja jsonArtist
	if err := c.get(ctx, "/artist/"+url.PathEscape(id), nil, &ja); err != nil {
		return store.Artist{}, nil, err
	}
	artist := ja.store()

	albums := []store.Album{}
	for offset, pages := 0, 0; ; {
		var page struct {
			Count         int                `json:"release-group-count"`
			ReleaseGroups []jsonReleaseGroup `json:"release-groups"`
		}
		params := url.Values{"artist": {id}, "type": {"album|ep|single"}, "limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}}
		if err := c.get(ctx, "/release-group", params, &page); err != nil {
			return store.Artist{}, nil, err
		}
		for _, g := range page.ReleaseGroups {
			al := g.store()
			al.ArtistID, al.ArtistName = artist.ID, artist.Name
			albums = append(albums, al)
		}
		offset += len(page.ReleaseGroups)
		pages++
		if len(page.ReleaseGroups) == 0 || offset >= page.Count {
			break
		}
		if pages >= maxArtistPages {
			slog.Info("musicbrainz: artist discography truncated",
				"artist", id, "kept", offset, "total", page.Count)
			break
		}
	}
	return artist, albums, nil
}

type jsonRelease struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Date   string `json:"date"`
	Media  []struct {
		Position   int `json:"position"`
		TrackCount int `json:"track-count"`
	} `json:"media"`
}

func (r jsonRelease) trackCount() int {
	n := 0
	for _, m := range r.Media {
		n += m.TrackCount
	}
	return n
}

// pickReference chooses the release whose tracklist represents the album:
// official status, earliest date (undated last), then fewest tracks, then ID
// for determinism. Without any official release, all releases compete.
func pickReference(releases []jsonRelease) (jsonRelease, bool) {
	var cands []jsonRelease
	for _, r := range releases {
		if r.Status == "Official" {
			cands = append(cands, r)
		}
	}
	if len(cands) == 0 {
		cands = releases
	}
	if len(cands) == 0 {
		return jsonRelease{}, false
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.Date != b.Date {
			if a.Date == "" || b.Date == "" {
				return b.Date == ""
			}
			return a.Date < b.Date
		}
		if a.trackCount() != b.trackCount() {
			return a.trackCount() < b.trackCount()
		}
		return a.ID < b.ID
	})
	return cands[0], true
}

// GetAlbum returns a release group with the tracklist of its reference
// release. An album without any release has no tracks.
func (c *Client) GetAlbum(ctx context.Context, id string) (store.Album, error) {
	var g jsonReleaseGroup
	if err := c.get(ctx, "/release-group/"+url.PathEscape(id), url.Values{"inc": {"artist-credits"}}, &g); err != nil {
		return store.Album{}, err
	}
	album := g.store()

	var browse struct {
		Releases []jsonRelease `json:"releases"`
	}
	params := url.Values{"release-group": {id}, "inc": {"media"}, "limit": {strconv.Itoa(pageSize)}}
	if err := c.get(ctx, "/release", params, &browse); err != nil {
		return store.Album{}, err
	}
	ref, ok := pickReference(browse.Releases)
	if !ok {
		return album, nil
	}

	var rel struct {
		Media []struct {
			Position int `json:"position"`
			Tracks   []struct {
				Position  int    `json:"position"`
				Title     string `json:"title"`
				Length    int    `json:"length"`
				Recording struct {
					ID     string `json:"id"`
					Title  string `json:"title"`
					Length int    `json:"length"`
				} `json:"recording"`
			} `json:"tracks"`
		} `json:"media"`
	}
	if err := c.get(ctx, "/release/"+url.PathEscape(ref.ID), url.Values{"inc": {"recordings artist-credits"}}, &rel); err != nil {
		return store.Album{}, err
	}
	for _, m := range rel.Media {
		for _, t := range m.Tracks {
			length := t.Length
			if length == 0 {
				length = t.Recording.Length
			}
			title := t.Title
			if title == "" {
				title = t.Recording.Title
			}
			album.Tracks = append(album.Tracks, store.Track{
				RecordingID: t.Recording.ID, Title: title, Disc: m.Position, Position: t.Position, LengthMs: length,
			})
		}
	}
	return album, nil
}

// --- Recordings ---

// RecordingQuery describes a recording to look for by its tags. Album is
// optional.
type RecordingQuery struct {
	Artist, Title, Album string
}

// ReleaseRef is a release a recording appears on, with its release group.
// Disc and Position are 0 when MusicBrainz does not say.
type ReleaseRef struct {
	ReleaseGroupID, Title, PrimaryType string
	SecondaryTypes                     []string
	Date                               string
	Disc, Position                     int
}

// RecordingHit is a recording with the releases it appears on. Score is the
// MusicBrainz search score (0-100), 0 for a lookup.
type RecordingHit struct {
	RecordingID, Title, ArtistName string
	LengthMs, Score                int
	Releases                       []ReleaseRef
}

type jsonRecording struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Length       int          `json:"length"`
	Score        int          `json:"score"`
	ArtistCredit []jsonCredit `json:"artist-credit"`
	Releases     []struct {
		Title        string `json:"title"`
		Date         string `json:"date"`
		ReleaseGroup struct {
			ID             string   `json:"id"`
			Title          string   `json:"title"`
			PrimaryType    string   `json:"primary-type"`
			SecondaryTypes []string `json:"secondary-types"`
		} `json:"release-group"`
		// A search lists in the medium only the matching track and gives its
		// zero-based track-offset; a lookup with inc=media gives the track
		// itself in "tracks". Both are understood.
		Media []struct {
			Position    int  `json:"position"`
			TrackOffset *int `json:"track-offset"`
			Tracks      []struct {
				Position int `json:"position"`
			} `json:"tracks"`
		} `json:"media"`
	} `json:"releases"`
}

func (r jsonRecording) hit() RecordingHit {
	h := RecordingHit{
		RecordingID: r.ID, Title: r.Title, ArtistName: joinCredits(r.ArtistCredit),
		LengthMs: r.Length, Score: r.Score, Releases: []ReleaseRef{},
	}
	for _, rel := range r.Releases {
		ref := ReleaseRef{
			ReleaseGroupID: rel.ReleaseGroup.ID, Title: rel.ReleaseGroup.Title,
			PrimaryType: rel.ReleaseGroup.PrimaryType, SecondaryTypes: rel.ReleaseGroup.SecondaryTypes,
			Date: rel.Date,
		}
		if ref.Title == "" {
			ref.Title = rel.Title
		}
		if len(rel.Media) > 0 {
			m := rel.Media[0]
			switch {
			case m.TrackOffset != nil:
				ref.Disc, ref.Position = m.Position, *m.TrackOffset+1
			case len(m.Tracks) > 0:
				ref.Disc, ref.Position = m.Position, m.Tracks[0].Position
			}
		}
		h.Releases = append(h.Releases, ref)
	}
	return h
}

// FindRecordings searches recordings by artist, title and optionally album
// (a fielded query; values are matched as phrases), best score first.
func (c *Client) FindRecordings(ctx context.Context, q RecordingQuery, limit int) ([]RecordingHit, error) {
	if strings.TrimSpace(q.Title) == "" || strings.TrimSpace(q.Artist) == "" {
		return []RecordingHit{}, nil
	}
	query := `recording:"` + escapePhrase(q.Title) + `" AND artist:"` + escapePhrase(q.Artist) + `"`
	if q.Album != "" {
		query += ` AND release:"` + escapePhrase(q.Album) + `"`
	}
	var res struct {
		Recordings []jsonRecording `json:"recordings"`
	}
	params := url.Values{"query": {query}, "limit": {strconv.Itoa(limit)}}
	if err := c.get(ctx, "/recording", params, &res); err != nil {
		return nil, err
	}
	out := make([]RecordingHit, 0, len(res.Recordings))
	for _, r := range res.Recordings {
		out = append(out, r.hit())
	}
	return out, nil
}

// GetRecording returns one recording with its releases. The request asks for
// inc=releases+release-groups+artist-credits+media: with media, each release
// carries the medium position and the track of this recording, which gives
// Disc and Position.
func (c *Client) GetRecording(ctx context.Context, id string) (RecordingHit, error) {
	var r jsonRecording
	params := url.Values{"inc": {"releases release-groups artist-credits media"}}
	if err := c.get(ctx, "/recording/"+url.PathEscape(id), params, &r); err != nil {
		return RecordingHit{}, err
	}
	return r.hit(), nil
}
