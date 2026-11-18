package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	coverID    = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	missingID  = "11111111-2222-4333-8444-555555555555"
	fakeImage  = "\xff\xd8\xff\xe0fake-jpeg-bytes"
	userAgentW = "MusicHub ( https://github.com/Wuhn14/MusicHub )"
)

// fakeCAA mimics Cover Art Archive: front-<size> answers with a 307 to an
// image URL on the same server; unknown release groups answer 404.
type fakeCAA struct {
	*httptest.Server
	redirects atomic.Int32
	images    atomic.Int32
	mu        sync.Mutex
	agents    []string
	status    int // when non-zero, every redirect request answers it
	delay     time.Duration
	gate      chan struct{} // when set, redirects block until it is closed
	imgType   string        // Content-Type of the image, default image/jpeg
	imgSize   int           // when set, the image body has this many bytes
}

func newFakeCAA(t *testing.T) *fakeCAA {
	t.Helper()
	f := &fakeCAA{}
	mux := http.NewServeMux()
	mux.HandleFunc("/release-group/", func(w http.ResponseWriter, r *http.Request) {
		f.redirects.Add(1)
		f.mu.Lock()
		f.agents = append(f.agents, r.UserAgent())
		status := f.status
		f.mu.Unlock()
		time.Sleep(f.delay)
		if f.gate != nil {
			<-f.gate
		}
		if status != 0 {
			http.Error(w, "boom", status)
			return
		}
		if !strings.Contains(r.URL.Path, "/"+coverID+"/") {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/image/cover.jpg", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/image/", func(w http.ResponseWriter, r *http.Request) {
		f.images.Add(1)
		ct := f.imgType
		if ct == "" {
			ct = "image/jpeg"
		}
		w.Header().Set("Content-Type", ct)
		if f.imgSize > 0 {
			_, _ = w.Write(make([]byte, f.imgSize))
			return
		}
		_, _ = w.Write([]byte(fakeImage))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func TestDownloadsOnceThenServesFromDisk(t *testing.T) {
	caa := newFakeCAA(t)
	dir := filepath.Join(t.TempDir(), "covers")
	c := NewCovers(caa.URL, dir, time.Now)

	p1, err := c.Get(context.Background(), coverID, 500)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, coverID+"-500.jpg"); p1 != want {
		t.Errorf("path = %q, want %q", p1, want)
	}
	b, err := os.ReadFile(p1)
	if err != nil || string(b) != fakeImage {
		t.Fatalf("file content = %q, err %v", b, err)
	}
	p2, err := c.Get(context.Background(), coverID, 500)
	if err != nil || p2 != p1 {
		t.Fatalf("second Get = %q, %v", p2, err)
	}
	if n := caa.images.Load(); n != 1 {
		t.Errorf("image downloads = %d, want 1", n)
	}
	if n := caa.redirects.Load(); n != 1 {
		t.Errorf("CAA requests = %d, want 1", n)
	}
	if caa.agents[0] != userAgentW {
		t.Errorf("User-Agent = %q", caa.agents[0])
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1", len(entries))
	}
}

func TestUnknownSizeFallsBackTo250(t *testing.T) {
	caa := newFakeCAA(t)
	c := NewCovers(caa.URL, t.TempDir(), time.Now)
	p, err := c.Get(context.Background(), coverID, 123)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, coverID+"-250.jpg") {
		t.Errorf("path = %q", p)
	}
}

func TestRemembersMissingCover(t *testing.T) {
	caa := newFakeCAA(t)
	dir := t.TempDir()
	clock := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	c := NewCovers(caa.URL, dir, now)

	if _, err := c.Get(context.Background(), missingID, 250); !errors.Is(err, ErrNoCover) {
		t.Fatalf("err = %v, want ErrNoCover", err)
	}
	// A new instance (restart) still remembers, thanks to the marker file.
	c = NewCovers(caa.URL, dir, now)
	clock = clock.Add(23 * time.Hour)
	if _, err := c.Get(context.Background(), missingID, 250); !errors.Is(err, ErrNoCover) {
		t.Fatalf("err = %v, want ErrNoCover", err)
	}
	if n := caa.redirects.Load(); n != 1 {
		t.Fatalf("CAA requests = %d, want 1 within a day", n)
	}
	clock = clock.Add(2 * time.Hour)
	if _, err := c.Get(context.Background(), missingID, 250); !errors.Is(err, ErrNoCover) {
		t.Fatalf("err = %v, want ErrNoCover", err)
	}
	if n := caa.redirects.Load(); n != 2 {
		t.Errorf("CAA requests = %d, want 2 after a day", n)
	}
}

func TestConcurrentRequestsDownloadOnce(t *testing.T) {
	caa := newFakeCAA(t)
	caa.delay = 50 * time.Millisecond
	c := NewCovers(caa.URL, t.TempDir(), time.Now)

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Get(context.Background(), coverID, 250)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if n := caa.images.Load(); n != 1 {
		t.Errorf("image downloads = %d, want 1", n)
	}
}

func TestRejectsInvalidAlbumID(t *testing.T) {
	caa := newFakeCAA(t)
	dir := filepath.Join(t.TempDir(), "covers")
	c := NewCovers(caa.URL, dir, time.Now)
	for _, id := range []string{"../../etc/passwd", strings.ToUpper(coverID), "", coverID + "/x"} {
		_, err := c.Get(context.Background(), id, 250)
		if err == nil || errors.Is(err, ErrNoCover) {
			t.Errorf("id %q: err = %v, want a validation error", id, err)
		}
	}
	if n := caa.redirects.Load(); n != 0 {
		t.Errorf("CAA requests = %d, want 0", n)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("dir was created for an invalid id")
	}
}

func TestTransientErrorWritesNoMarker(t *testing.T) {
	caa := newFakeCAA(t)
	caa.status = http.StatusServiceUnavailable
	dir := t.TempDir()
	c := NewCovers(caa.URL, dir, time.Now)
	_, err := c.Get(context.Background(), coverID, 250)
	if err == nil || errors.Is(err, ErrNoCover) {
		t.Fatalf("err = %v, want a transient error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("dir has %d entries, want 0", len(entries))
	}
}

func TestLeaderCancelDoesNotFailWaiters(t *testing.T) {
	caa := newFakeCAA(t)
	caa.gate = make(chan struct{})
	c := NewCovers(caa.URL, t.TempDir(), time.Now)

	leaderCtx, cancel := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := c.Get(leaderCtx, coverID, 250)
		leaderErr <- err
	}()
	for caa.redirects.Load() == 0 { // the leader's request reached CAA
		time.Sleep(time.Millisecond)
	}
	type res struct {
		path string
		err  error
	}
	waiter := make(chan res, 1)
	go func() {
		p, err := c.Get(context.Background(), coverID, 250)
		waiter <- res{p, err}
	}()
	time.Sleep(20 * time.Millisecond) // let the waiter join the flight
	cancel()
	if err := <-leaderErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader err = %v, want context.Canceled", err)
	}
	close(caa.gate)
	r := <-waiter
	if r.err != nil {
		t.Fatalf("waiter err = %v", r.err)
	}
	if b, err := os.ReadFile(r.path); err != nil || string(b) != fakeImage {
		t.Errorf("file = %q, %v", b, err)
	}
	if n := caa.images.Load(); n != 1 {
		t.Errorf("image downloads = %d, want 1", n)
	}
}

func TestRejectsOversizeCover(t *testing.T) {
	caa := newFakeCAA(t)
	caa.imgSize = maxCoverBytes + 1
	dir := t.TempDir()
	c := NewCovers(caa.URL, dir, time.Now)
	_, err := c.Get(context.Background(), coverID, 250)
	if err == nil || errors.Is(err, ErrNoCover) {
		t.Fatalf("err = %v, want an error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("dir has %d entries, want 0", len(entries))
	}
}

func TestRejectsNonImageContentType(t *testing.T) {
	caa := newFakeCAA(t)
	caa.imgType = "text/html"
	dir := t.TempDir()
	c := NewCovers(caa.URL, dir, time.Now)
	_, err := c.Get(context.Background(), coverID, 250)
	if err == nil || errors.Is(err, ErrNoCover) {
		t.Fatalf("err = %v, want an error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("dir has %d entries, want 0", len(entries))
	}
}
