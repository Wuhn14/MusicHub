package stream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/media/mediatest"
	"github.com/Wuhn14/MusicHub/internal/store"
)

// mount registers s the way the API does, so path values are populated.
func mount(s *Server) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/stream/{fileId}", s)
	return mux
}

const testID = "0123456789abcdef0123456789abcdef"

type fakeFiles map[string]store.File

func (f fakeFiles) GetFile(_ context.Context, id string) (store.File, error) {
	if v, ok := f[id]; ok {
		return v, nil
	}
	return store.File{}, store.ErrNotFound
}

func newRealServer(t *testing.T, files fakeFiles, max int) *httptest.Server {
	t.Helper()
	mediatest.RequireTool(t, "ffmpeg")
	s := New(files, media.New("ffmpeg", "ffprobe", "fpcalc"), func() int { return max })
	ts := httptest.NewServer(mount(s))
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, url string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func sineFile(t *testing.T, name string, secs int) (store.File, string) {
	t.Helper()
	p := mediatest.Sine(t, t.TempDir(), name, secs, nil)
	return store.File{ID: testID, Path: p, Status: store.FileLocal, DurationMs: secs * 1000, Mtime: time.Now()}, p
}

func TestOriginalSupportsRange(t *testing.T) {
	f, p := sineFile(t, "a.mp3", 3)
	ts := newRealServer(t, fakeFiles{testID: f}, 3)
	raw, _ := os.ReadFile(p)

	resp := get(t, ts.URL+"/api/stream/"+testID+"?quality=original", map[string]string{"Range": "bytes=10-19"})
	if resp.StatusCode != 206 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("content-type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Errorf("cache-control %q", cc)
	}
	if cr := resp.Header.Get("Content-Range"); !strings.HasPrefix(cr, "bytes 10-19/") {
		t.Errorf("content-range %q", cr)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(raw[10:20]) {
		t.Error("range body mismatch")
	}
	// No quality parameter means original; start is ignored.
	resp2 := get(t, ts.URL+"/api/stream/"+testID+"?start=1", nil)
	body2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != 200 || len(body2) != len(raw) {
		t.Errorf("default original: status %d, %d bytes want %d", resp2.StatusCode, len(body2), len(raw))
	}
	req, _ := http.NewRequest("HEAD", ts.URL+"/api/stream/"+testID, nil)
	hr, err := http.DefaultClient.Do(req)
	if err != nil || hr.StatusCode != 200 {
		t.Fatalf("HEAD: %v %v", err, hr)
	}
	hr.Body.Close()
}

func TestTranscodeIsMP3(t *testing.T) {
	f, _ := sineFile(t, "a.flac", 4)
	ts := newRealServer(t, fakeFiles{testID: f}, 3)
	resp := get(t, ts.URL+"/api/stream/"+testID+"?quality=128&start=1.5", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("content-type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("cache-control %q", cc)
	}
	if resp.Header.Get("Content-Length") != "" {
		t.Error("unexpected Content-Length")
	}
	if d := resp.Header.Get("X-Content-Duration"); d != "2.5" {
		t.Errorf("X-Content-Duration %q, want 2.5", d)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) < 3 || !(string(body[:3]) == "ID3" || (body[0] == 0xFF && body[1]&0xE0 == 0xE0)) {
		t.Errorf("not an MP3 stream, starts with % x", body[:min(4, len(body))])
	}
}

func TestTranscodeRejectsBadStart(t *testing.T) {
	f, _ := sineFile(t, "a.flac", 3)
	ts := newRealServer(t, fakeFiles{testID: f}, 3)
	base := ts.URL + "/api/stream/" + testID + "?quality=128&start="
	for q, want := range map[string]int{
		"abc": 400, "-1": 400, "NaN": 400, "Inf": 400, "": 400,
		"3": 416, "99": 416, "1e9": 416,
	} {
		resp := get(t, base+q, nil)
		if resp.StatusCode != want {
			t.Errorf("start=%q: status %d, want %d", q, resp.StatusCode, want)
		}
	}
	if resp := get(t, ts.URL+"/api/stream/"+testID+"?quality=64", nil); resp.StatusCode != 400 {
		t.Errorf("bad quality: %d", resp.StatusCode)
	}
	if resp := get(t, ts.URL+"/api/stream/..%2Fetc", nil); resp.StatusCode != 400 {
		t.Errorf("bad id: %d", resp.StatusCode)
	}
	if resp := get(t, ts.URL+"/api/stream/"+strings.Repeat("a", 32), nil); resp.StatusCode != 404 {
		t.Errorf("unknown id: %d", resp.StatusCode)
	}
}

func TestStatusAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := fakeFiles{
		"00000000000000000000000000000001": {Path: p, Status: store.FileMissing},
		"00000000000000000000000000000002": {Path: p, Status: store.FileUnreadable},
		"00000000000000000000000000000003": {Path: filepath.Join(dir, "gone.mp3"), Status: store.FileLocal},
	}
	ts := httptest.NewServer(mount(New(files, nil, func() int { return 1 })))
	defer ts.Close()
	for id, want := range map[string]int{
		"00000000000000000000000000000001": 404,
		"00000000000000000000000000000002": 422,
		"00000000000000000000000000000003": 404,
	} {
		if resp := get(t, ts.URL+"/api/stream/"+id, nil); resp.StatusCode != want {
			t.Errorf("%s: %d want %d", id, resp.StatusCode, want)
		}
	}
}

// holdTranscoder writes a few bytes then blocks until the request is cancelled.
type holdTranscoder struct{}

func (h *holdTranscoder) Transcode(ctx context.Context, _ string, _, _ int, w io.Writer) error {
	if _, err := w.Write([]byte("ID3")); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

func holdServer(t *testing.T, max int) (*Server, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.flac")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := fakeFiles{testID: {ID: testID, Path: p, Status: store.FileLocal, DurationMs: 600000}}
	s := New(files, &holdTranscoder{}, func() int { return max })
	ts := httptest.NewServer(mount(s))
	t.Cleanup(ts.Close)
	return s, ts.URL + "/api/stream/" + testID + "?quality=128"
}

func open(t *testing.T, url string) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return resp, cancel
}

func TestTranscodeLimit(t *testing.T) {
	s, url := holdServer(t, 3)
	var cancels []context.CancelFunc
	for i := 0; i < 3; i++ {
		resp, cancel := open(t, url)
		if resp.StatusCode != 200 {
			t.Fatalf("stream %d: %d", i, resp.StatusCode)
		}
		cancels = append(cancels, cancel)
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()
	resp := get(t, url, nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 503 || !strings.Contains(string(body), "Trop de conversions en cours, réessaie dans un instant.") {
		t.Fatalf("4th: %d %s", resp.StatusCode, body)
	}
	cancels[0]()
	waitFor(t, func() bool { return s.InFlight() == 2 })
	resp5, cancel5 := open(t, url)
	defer cancel5()
	if resp5.StatusCode != 200 {
		t.Fatalf("after release: %d", resp5.StatusCode)
	}
}

func TestTranscodeStopsWhenClientLeaves(t *testing.T) {
	s, url := holdServer(t, 3)
	resp, cancel := open(t, url)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	waitFor(t, func() bool { return s.InFlight() == 1 })
	cancel()
	waitFor(t, func() bool { return s.InFlight() == 0 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within 3s")
}

func TestStreamsPendingUploadedFileFromIncoming(t *testing.T) {
	f, p := sineFile(t, "up.mp3", 2)
	f.Origin, f.Status = store.OriginUploaded, store.FilePending
	ts := newRealServer(t, fakeFiles{testID: f}, 3)
	raw, _ := os.ReadFile(p)

	resp := get(t, ts.URL+"/api/stream/"+testID+"?quality=original", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if len(got) != len(raw) {
		t.Errorf("got %d bytes, want %d", len(got), len(raw))
	}
}

func TestHeadOfConversionTakesNoSlot(t *testing.T) {
	s, url := holdServer(t, 1)
	head := func() *http.Response {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "HEAD", url+"&start=60", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			// The hold transcoder never returns: a HEAD that started it
			// would time out here.
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	resp := head()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/mpeg" || resp.Header.Get("X-Content-Duration") != "540.0" {
		t.Fatalf("HEAD = %d %v", resp.StatusCode, resp.Header)
	}
	if n := s.InFlight(); n != 0 {
		t.Fatalf("in flight after HEAD = %d", n)
	}
	// With the only slot taken, HEAD still answers.
	get, cancel := open(t, url)
	defer cancel()
	if get.StatusCode != 200 {
		t.Fatalf("GET = %d", get.StatusCode)
	}
	if resp := head(); resp.StatusCode != 200 {
		t.Fatalf("HEAD with no free slot = %d", resp.StatusCode)
	}
}
