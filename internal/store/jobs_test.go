package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestJobsQueue(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	a, _ := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", LastSeen: t0})
	b, _ := s.UpsertScannedFile(ctx, File{Path: "/m/b.mp3", LastSeen: t0})

	if added, err := s.EnqueueJob(ctx, "identify", a.ID, t0); err != nil || !added {
		t.Fatalf("EnqueueJob a = %v, %v", added, err)
	}
	if added, err := s.EnqueueJob(ctx, "identify", a.ID, t0.Add(time.Hour)); err != nil || added {
		t.Fatalf("second EnqueueJob a = %v, %v (want idempotent)", added, err)
	}
	if _, err := s.EnqueueJob(ctx, "identify", b.ID, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountJobs(ctx, "identify"); n != 2 {
		t.Fatalf("CountJobs = %d, want 2", n)
	}
	j, err := s.NextJob(ctx, "identify", t0.Add(time.Minute))
	if err != nil || j.FileID != a.ID {
		t.Fatalf("NextJob = %+v, %v; want a", j, err)
	}
	if err := s.RescheduleJob(ctx, j.ID, 1, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	j, err = s.NextJob(ctx, "identify", t0.Add(time.Minute))
	if err != nil || j.FileID != b.ID {
		t.Fatalf("NextJob after reschedule = %+v, %v; want b", j, err)
	}
	s.DeleteJob(ctx, j.ID)
	if _, err := s.NextJob(ctx, "identify", t0.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("NextJob before run_after err = %v, want ErrNotFound", err)
	}
	j, err = s.NextJob(ctx, "identify", t0.Add(time.Hour))
	if err != nil || j.FileID != a.ID || j.Attempts != 1 || !j.RunAfter.Equal(t0.Add(time.Hour)) {
		t.Fatalf("NextJob later = %+v, %v", j, err)
	}

	// Deleting the file removes its job.
	if err := s.DeleteFile(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountJobs(ctx, "identify"); n != 0 {
		t.Errorf("CountJobs after DeleteFile = %d, want 0", n)
	}
}

func TestJobsPersistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", LastSeen: t0})
	s.EnqueueJob(ctx, "identify", f.ID, t0)
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	j, err := s.NextJob(ctx, "identify", t0)
	if err != nil || j.FileID != f.ID {
		t.Fatalf("NextJob after reopen = %+v, %v", j, err)
	}
}

func TestPendingFilesWithoutJob(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	a, _ := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", LastSeen: t0})
	b, _ := s.UpsertScannedFile(ctx, File{Path: "/m/b.mp3", LastSeen: t0})
	c, _ := s.UpsertScannedFile(ctx, File{Path: "/m/c.mp3", LastSeen: t0})
	d, _ := s.UpsertScannedFile(ctx, File{Path: "/m/d.mp3", LastSeen: t0})
	s.EnqueueJob(ctx, "identify", b.ID, t0)
	s.SetCandidates(ctx, c.ID, []Candidate{{RecordingID: "r1", Score: 50, Source: SourceSearch}})
	s.SetFileStatus(ctx, d.ID, FileLocal)
	ids, err := s.PendingFilesWithoutJob(ctx, "identify")
	if err != nil || len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("PendingFilesWithoutJob = %v, %v; want [a]", ids, err)
	}
}
