package acoustid

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/acoustid/acoustidtest"
)

func newTestClient(t *testing.T, minInterval time.Duration) (*Client, *acoustidtest.Server) {
	t.Helper()
	srv := acoustidtest.New()
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/v2", minInterval)
	c.retryBase = 10 * time.Millisecond
	return c, srv
}

func TestLookupParsesMatches(t *testing.T) {
	c, srv := newTestClient(t, 0)
	got, err := c.Lookup(context.Background(), "test-key", acoustidtest.FingerprintGlassTide, 201)
	if err != nil {
		t.Fatal(err)
	}
	req := srv.Last()
	if req.Path != "/v2/lookup" {
		t.Errorf("path = %q", req.Path)
	}
	for k, want := range map[string]string{
		"client": "test-key", "duration": "201", "fingerprint": acoustidtest.FingerprintGlassTide,
		"meta": "recordings releasegroups compress", "format": "json",
	} {
		if req.Form.Get(k) != want {
			t.Errorf("form %s = %q, want %q", k, req.Form.Get(k), want)
		}
	}
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2: %+v", len(got), got)
	}
	m := got[0] // sorted by score, best first
	if m.Score != 0.97 || m.RecordingID != acoustidtest.RecordingGlassTide || m.Title != "Glass Tide" || m.DurationSec != 201 ||
		len(m.ReleaseGroups) != 1 {
		t.Fatalf("best match = %+v", m)
	}
	rg := m.ReleaseGroups[0]
	if rg.ID != acoustidtest.ReleaseGroupLanterns || rg.Title != "Paper Lanterns" || rg.Type != "Album" ||
		len(rg.SecondaryTypes) != 1 || rg.SecondaryTypes[0] != "Compilation" {
		t.Errorf("release group = %+v", rg)
	}
	if got[1].Score != 0.55 {
		t.Errorf("second score = %v", got[1].Score)
	}

	none, err := c.Lookup(context.Background(), "test-key", acoustidtest.FingerprintNoRecording, 10)
	if err != nil || len(none) != 0 {
		t.Errorf("result without recording: %+v, %v", none, err)
	}
	none, err = c.Lookup(context.Background(), "test-key", "unknown", 10)
	if err != nil || len(none) != 0 {
		t.Errorf("unknown fingerprint: %+v, %v", none, err)
	}
}

func TestLookupBadKey(t *testing.T) {
	c, srv := newTestClient(t, 0)
	for _, key := range []string{acoustidtest.BadKey, acoustidtest.BadKey200} {
		before := srv.Requests()
		_, err := c.Lookup(context.Background(), key, "fp", 10)
		if !errors.Is(err, ErrBadKey) {
			t.Fatalf("key %s: err = %v, want ErrBadKey", key, err)
		}
		if srv.Requests() != before+1 {
			t.Errorf("a bad key must not be retried: %d requests", srv.Requests()-before)
		}
	}
}

func TestLookupUnavailable(t *testing.T) {
	c, srv := newTestClient(t, 0)
	srv.FailNext(2)
	if _, err := c.Lookup(context.Background(), "k", "fp", 10); err != nil {
		t.Fatalf("retries should recover: %v", err)
	}
	if srv.Requests() != 3 {
		t.Errorf("requests = %d, want 3", srv.Requests())
	}
	srv.FailNext(100)
	if _, err := c.Lookup(context.Background(), "k", "fp", 10); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestLookupRateLimited(t *testing.T) {
	const interval = 333 * time.Millisecond
	c, _ := newTestClient(t, interval)
	sends := recordSends(c)
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.Lookup(context.Background(), "k", "fp", 10); err != nil {
			t.Fatal(err)
		}
	}
	times := sends.all()
	if len(times) != 3 {
		t.Fatalf("got %d requests", len(times))
	}
	checkSpacing(t, start, times, interval)
}

func TestLookupKeyNotLogged(t *testing.T) {
	const key = "SECRET-KEY-123"
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	c, srv := newTestClient(t, 0)
	var errs []string

	// Service trouble: the secret key is used on a call that fails.
	srv.FailNext(100)
	_, err := c.Lookup(context.Background(), key, "fp", 10)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	errs = append(errs, err.Error())
	// The key travels in the body, never in the URL.
	if strings.Contains(srv.Last().Path, key) {
		t.Error("key in URL path")
	}
	srv.FailNext(0)

	// A refused key is reported without the key.
	_, err = c.Lookup(context.Background(), acoustidtest.BadKey, "fp", 10)
	errs = append(errs, err.Error())

	// An unreachable server: the transport error must not leak the key.
	srv.Close()
	_, err = c.Lookup(context.Background(), key, "fp", 10)
	if err == nil {
		t.Fatal("expected an error")
	}
	errs = append(errs, err.Error())

	for _, s := range append(errs, logs.String()) {
		if strings.Contains(s, key) {
			t.Errorf("key leaked in %q", s)
		}
	}
}
