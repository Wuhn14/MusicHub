package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// missingTTL is how long a "no cover" answer from CAA is remembered.
	missingTTL = 24 * time.Hour
	// maxCoverBytes caps the size of a downloaded image.
	maxCoverBytes = 10 << 20

	coverUserAgent = "MusicHub ( https://github.com/Wuhn14/MusicHub )"
)

// ErrNoCover means Cover Art Archive has no front cover for the album.
var ErrNoCover = errors.New("no cover available")

var mbidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Covers is an on-disk cache of album covers fetched from Cover Art Archive.
type Covers struct {
	baseURL string
	dir     string
	now     func() time.Time
	client  *http.Client

	mu     sync.Mutex
	flight map[string]*coverCall
}

// coverCall is one download in progress, shared by concurrent callers.
type coverCall struct {
	done chan struct{}
	path string
	err  error
}

// NewCovers returns a cache storing files in dir, fetching from the Cover Art
// Archive at baseURL. now dates the "no cover" markers (time.Now in production).
func NewCovers(baseURL, dir string, now func() time.Time) *Covers {
	return &Covers{
		baseURL: strings.TrimRight(baseURL, "/"),
		dir:     dir,
		now:     now,
		client:  &http.Client{Timeout: 30 * time.Second},
		flight:  make(map[string]*coverCall),
	}
}

// Get returns the path of the cover of the album (a MusicBrainz release group
// id) at the given size, downloading it if needed. size must be 250 or 500;
// anything else is treated as 250. It returns ErrNoCover when there is none.
func (c *Covers) Get(ctx context.Context, albumID string, size int) (string, error) {
	if !mbidPattern.MatchString(albumID) {
		return "", fmt.Errorf("invalid album id %q", albumID)
	}
	if size != 250 && size != 500 {
		size = 250
	}
	name := fmt.Sprintf("%s-%d", albumID, size)
	file := filepath.Join(c.dir, name+".jpg")
	marker := filepath.Join(c.dir, name+".missing")

	if _, err := os.Stat(file); err == nil {
		return file, nil
	}
	if c.missingRecently(marker) {
		return "", ErrNoCover
	}

	c.mu.Lock()
	call, ok := c.flight[name]
	if !ok {
		call = &coverCall{done: make(chan struct{})}
		c.flight[name] = call
		// The download is shared, so it must not depend on the context of
		// whichever caller happened to start it (its client may disconnect).
		// The HTTP client timeout still bounds it.
		go func() {
			call.path, call.err = c.fetch(context.WithoutCancel(ctx), albumID, size, file, marker)
			c.mu.Lock()
			delete(c.flight, name)
			c.mu.Unlock()
			close(call.done)
		}()
	}
	c.mu.Unlock()

	select {
	case <-call.done:
		return call.path, call.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *Covers) missingRecently(marker string) bool {
	info, err := os.Stat(marker)
	if err != nil {
		return false
	}
	return c.now().Sub(info.ModTime()) < missingTTL
}

func (c *Covers) fetch(ctx context.Context, albumID string, size int, file, marker string) (string, error) {
	url := fmt.Sprintf("%s/release-group/%s/front-%d", c.baseURL, albumID, size)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", coverUserAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching cover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		if err := c.writeMarker(marker); err != nil {
			return "", err
		}
		return "", ErrNoCover
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching cover: unexpected status %s", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return "", fmt.Errorf("fetching cover: unexpected content type %q", ct)
	}

	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(c.dir, ".cover-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxCoverBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading cover: %w", err)
	}
	if n > maxCoverBytes {
		return "", errors.New("cover is too large")
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, file); err != nil {
		return "", err
	}
	ok = true
	return file, nil
}

// writeMarker records that CAA has no cover, dated with the cache clock.
func (c *Covers) writeMarker(marker string) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return err
	}
	t := c.now()
	return os.Chtimes(marker, t, t)
}
