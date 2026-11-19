package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCandidatesReplaceAndLink(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	f, _ := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", LastSeen: t0})
	if err := s.SetCandidates(ctx, f.ID, []Candidate{
		{RecordingID: "r1", Title: "One", Score: 90, Source: SourceAcoustID},
		{RecordingID: "r2", Title: "Two", Artist: "X", Album: "Y", AlbumID: "al", LengthMs: 1000, Score: 80, Source: SourceSearch},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListCandidates(ctx, f.ID)
	if err != nil || len(got) != 2 || got[0].Rank != 1 || got[0].RecordingID != "r1" ||
		got[1].Rank != 2 || got[1].Album != "Y" || got[1].LengthMs != 1000 || got[1].Source != SourceSearch {
		t.Fatalf("ListCandidates = %+v, %v", got, err)
	}
	s.SetCandidates(ctx, f.ID, []Candidate{{RecordingID: "r3", Score: 70, Source: SourceSearch}})
	if got, _ := s.ListCandidates(ctx, f.ID); len(got) != 1 || got[0].RecordingID != "r3" {
		t.Fatalf("after replace = %+v", got)
	}
	if err := s.LinkFile(ctx, f.ID, "r3", "al", 2, 5); err != nil {
		t.Fatal(err)
	}
	g, _ := s.GetFile(ctx, f.ID)
	if g.Status != FileIdentified || g.RecordingID != "r3" || g.AlbumID != "al" || g.Disc != 2 || g.Position != 5 {
		t.Errorf("linked = %+v", g)
	}
	if got, _ := s.ListCandidates(ctx, f.ID); len(got) != 0 {
		t.Errorf("candidates after link = %+v", got)
	}
	if err := s.LinkFile(ctx, "nope", "r", "", 0, 0); err != ErrNotFound {
		t.Errorf("LinkFile unknown = %v", err)
	}
}

func TestAvailability(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	if err := s.UpsertAlbum(ctx, Album{ID: "alA", Title: "A", ArtistID: "ar", FetchedAt: t0, Tracks: []Track{
		{RecordingID: "r1", Title: "One", Disc: 1, Position: 1},
		{RecordingID: "r2", Title: "Two", Disc: 1, Position: 2},
		{RecordingID: "r3", Title: "Three", Disc: 1, Position: 3},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAlbum(ctx, Album{ID: "alB", Title: "B", ArtistID: "ar", FetchedAt: t0, Tracks: []Track{
		{RecordingID: "r2", Title: "Two", Disc: 1, Position: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	add := func(path, rec, album, status string) string {
		f, _ := s.UpsertScannedFile(ctx, File{Path: path, LastSeen: t0})
		s.LinkFile(ctx, f.ID, rec, album, 1, 1)
		if status != FileIdentified {
			s.SetFileStatus(ctx, f.ID, status)
		}
		return f.ID
	}
	f1 := add("/m/1.mp3", "r1", "alA", FileIdentified)
	f2 := add("/m/2.mp3", "r2", "alB", FileIdentified) // owned through album B
	add("/m/3.mp3", "r3", "alA", FileHidden)           // hidden: not available
	fx := add("/m/x.mp3", "rx", "alA", FileIdentified) // linked to A, not in its track list

	got, err := s.AlbumAvailability(ctx, "alA")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"r1": f1, "r2": f2, "rx": fx}
	if len(got) != len(want) {
		t.Fatalf("AlbumAvailability = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("AlbumAvailability[%s] = %q, want %q", k, got[k], v)
		}
	}
	counts, err := s.ArtistAvailability(ctx, "ar")
	if err != nil || counts["alA"] != 3 || counts["alB"] != 1 || len(counts) != 2 {
		t.Errorf("ArtistAvailability = %v, %v", counts, err)
	}
	if counts, _ := s.ArtistAvailability(ctx, "other"); len(counts) != 0 {
		t.Errorf("ArtistAvailability(other) = %v", counts)
	}
}

func TestIdentificationWritesNeedPending(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	f, _ := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", LastSeen: t0})
	s.SetFileStatus(ctx, f.ID, FileMissing)
	if err := s.LinkFile(ctx, f.ID, "r1", "al", 1, 1); !errors.Is(err, ErrNotPending) {
		t.Errorf("LinkFile on a missing file = %v, want ErrNotPending", err)
	}
	if err := s.SetCandidates(ctx, f.ID, []Candidate{{RecordingID: "r1"}}); !errors.Is(err, ErrNotPending) {
		t.Errorf("SetCandidates on a missing file = %v, want ErrNotPending", err)
	}
	if err := s.FinishUnmatched(ctx, "nope", nil, t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("FinishUnmatched unknown = %v, want ErrNotFound", err)
	}
	if g, _ := s.GetFile(ctx, f.ID); g.Status != FileMissing || g.RecordingID != "" {
		t.Errorf("file = %+v", g)
	}
	if c, _ := s.ListCandidates(ctx, f.ID); len(c) != 0 {
		t.Errorf("candidates = %+v", c)
	}
}

func TestIdentifyAttemptMarker(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	f, _ := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", Size: 10, Mtime: t0, LastSeen: t0})
	// Partial candidates (a failed run) do not count as an attempt.
	if err := s.SetCandidates(ctx, f.ID, nil); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.PendingFilesWithoutJob(ctx, "identify"); len(ids) != 1 {
		t.Fatalf("backlog after SetCandidates = %v, want the file", ids)
	}
	// A finished run without match nor candidates: not queued again.
	if err := s.FinishUnmatched(ctx, f.ID, nil, t0); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.PendingFilesWithoutJob(ctx, "identify"); len(ids) != 0 {
		t.Fatalf("backlog after FinishUnmatched = %v, want none", ids)
	}
	// The file changes on disk: the attempt is forgotten.
	if _, err := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", Size: 11, Mtime: t0.Add(time.Hour), Status: FilePending, LastSeen: t0}); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.PendingFilesWithoutJob(ctx, "identify"); len(ids) != 1 || ids[0] != f.ID {
		t.Fatalf("backlog after a change = %v, want the file", ids)
	}
}
