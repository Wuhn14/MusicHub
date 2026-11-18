package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/media"
)

func TestFileUpsertKeepsLinksAndRestoresMissing(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	f, err := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", Size: 10, Mtime: t0, Tags: media.Tags{Title: "T", Track: 3}, Status: FilePending, LastSeen: t0})
	if err != nil {
		t.Fatal(err)
	}
	if f.ID == "" || f.Origin != OriginDropped || f.Tags.Title != "T" || f.Tags.Track != 3 {
		t.Fatalf("file = %+v", f)
	}
	if err := s.SetFileStatus(ctx, f.ID, FileIdentified); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(time.Hour)
	if n, err := s.MarkMissing(ctx, t1, t1); err != nil || n != 1 {
		t.Fatalf("MarkMissing = %d, %v", n, err)
	}
	if n, _ := s.MarkMissing(ctx, t1, t1); n != 0 {
		t.Errorf("second MarkMissing = %d, want 0", n)
	}
	g, _ := s.GetFile(ctx, f.ID)
	if g.Status != FileMissing || g.MissingSince.IsZero() {
		t.Fatalf("after MarkMissing = %+v", g)
	}
	h, err := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", Size: 11, Mtime: t1, Status: FilePending, LastSeen: t1})
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != f.ID || h.Status != FileIdentified || !h.MissingSince.IsZero() || h.Size != 11 {
		t.Errorf("restored = %+v", h)
	}
	if _, err := s.GetFile(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetFile err = %v", err)
	}
	if err := s.SetFileStatus(ctx, "nope", FileLocal); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetFileStatus err = %v", err)
	}
}

func TestPurgeMissing(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", LastSeen: t0})
	s.UpsertScannedFile(ctx, File{Path: "/m/b.mp3", LastSeen: t0.Add(48 * time.Hour)})
	s.MarkMissing(ctx, t0.Add(time.Hour), t0.Add(2*time.Hour))
	if n, _ := s.PurgeMissing(ctx, t0); n != 0 {
		t.Errorf("early purge = %d", n)
	}
	if n, _ := s.PurgeMissing(ctx, t0.Add(3*time.Hour)); n != 1 {
		t.Errorf("purge = %d, want 1", n)
	}
	all, _ := s.ListFiles(ctx, FileFilter{})
	if len(all) != 1 || all[0].Path != "/m/b.mp3" {
		t.Errorf("remaining = %+v", all)
	}
}
