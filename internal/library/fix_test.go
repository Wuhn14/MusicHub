package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/store"
)

func okProbe(string) (media.Probe, error) { return media.Probe{Format: "mp3"}, nil }

func TestChangedFileBecomesUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp3")
	os.WriteFile(path, []byte("a"), 0o644)
	bad := false
	p := &countingProber{fn: func(string) (media.Probe, error) {
		if bad {
			return media.Probe{}, media.ErrNotAudio
		}
		return media.Probe{Format: "mp3", DurationMs: 1000}, nil
	}}
	st := openStore(t)
	ctx := context.Background()
	clk := newClock()
	sc := NewScanner(st, p, dir, clk.now)
	var added int
	sc.OnFileAdded = func(store.File) { added++ }
	sc.Scan(ctx)
	f, _ := st.GetFileByPath(ctx, path)
	st.SetFileStatus(ctx, f.ID, store.FileIdentified)

	bad = true
	os.WriteFile(path, []byte("corrupted"), 0o644)
	clk.t = clk.t.Add(time.Hour)
	rep, _ := sc.Scan(ctx)
	g, _ := st.GetFile(ctx, f.ID)
	if g.Status != store.FileUnreadable || rep.Unreadable != 1 || added != 1 {
		t.Errorf("status=%s report=%+v onAdded=%d (want 1: only the first scan)", g.Status, rep, added)
	}
}

func TestTransientProbeErrorIsRetried(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.mp3"), []byte("a"), 0o644)
	failing := true
	p := &countingProber{fn: func(string) (media.Probe, error) {
		if failing {
			return media.Probe{}, errors.New("timeout")
		}
		return media.Probe{Format: "mp3"}, nil
	}}
	st := openStore(t)
	ctx := context.Background()
	clk := newClock()
	sc := NewScanner(st, p, dir, clk.now)
	rep, err := sc.Scan(ctx)
	if err != nil || rep.Errors != 1 || rep.Added != 0 {
		t.Fatalf("report=%+v err=%v", rep, err)
	}
	if files, _ := st.ListFiles(ctx, store.FileFilter{}); len(files) != 0 {
		t.Fatalf("transient error recorded a row: %+v", files)
	}
	failing = false
	clk.t = clk.t.Add(time.Hour)
	rep, _ = sc.Scan(ctx)
	if rep.Added != 1 || rep.Errors != 0 {
		t.Errorf("retry report = %+v", rep)
	}
	files, _ := st.ListFiles(ctx, store.FileFilter{})
	if len(files) != 1 || files[0].Status != store.FilePending {
		t.Errorf("files = %+v", files)
	}
}

func TestTransientErrorDoesNotMarkMissing(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mp3")
	b := filepath.Join(dir, "b.mp3")
	os.WriteFile(a, []byte("a"), 0o644)
	os.WriteFile(b, []byte("b"), 0o644)
	p := &countingProber{fn: okProbe}
	st := openStore(t)
	ctx := context.Background()
	clk := newClock()
	sc := NewScanner(st, p, dir, clk.now)
	sc.Scan(ctx)
	os.Remove(a)
	os.WriteFile(b, []byte("bb"), 0o644) // changed: re-probed, fails
	p.fn = func(string) (media.Probe, error) { return media.Probe{}, errors.New("timeout") }
	clk.t = clk.t.Add(time.Hour)
	rep, _ := sc.Scan(ctx)
	if rep.Missing != 0 || rep.Errors != 1 {
		t.Errorf("report = %+v", rep)
	}
	if m, _ := st.ListFiles(ctx, store.FileFilter{Status: store.FileMissing}); len(m) != 0 {
		t.Errorf("missing = %+v", m)
	}
}

func TestCancelledScanMarksNothingMissing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.mp3"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.mp3"), []byte("b"), 0o644)
	st := openStore(t)
	clk := newClock()
	sc := NewScanner(st, &countingProber{fn: okProbe}, dir, clk.now)
	sc.Scan(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clk.t = clk.t.Add(time.Hour)
	if _, err := sc.Scan(ctx); err == nil {
		t.Fatal("want cancellation error")
	}
	if m, _ := st.ListFiles(context.Background(), store.FileFilter{Status: store.FileMissing}); len(m) != 0 {
		t.Errorf("cancelled scan marked missing: %+v", m)
	}
}

func TestOnFileAddedForChangedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mp3")
	os.WriteFile(path, []byte("a"), 0o644)
	st := openStore(t)
	clk := newClock()
	sc := NewScanner(st, &countingProber{fn: okProbe}, dir, clk.now)
	var got []string
	sc.OnFileAdded = func(f store.File) { got = append(got, f.Path) }
	sc.Scan(context.Background())
	os.WriteFile(path, []byte("changed"), 0o644)
	clk.t = clk.t.Add(time.Hour)
	sc.Scan(context.Background())
	if len(got) != 2 {
		t.Errorf("OnFileAdded calls = %v, want 2", got)
	}
}

type noFFprobe struct{ countingProber }

func (*noFFprobe) FFprobeAvailable() bool { return false }

func TestScanAbortsWithoutFFprobe(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.mp3"), []byte("a"), 0o644)
	st := openStore(t)
	_, err := NewScanner(st, &noFFprobe{}, dir, newClock().now).Scan(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ffprobe est introuvable") {
		t.Errorf("err = %v", err)
	}
}
