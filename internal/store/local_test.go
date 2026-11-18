package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/media"
)

func TestCreateLocalTrackAttachesPendingFile(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	f, err := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", Status: FilePending, LastSeen: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCandidates(ctx, f.ID, []Candidate{{RecordingID: "r1", Score: 50}}); err != nil {
		t.Fatal(err)
	}
	lt, err := s.CreateLocalTrack(ctx, f.ID, LocalTrack{Title: "Démo", Artist: "Moi", Album: "Maison", Disc: 1, Position: 4, Year: 2021, CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if lt.ID == "" || lt.Title != "Démo" || lt.Year != 2021 || !lt.CreatedAt.Equal(t0) {
		t.Fatalf("local track = %+v", lt)
	}
	g, _ := s.GetFile(ctx, f.ID)
	if g.Status != FileLocal || g.LocalTrackID != lt.ID || g.Disc != 1 || g.Position != 4 {
		t.Errorf("file = %+v", g)
	}
	if c, _ := s.ListCandidates(ctx, f.ID); len(c) != 0 {
		t.Errorf("candidates kept: %+v", c)
	}
	// A second creation for the same file is refused and writes nothing.
	if _, err := s.CreateLocalTrack(ctx, f.ID, LocalTrack{Title: "x", Artist: "y"}); !errors.Is(err, ErrNotPending) {
		t.Errorf("second create err = %v", err)
	}
	if _, err := s.CreateLocalTrack(ctx, "nope", LocalTrack{Title: "x", Artist: "y"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown file err = %v", err)
	}
	all, _ := s.ListLocalTracks(ctx)
	if len(all) != 1 {
		t.Errorf("local tracks = %d, want 1", len(all))
	}
	if err := s.SetLocalTrackCover(ctx, lt.ID, lt.ID+".png"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetLocalTrack(ctx, lt.ID); got.CoverPath != lt.ID+".png" {
		t.Errorf("cover = %q", got.CoverPath)
	}
	if err := s.SetLocalTrackCover(ctx, "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cover unknown err = %v", err)
	}
	if _, err := s.GetLocalTrack(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get unknown err = %v", err)
	}
}

// R17: an uploaded file still in the incoming folder is never flagged as
// missing, whatever its status; once moved into the music folder it is.
func TestMarkMissingSkipsUploadsUnderIncoming(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	incoming := filepath.Join(string(filepath.Separator)+"data", "incoming")
	inside, _ := s.InsertUploadedFile(ctx, File{Path: filepath.Join(incoming, "a.mp3"), AddedAt: t0})
	placed, _ := s.InsertUploadedFile(ctx, File{Path: filepath.Join(string(filepath.Separator)+"music", "b.mp3"), AddedAt: t0})
	lookalike, _ := s.InsertUploadedFile(ctx, File{Path: incoming + "-old" + string(filepath.Separator) + "c.mp3", AddedAt: t0})
	for _, id := range []string{inside.ID, placed.ID, lookalike.ID} {
		if err := s.SetFileStatus(ctx, id, FileIdentified); err != nil {
			t.Fatal(err)
		}
	}
	later := t0.Add(time.Hour)
	n, err := s.MarkMissing(ctx, later, later, incoming+string(filepath.Separator))
	if err != nil || n != 2 {
		t.Fatalf("MarkMissing = %d, %v; want 2", n, err)
	}
	if g, _ := s.GetFile(ctx, inside.ID); g.Status != FileIdentified {
		t.Errorf("incoming file status = %s", g.Status)
	}
	if g, _ := s.GetFile(ctx, placed.ID); g.Status != FileMissing {
		t.Errorf("placed file status = %s", g.Status)
	}
}

func TestSetFilePathAndFinishPlacing(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	a, _ := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", Tags: media.Tags{Title: "old", Artist: "keep"}, LastSeen: t0})
	b, _ := s.UpsertScannedFile(ctx, File{Path: "/m/b.mp3", LastSeen: t0})
	if err := s.SetFilePath(ctx, a.ID, b.Path); !errors.Is(err, ErrPathTaken) {
		t.Errorf("taken err = %v", err)
	}
	if err := s.SetFilePath(ctx, "nope", "/m/z.mp3"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown err = %v", err)
	}
	if err := s.SetFilePath(ctx, a.ID, "/m/new/a.mp3"); err != nil {
		t.Fatal(err)
	}
	s.SetFileStatus(ctx, a.ID, FileIdentified)
	s.MarkMissing(ctx, t0.Add(time.Hour), t0.Add(time.Hour), "")
	t1 := t0.Add(2 * time.Hour)
	if err := s.FinishPlacing(ctx, a.ID, 99, t1, &media.Tags{Title: "new", Track: 3}, t1); err != nil {
		t.Fatal(err)
	}
	g, _ := s.GetFile(ctx, a.ID)
	if g.Path != "/m/new/a.mp3" || g.Origin != OriginDropped || g.Status != FileIdentified || g.Size != 99 ||
		!g.Mtime.Equal(t1) || !g.LastSeen.Equal(t1) || g.Tags.Title != "new" || g.Tags.Artist != "keep" || g.Tags.Track != 3 {
		t.Errorf("placed = %+v", g)
	}
}

func TestDeletePendingUpload(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	up, _ := s.InsertUploadedFile(ctx, File{Path: "/in/a.mp3"})
	dropped, _ := s.UpsertScannedFile(ctx, File{Path: "/m/a.mp3", Status: FilePending})
	done, _ := s.InsertUploadedFile(ctx, File{Path: "/in/b.mp3"})
	s.SetFileStatus(ctx, done.ID, FileIdentified)
	if _, err := s.DeletePendingUpload(ctx, dropped.ID); !errors.Is(err, ErrNotUploaded) {
		t.Errorf("dropped err = %v", err)
	}
	if _, err := s.DeletePendingUpload(ctx, done.ID); !errors.Is(err, ErrNotPending) {
		t.Errorf("identified err = %v", err)
	}
	if _, err := s.DeletePendingUpload(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown err = %v", err)
	}
	f, err := s.DeletePendingUpload(ctx, up.ID)
	if err != nil || f.Path != "/in/a.mp3" {
		t.Fatalf("delete = %+v, %v", f, err)
	}
	if _, err := s.GetFile(ctx, up.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("row still there: %v", err)
	}
}

func TestAwaitingFiles(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	a, _ := s.InsertUploadedFile(ctx, File{Path: "/in/a.mp3", AddedAt: t0})
	b, _ := s.UpsertScannedFile(ctx, File{Path: "/m/b.mp3", Status: FilePending, LastSeen: t0.Add(time.Minute)})
	c, _ := s.UpsertScannedFile(ctx, File{Path: "/m/c.mp3", Status: FilePending, LastSeen: t0})
	s.SetFileStatus(ctx, c.ID, FileIdentified)
	if _, err := s.EnqueueJob(ctx, "identify", b.ID, t0); err != nil {
		t.Fatal(err)
	}
	awaiting, queued, err := s.CountPending(ctx, "identify")
	if err != nil || awaiting != 1 || queued != 1 {
		t.Fatalf("CountPending = %d, %d, %v", awaiting, queued, err)
	}
	list, err := s.ListAwaiting(ctx, "identify", 10)
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("ListAwaiting = %+v, %v", list, err)
	}
}
