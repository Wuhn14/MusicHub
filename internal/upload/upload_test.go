package upload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/media/mediatest"
	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
)

type fixedSettings int64

func (f fixedSettings) Get(context.Context) (settings.Settings, error) {
	return settings.Settings{MaxUploadBytes: int64(f)}, nil
}

type neverProber struct{ t *testing.T }

func (p neverProber) Probe(context.Context, string) (media.Probe, error) {
	p.t.Error("Probe must not be called")
	return media.Probe{}, errors.New("unexpected")
}

type setup struct {
	st       *store.Store
	incoming string
	accepted []store.File
	r        *Receiver
}

func newSetup(t *testing.T, tools Prober, max int64) *setup {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &setup{st: st, incoming: filepath.Join(t.TempDir(), "incoming")}
	s.r = New(st, tools, s.incoming, fixedSettings(max), func(f store.File) { s.accepted = append(s.accepted, f) })
	return s
}

func (s *setup) entries(t *testing.T) []string {
	t.Helper()
	es, err := os.ReadDir(s.incoming)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

func (s *setup) fileCount(t *testing.T) int {
	t.Helper()
	files, err := s.st.ListFiles(context.Background(), store.FileFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}

func realTools(t *testing.T) *media.Tools {
	mediatest.RequireTool(t, "ffprobe")
	return media.New("ffmpeg", "ffprobe", "fpcalc")
}

func TestReceiveAcceptsAudio(t *testing.T) {
	s := newSetup(t, realTools(t), 1<<30)
	src := mediatest.Sine(t, t.TempDir(), "x.mp3", 2, map[string]string{"title": "Invented", "artist": "Nobody"})
	raw, _ := os.ReadFile(src)

	f, err := s.r.Receive(context.Background(), `Mon dossier\Some Song.MP3`, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	// The sender's name is kept for display only, without its folders.
	if f.UploadName != "Some Song.MP3" || strings.Contains(f.Path, "Some Song") {
		t.Errorf("upload name %q, path %q", f.UploadName, f.Path)
	}
	if f.Origin != store.OriginUploaded || f.Status != store.FilePending || f.Tags.Title != "Invented" || f.DurationMs < 1500 {
		t.Errorf("stored file: %+v", f)
	}
	if filepath.Dir(f.Path) != s.incoming || !strings.HasSuffix(f.Path, ".mp3") || f.Size != int64(len(raw)) {
		t.Errorf("path %q size %d", f.Path, f.Size)
	}
	if got, err := os.ReadFile(f.Path); err != nil || !bytes.Equal(got, raw) {
		t.Errorf("stored content differs: %v", err)
	}
	if names := s.entries(t); len(names) != 1 {
		t.Errorf("incoming: %v", names)
	}
	if len(s.accepted) != 1 || s.accepted[0].ID != f.ID {
		t.Errorf("onAccepted calls: %v", s.accepted)
	}
	if db, err := s.st.GetFile(context.Background(), f.ID); err != nil || db.Path != f.Path {
		t.Errorf("db row: %+v %v", db, err)
	}
}

func TestReceiveRejectsBadExtension(t *testing.T) {
	s := newSetup(t, neverProber{t}, 1<<30)
	for _, name := range []string{"notes.txt", "song", "song.mp3.exe", ""} {
		_, err := s.r.Receive(context.Background(), name, strings.NewReader("data"))
		if !errors.Is(err, ErrUnsupportedFormat) {
			t.Errorf("%q: %v", name, err)
		}
	}
	if names := s.entries(t); len(names) != 0 {
		t.Errorf("incoming not empty: %v", names)
	}
	if s.fileCount(t) != 0 || len(s.accepted) != 0 {
		t.Error("a rejected upload was recorded")
	}
}

func TestReceiveRejectsNonAudio(t *testing.T) {
	s := newSetup(t, realTools(t), 1<<30)
	_, err := s.r.Receive(context.Background(), "fake.mp3", strings.NewReader("this is just text, not music"))
	if !errors.Is(err, ErrNotAudio) {
		t.Fatalf("err = %v", err)
	}
	if names := s.entries(t); len(names) != 0 {
		t.Errorf("incoming not empty: %v", names)
	}
	if s.fileCount(t) != 0 || len(s.accepted) != 0 {
		t.Error("a rejected upload was recorded")
	}
}

func TestReceiveTooLarge(t *testing.T) {
	s := newSetup(t, neverProber{t}, 100)
	_, err := s.r.Receive(context.Background(), "big.mp3", bytes.NewReader(make([]byte, 101)))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if names := s.entries(t); len(names) != 0 || s.fileCount(t) != 0 {
		t.Errorf("leftovers: %v", names)
	}
	// Exactly at the limit is fine for the size check (the probe then runs).
	if _, err := s.r.Receive(context.Background(), "empty.mp3", strings.NewReader("")); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty body: %v", err)
	}
}

type brokenReader struct{ sent bool }

func (b *brokenReader) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, bytes.Repeat([]byte{1}, 1000)), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func TestInterruptedUploadLeavesNothing(t *testing.T) {
	s := newSetup(t, neverProber{t}, 1<<30)
	_, err := s.r.Receive(context.Background(), "cut.mp3", &brokenReader{})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v", err)
	}
	if names := s.entries(t); len(names) != 0 {
		t.Errorf("incoming not empty: %v", names)
	}
	if s.fileCount(t) != 0 || len(s.accepted) != 0 {
		t.Error("an interrupted upload was recorded")
	}
	// A cancelled context stops a stalled upload too.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.r.Receive(ctx, "x.mp3", strings.NewReader("abc")); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if names := s.entries(t); len(names) != 0 {
		t.Errorf("incoming not empty: %v", names)
	}
}

func TestStartupCleansPartFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "incoming")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a.mp3.part", "b.flac.part", "keep.mp3"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := CleanPartFiles(dir)
	if err != nil || n != 2 {
		t.Fatalf("removed %d, err %v", n, err)
	}
	es, _ := os.ReadDir(dir)
	if len(es) != 1 || es[0].Name() != "keep.mp3" {
		t.Errorf("remaining: %v", es)
	}
	// A missing folder is created.
	if n, err := CleanPartFiles(filepath.Join(t.TempDir(), "new", "incoming")); err != nil || n != 0 {
		t.Errorf("new dir: %d %v", n, err)
	}
}

func TestClientNameNeverUsedAsPath(t *testing.T) {
	s := newSetup(t, realTools(t), 1<<30)
	root := filepath.Dir(s.incoming)
	src := mediatest.Sine(t, t.TempDir(), "x.mp3", 1, nil)
	raw, _ := os.ReadFile(src)

	for _, name := range []string{"../../evil.mp3", `..\..\evil.mp3`, "/etc/evil.mp3", `C:\evil.mp3`} {
		f, err := s.r.Receive(context.Background(), name, bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if filepath.Dir(f.Path) != s.incoming || strings.Contains(filepath.Base(f.Path), "evil") {
			t.Errorf("%q stored at %q", name, f.Path)
		}
	}
	for _, p := range []string{filepath.Join(root, "evil.mp3"), filepath.Join(filepath.Dir(root), "evil.mp3")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("file written outside incoming: %s", p)
		}
	}
}
