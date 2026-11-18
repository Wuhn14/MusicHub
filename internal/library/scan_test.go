package library

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/media/mediatest"
	"github.com/Wuhn14/MusicHub/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newClock() *clock { return &clock{t: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)} }

// countingProber counts calls and delegates to a real or fake probe.
type countingProber struct {
	calls atomic.Int32
	fn    func(path string) (media.Probe, error)
}

func (c *countingProber) Probe(_ context.Context, path string) (media.Probe, error) {
	c.calls.Add(1)
	return c.fn(path)
}

func realTools(t *testing.T) *media.Tools {
	t.Helper()
	mediatest.RequireTool(t, "ffmpeg")
	mediatest.RequireTool(t, "ffprobe")
	return media.New("ffmpeg", "ffprobe", "fpcalc")
}

func TestScanAddsFilesWithTags(t *testing.T) {
	dir := t.TempDir()
	tools := realTools(t)
	sub := filepath.Join(dir, "Artist", "Album")
	os.MkdirAll(sub, 0o755)
	mediatest.Sine(t, sub, "01.mp3", 2, map[string]string{"title": "Song One", "artist": "Test Band", "album": "Demo", "track": "1"})
	mediatest.Sine(t, sub, "02.flac", 2, map[string]string{"title": "Song Two", "artist": "Test Band"})

	st := openStore(t)
	clk := newClock()
	sc := NewScanner(st, tools, dir, clk.now)
	var added []store.File
	sc.OnFileAdded = func(f store.File) { added = append(added, f) }
	rep, err := sc.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Seen != 2 || rep.Added != 2 || rep.Unreadable != 0 {
		t.Errorf("report = %+v", rep)
	}
	if len(added) != 2 {
		t.Errorf("OnFileAdded calls = %d, want 2", len(added))
	}
	files, _ := st.ListFiles(context.Background(), store.FileFilter{})
	if len(files) != 2 {
		t.Fatalf("files = %d", len(files))
	}
	f := files[0]
	if f.Status != store.FilePending || f.Tags.Title != "Song One" || f.Tags.Artist != "Test Band" || f.Tags.Track != 1 ||
		f.DurationMs < 1500 || f.Origin != store.OriginDropped || !filepath.IsAbs(f.Path) {
		t.Errorf("file = %+v", f)
	}
}

func TestScanSkipsUnchanged(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.mp3"), []byte("aaa"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.flac"), []byte("bbbb"), 0o644)
	p := &countingProber{fn: func(string) (media.Probe, error) {
		return media.Probe{Format: "mp3", Codec: "mp3", DurationMs: 1000}, nil
	}}
	st := openStore(t)
	clk := newClock()
	sc := NewScanner(st, p, dir, clk.now)
	if _, err := sc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 2 {
		t.Fatalf("first scan probes = %d", p.calls.Load())
	}
	clk.t = clk.t.Add(time.Hour)
	rep, err := sc.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 2 || rep.Seen != 2 || rep.Added != 0 || rep.Updated != 0 {
		t.Errorf("second scan: probes=%d report=%+v", p.calls.Load(), rep)
	}
	// A changed file is probed again and reported as updated.
	os.WriteFile(filepath.Join(dir, "a.mp3"), []byte("aaaaaa"), 0o644)
	rep, _ = sc.Scan(context.Background())
	if p.calls.Load() != 3 || rep.Updated != 1 {
		t.Errorf("third scan: probes=%d report=%+v", p.calls.Load(), rep)
	}
}

func TestScanMarksUnreadableAndContinues(t *testing.T) {
	dir := t.TempDir()
	tools := realTools(t)
	os.WriteFile(filepath.Join(dir, "fake.mp3"), []byte("this is just text, not audio"), 0o644)
	mediatest.Sine(t, dir, "good.mp3", 2, nil)
	flac := mediatest.Sine(t, t.TempDir(), "x.flac", 2, nil)
	// A truncated real file.
	data, _ := os.ReadFile(flac)
	os.WriteFile(filepath.Join(dir, "trunc.flac"), data[:40], 0o644)

	st := openStore(t)
	sc := NewScanner(st, tools, dir, newClock().now)
	rep, err := sc.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Seen != 3 || rep.Unreadable != 2 || rep.Added != 3 {
		t.Errorf("report = %+v", rep)
	}
	bad, _ := st.ListFiles(context.Background(), store.FileFilter{Status: store.FileUnreadable})
	ok, _ := st.ListFiles(context.Background(), store.FileFilter{Status: store.FilePending})
	if len(bad) != 2 || len(ok) != 1 {
		t.Errorf("unreadable=%d pending=%d", len(bad), len(ok))
	}
}

func TestScanMarksMissingThenPurgesAfter30Days(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mp3")
	os.WriteFile(a, []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.mp3"), []byte("b"), 0o644)
	p := &countingProber{fn: func(string) (media.Probe, error) { return media.Probe{Format: "mp3"}, nil }}
	st := openStore(t)
	clk := newClock()
	ctx := context.Background()
	sc := NewScanner(st, p, dir, clk.now)
	sc.Scan(ctx)
	files, _ := st.ListFiles(ctx, store.FileFilter{})
	aID := files[0].ID
	st.SetFileStatus(ctx, aID, store.FileIdentified)

	os.Remove(a)
	clk.t = clk.t.Add(time.Hour)
	rep, _ := sc.Scan(ctx)
	if rep.Missing != 1 {
		t.Fatalf("report = %+v", rep)
	}
	g, _ := st.GetFile(ctx, aID)
	if g.Status != store.FileMissing {
		t.Fatalf("status = %s", g.Status)
	}

	// Comes back: previous status restored.
	os.WriteFile(a, []byte("a"), 0o644)
	clk.t = clk.t.Add(time.Hour)
	sc.Scan(ctx)
	g, _ = st.GetFile(ctx, aID)
	if g.Status != store.FileIdentified || !g.MissingSince.IsZero() {
		t.Fatalf("restored = %+v", g)
	}

	// Gone again: kept 29 days, purged after 30.
	os.Remove(a)
	clk.t = clk.t.Add(time.Hour)
	sc.Scan(ctx)
	clk.t = clk.t.Add(29 * 24 * time.Hour)
	sc.Scan(ctx)
	if _, err := st.GetFile(ctx, aID); err != nil {
		t.Fatalf("purged too early: %v", err)
	}
	clk.t = clk.t.Add(2 * 24 * time.Hour)
	sc.Scan(ctx)
	if _, err := st.GetFile(ctx, aID); err == nil {
		t.Fatal("not purged after 30 days")
	}
}

func TestScanIgnoresHiddenAndNonAudio(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".trash"), 0o755)
	os.MkdirAll(filepath.Join(dir, "Album"), 0o755)
	for _, n := range []string{".hidden.mp3", ".trash/x.mp3", "cover.jpg", "notes.txt", "Album/song.MP3", "Album/song2.opus"} {
		os.WriteFile(filepath.Join(dir, filepath.FromSlash(n)), []byte("x"), 0o644)
	}
	p := &countingProber{fn: func(string) (media.Probe, error) { return media.Probe{Format: "x"}, nil }}
	st := openStore(t)
	rep, err := NewScanner(st, p, dir, newClock().now).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Seen != 2 || p.calls.Load() != 2 {
		t.Errorf("report=%+v probes=%d", rep, p.calls.Load())
	}
}

func TestScanMissingMusicDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	st := openStore(t)
	_, err := NewScanner(st, &countingProber{}, missing, newClock().now).Scan(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Error("scan created the music dir")
	}
}

func TestRunnerSingleFlight(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.mp3"), []byte("a"), 0o644)
	release := make(chan struct{})
	entered := make(chan struct{}, 10)
	var once sync.Once
	p := &countingProber{fn: func(string) (media.Probe, error) {
		entered <- struct{}{}
		<-release
		return media.Probe{Format: "mp3"}, nil
	}}
	st := openStore(t)
	sc := NewScanner(st, p, dir, time.Now)
	r := NewRunner(sc, func() time.Duration { return time.Hour })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	<-entered // first scan (at startup) is now blocked inside Probe
	if !r.Status().Running {
		t.Error("want Running")
	}
	for i := 0; i < 5; i++ {
		r.Trigger() // must neither block nor start a second scan
	}
	time.Sleep(50 * time.Millisecond)
	if n := p.calls.Load(); n != 1 {
		t.Errorf("probes while running = %d, want 1", n)
	}
	once.Do(func() { close(release) })
	deadline := time.Now().Add(5 * time.Second)
	for r.Status().Running || r.Status().LastRun.IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := r.Status(); got.LastReport.Added != 1 || got.LastError != "" {
		t.Errorf("status = %+v", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRunnerRecordsError(t *testing.T) {
	st := openStore(t)
	sc := NewScanner(st, &countingProber{}, filepath.Join(t.TempDir(), "nope"), time.Now)
	r := NewRunner(sc, func() time.Duration { return time.Hour })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for r.Status().LastError == "" {
		if time.Now().After(deadline) {
			t.Fatal("no error recorded")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// walkFailing walks like filepath.WalkDir, except that reading the folder
// bad fails with err, as WalkDir reports a folder it cannot list.
func walkFailing(bad string, err error) func(string, fs.WalkDirFunc) error {
	return func(root string, fn fs.WalkDirFunc) error {
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
			if werr == nil && path == bad && d.IsDir() {
				if r := fn(path, d, nil); r != nil {
					return r
				}
				return fn(path, d, err)
			}
			return fn(path, d, werr)
		})
	}
}

func TestScanUnreadableFolderOnlyShieldsItself(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	os.MkdirAll(locked, 0o755)
	for _, n := range []string{"locked/a.mp3", "b.mp3", "c.mp3", "d.mp3"} {
		os.WriteFile(filepath.Join(dir, filepath.FromSlash(n)), []byte("x"), 0o644)
	}
	d := filepath.Join(dir, "d.mp3")
	var failD atomic.Bool
	p := &countingProber{fn: func(path string) (media.Probe, error) {
		if failD.Load() && path == d {
			return media.Probe{}, errors.New("ffprobe timed out")
		}
		return media.Probe{Format: "mp3"}, nil
	}}
	st := openStore(t)
	clk := newClock()
	ctx := context.Background()
	sc := NewScanner(st, p, dir, clk.now)
	if _, err := sc.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	// Next scan: the folder cannot be listed any more, c.mp3 is gone and
	// d.mp3 changed but cannot be probed right now.
	sc.walk = walkFailing(locked, fs.ErrPermission)
	os.Remove(filepath.Join(dir, "c.mp3"))
	os.WriteFile(d, []byte("xx"), 0o644)
	failD.Store(true)
	clk.t = clk.t.Add(time.Hour)
	rep, err := sc.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped != 1 || rep.Missing != 1 || rep.Errors != 1 {
		t.Fatalf("report = %+v; want 1 skipped, 1 missing, 1 error", rep)
	}
	want := map[string]string{
		"locked/a.mp3": store.FilePending, // behind the unreadable folder
		"b.mp3":        store.FilePending,
		"c.mp3":        store.FileMissing, // really gone
		"d.mp3":        store.FilePending, // probe failed: retried, not missing
	}
	for n, status := range want {
		f, err := st.GetFileByPath(ctx, filepath.Join(dir, filepath.FromSlash(n)))
		if err != nil || f.Status != status {
			t.Errorf("%s = %s, %v; want %s", n, f.Status, err, status)
		}
	}
}

func TestScanPermissionDeniedFileIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	denied := filepath.Join(dir, "denied.mp3")
	os.WriteFile(denied, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "ok.mp3"), []byte("x"), 0o644)
	p := &countingProber{fn: func(string) (media.Probe, error) { return media.Probe{Format: "mp3"}, nil }}
	st := openStore(t)
	clk := newClock()
	ctx := context.Background()
	sc := NewScanner(st, p, dir, clk.now)
	var locked atomic.Bool
	locked.Store(true)
	sc.readable = func(path string) error {
		if locked.Load() && path == denied {
			return &fs.PathError{Op: "open", Path: path, Err: fs.ErrPermission}
		}
		return nil
	}
	rep, err := sc.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Unreadable != 1 || rep.Errors != 0 || p.calls.Load() != 1 {
		t.Fatalf("report = %+v, probes = %d; want the denied file unreadable and not probed", rep, p.calls.Load())
	}
	if f, _ := st.GetFileByPath(ctx, denied); f.Status != store.FileUnreadable {
		t.Fatalf("denied file = %s", f.Status)
	}

	// Unchanged: not checked again. Changed: probed again.
	clk.t = clk.t.Add(time.Hour)
	sc.Scan(ctx)
	if f, _ := st.GetFileByPath(ctx, denied); f.Status != store.FileUnreadable || p.calls.Load() != 1 {
		t.Fatalf("unchanged denied file = %s, probes = %d", f.Status, p.calls.Load())
	}
	locked.Store(false)
	later := time.Now().Add(time.Minute)
	os.Chtimes(denied, later, later)
	clk.t = clk.t.Add(time.Hour)
	sc.Scan(ctx)
	if f, _ := st.GetFileByPath(ctx, denied); f.Status != store.FilePending {
		t.Fatalf("touched file = %s, want pending", f.Status)
	}
}

func TestScanSkipsNASSystemFolders(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"@eaDir/song.mp3/SYNOAUDIO.mp3", "#recycle/old.mp3", "#snapshot/daily/a.mp3",
		".@__thumb/t.mp3", "@Recycle/r.mp3", "lost+found/l.mp3", "Album/@EADIR/x.mp3",
		"Album/song.mp3", "@eaDir.mp3",
	}
	for _, n := range files {
		path := filepath.Join(dir, filepath.FromSlash(n))
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte("x"), 0o644)
	}
	p := &countingProber{fn: func(string) (media.Probe, error) { return media.Probe{Format: "mp3"}, nil }}
	st := openStore(t)
	rep, err := NewScanner(st, p, dir, newClock().now).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Only Album/song.mp3 and the file (not folder) named @eaDir.mp3.
	if rep.Seen != 2 || rep.Skipped != 0 {
		t.Errorf("report = %+v", rep)
	}
}

// TestScanRealUnreadableFolder uses real permissions (not on Windows, and not
// as root, who reads everything).
func TestScanRealUnreadableFolder(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	os.MkdirAll(locked, 0o755)
	os.WriteFile(filepath.Join(locked, "a.mp3"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.mp3"), []byte("x"), 0o644)
	denied := filepath.Join(dir, "denied.mp3")
	os.WriteFile(denied, []byte("x"), 0o644)
	p := &countingProber{fn: func(string) (media.Probe, error) { return media.Probe{Format: "mp3"}, nil }}
	st := openStore(t)
	clk := newClock()
	ctx := context.Background()
	sc := NewScanner(st, p, dir, clk.now)
	sc.Scan(ctx)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	os.Chmod(denied, 0)
	t.Cleanup(func() { os.Chmod(denied, 0o644) })
	later := time.Now().Add(time.Minute)
	os.Chtimes(denied, later, later)
	os.Remove(filepath.Join(dir, "b.mp3"))
	clk.t = clk.t.Add(time.Hour)
	rep, err := sc.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped != 1 || rep.Missing != 1 || rep.Unreadable != 1 {
		t.Errorf("report = %+v; want 1 skipped, 1 missing, 1 unreadable", rep)
	}
	if f, _ := st.GetFileByPath(ctx, filepath.Join(locked, "a.mp3")); f.Status != store.FilePending {
		t.Errorf("file behind the locked folder = %s", f.Status)
	}
}
