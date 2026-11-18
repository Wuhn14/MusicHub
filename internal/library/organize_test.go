package library

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/media/mediatest"
	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
)

type fakeSettings struct{ s settings.Settings }

func (f *fakeSettings) Get(context.Context) (settings.Settings, error) { return f.s, nil }

// recordingWriter remembers the tags it was asked to write.
type recordingWriter struct {
	mu    sync.Mutex
	calls map[string]media.Tags
}

func (r *recordingWriter) WriteTags(_ context.Context, path string, t media.Tags) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.calls == nil {
		r.calls = map[string]media.Tags{}
	}
	r.calls[path] = t
	return nil
}

type orgEnv struct {
	st       *store.Store
	music    string
	incoming string
	set      *fakeSettings
	org      *Organizer
	n        int
}

func newOrgEnv(t *testing.T, tools TagWriter) *orgEnv {
	t.Helper()
	root := t.TempDir()
	e := &orgEnv{
		st:       openStore(t),
		music:    filepath.Join(root, "music"),
		incoming: filepath.Join(root, "data", "incoming"),
		set:      &fakeSettings{s: settings.Defaults()},
	}
	for _, d := range []string{e.music, e.incoming} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.org = NewOrganizer(e.st, tools, e.music, e.set)
	return e
}

// upload records an uploaded file in the incoming folder with the given
// content.
func (e *orgEnv) upload(t *testing.T, content string) store.File {
	t.Helper()
	e.n++
	path := filepath.Join(e.incoming, strings.Repeat("ab", 8)+string(rune('a'+e.n))+".mp3")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := e.st.InsertUploadedFile(context.Background(), store.File{Path: path, Size: int64(len(content))})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (e *orgEnv) makeLocal(t *testing.T, f store.File, lt store.LocalTrack) {
	t.Helper()
	if _, err := e.st.CreateLocalTrack(context.Background(), f.ID, lt); err != nil {
		t.Fatal(err)
	}
}

func (e *orgEnv) place(t *testing.T, id string) store.File {
	t.Helper()
	f, err := e.org.Place(context.Background(), id)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	return f
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSanitizeSegment(t *testing.T) {
	long := strings.Repeat("é", 150) // 300 bytes
	cases := []struct{ in, want string }{
		{"Café crème", "Café crème"},
		{`a/b\c:d*e?f"g<h>i|j`, "a_b_c_d_e_f_g_h_i_j"},
		{"tab\there\x00", "tab_here_"},
		{"CON", "CON_"},
		{"con", "con_"},
		{"Nul.txt", "Nul_.txt"},
		{"LPT9", "LPT9_"},
		{"COM10", "COM10"},
		{"Console", "Console"},
		{"trailing. . ", "trailing"},
		{"...", "_"},
		{"", "_"},
		{"   ", "_"},
		{".hidden", "_hidden"},
		{"..", "_"},
		{long, strings.Repeat("é", 60)},
	}
	for _, c := range cases {
		if got := sanitizeSegment(c.in, maxSegmentBytes); got != c.want {
			t.Errorf("sanitizeSegment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// An odd byte limit never splits a two-byte character.
	if got := sanitizeSegment(long, 7); got != "ééé" {
		t.Errorf("odd limit = %q", got)
	}
}

func TestOrganizeSanitizesNames(t *testing.T) {
	e := newOrgEnv(t, nil)
	e.set.s.WriteTagsUploaded = false
	album := strings.Repeat("Ä", 150) // 300 bytes
	f := e.upload(t, "audio")
	e.makeLocal(t, f, store.LocalTrack{
		Title: `What? "Yes": a/b\c*<d>|`, Artist: "CON", Album: album, Year: 1999, Disc: 2, Position: 7,
	})
	got := e.place(t, f.ID)
	rel, err := filepath.Rel(e.music, got.Path)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 3 {
		t.Fatalf("path %q: want artist/album/file", rel)
	}
	if parts[0] != "CON_" {
		t.Errorf("artist folder = %q", parts[0])
	}
	wantAlbum := strings.Repeat("Ä", 56) + " (1999)"
	if parts[1] != wantAlbum {
		t.Errorf("album folder = %q (%d bytes), want %q", parts[1], len(parts[1]), wantAlbum)
	}
	if parts[2] != `2-07 - What_ _Yes__ a_b_c__d__.mp3` {
		t.Errorf("file name = %q", parts[2])
	}
	for _, p := range parts {
		if len(p) > maxSegmentBytes {
			t.Errorf("segment %q is %d bytes", p, len(p))
		}
	}
	if readFile(t, got.Path) != "audio" {
		t.Error("content changed")
	}
	if _, err := os.Stat(f.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source still there: %v", err)
	}
	if got.ID != f.ID || got.Origin != store.OriginUploaded || got.Status != store.FileLocal {
		t.Errorf("row = %+v", got)
	}

	// A track without album, year or number goes under "Titres sans album".
	g := e.upload(t, "single")
	e.makeLocal(t, g, store.LocalTrack{Title: "Solo", Artist: "Quelqu'un"})
	placed := e.place(t, g.ID)
	if want := filepath.Join(e.music, "Quelqu'un", "Titres sans album", "Solo.mp3"); placed.Path != want {
		t.Errorf("no album path = %q, want %q", placed.Path, want)
	}
	// An album without year has no parentheses; the album artist wins.
	h := e.upload(t, "x")
	e.makeLocal(t, h, store.LocalTrack{Title: "Un", Artist: "Invité", AlbumArtist: "Groupe", Album: "Disque", Position: 1, Disc: 1})
	if p := e.place(t, h.ID).Path; p != filepath.Join(e.music, "Groupe", "Disque", "01 - Un.mp3") {
		t.Errorf("no year path = %q", p)
	}
}

func TestOrganizeCollision(t *testing.T) {
	e := newOrgEnv(t, nil)
	lt := store.LocalTrack{Title: "Même", Artist: "Pareil", Album: "Double", Year: 2020, Position: 1}
	dir := filepath.Join(e.music, "Pareil", "Double (2020)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A file the user already has at the destination is never overwritten.
	existing := filepath.Join(dir, "01 - Même.mp3")
	if err := os.WriteFile(existing, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, b := e.upload(t, "first"), e.upload(t, "second")
	e.makeLocal(t, a, lt)
	e.makeLocal(t, b, lt)
	pa, pb := e.place(t, a.ID), e.place(t, b.ID)
	if pa.Path != filepath.Join(dir, "01 - Même (2).mp3") || pb.Path != filepath.Join(dir, "01 - Même (3).mp3") {
		t.Errorf("paths = %q, %q", pa.Path, pb.Path)
	}
	if readFile(t, existing) != "mine" || readFile(t, pa.Path) != "first" || readFile(t, pb.Path) != "second" {
		t.Error("contents mixed up")
	}
	// Placing again keeps the file where it is.
	if again := e.place(t, a.ID); again.Path != pa.Path {
		t.Errorf("second Place moved the file to %q", again.Path)
	}
}

func TestOrganizeLeavesDroppedFilesByDefault(t *testing.T) {
	w := &recordingWriter{}
	e := newOrgEnv(t, w)
	path := filepath.Join(e.music, "en vrac.mp3")
	if err := os.WriteFile(path, []byte("dropped"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	f, err := e.st.UpsertScannedFile(ctx, store.File{Path: path, Status: store.FilePending, LastSeen: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	e.makeLocal(t, f, store.LocalTrack{Title: "Rangé", Artist: "Moi", Album: "Ici", Position: 2})
	got := e.place(t, f.ID)
	if got.Path != path || readFile(t, path) != "dropped" || len(w.calls) != 0 {
		t.Fatalf("dropped file touched: %+v, writes %v", got, w.calls)
	}

	// With the settings on, it is moved and gets its tags.
	e.set.s.OrganizeDropped, e.set.s.WriteTagsDropped = true, true
	got = e.place(t, f.ID)
	want := filepath.Join(e.music, "Moi", "Ici", "02 - Rangé.mp3")
	if got.Path != want || got.Origin != store.OriginDropped || got.ID != f.ID {
		t.Fatalf("organized = %+v", got)
	}
	if tg, ok := w.calls[want]; !ok || tg.Title != "Rangé" || tg.Track != 2 || tg.Album != "Ici" {
		t.Errorf("tags written = %+v", w.calls)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old path still there: %v", err)
	}
}

func TestOrganizeWritesTagsForUploads(t *testing.T) {
	tools := realTools(t)
	e := newOrgEnv(t, tools)
	ctx := context.Background()
	src := mediatest.Sine(t, e.incoming, "0123456789abcdef.mp3", 1, map[string]string{"title": "brut"})
	info, _ := os.Stat(src)
	f, err := e.st.InsertUploadedFile(ctx, store.File{Path: src, Size: info.Size()})
	if err != nil {
		t.Fatal(err)
	}
	// An identified file takes its names and ids from the catalog.
	if err := e.st.UpsertAlbum(ctx, store.Album{
		ID: "00000000-0000-4000-8000-0000000000b1", Title: "Papiers", ArtistName: "Les Lanternes",
		FirstReleaseDate: "2003-05-12", Tracks: []store.Track{{RecordingID: "00000000-0000-4000-8000-0000000000d1", Title: "Marée", Disc: 1, Position: 3}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.LinkFile(ctx, f.ID, "00000000-0000-4000-8000-0000000000d1", "00000000-0000-4000-8000-0000000000b1", 1, 3); err != nil {
		t.Fatal(err)
	}
	got := e.place(t, f.ID)
	want := filepath.Join(e.music, "Les Lanternes", "Papiers (2003)", "03 - Marée.mp3")
	if got.Path != want {
		t.Fatalf("path = %q, want %q", got.Path, want)
	}
	pr, err := tools.Probe(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	tg := pr.Tags
	if tg.Title != "Marée" || tg.Artist != "Les Lanternes" || tg.AlbumArtist != "Les Lanternes" || tg.Album != "Papiers" ||
		!strings.HasPrefix(tg.Date, "2003") || tg.Track != 3 || tg.Disc != 1 ||
		tg.MBRecordingID != "00000000-0000-4000-8000-0000000000d1" || tg.MBReleaseGroupID != "00000000-0000-4000-8000-0000000000b1" {
		t.Errorf("tags = %+v", tg)
	}
	// The row matches the file on disk, so the next scan does not reread it.
	now, _ := os.Stat(want)
	if got.Size != now.Size() || !got.Mtime.Equal(now.ModTime().UTC()) || got.Tags.Title != "Marée" {
		t.Errorf("row = size %d mtime %v tags %+v; disk size %d mtime %v", got.Size, got.Mtime, got.Tags, now.Size(), now.ModTime())
	}

	// With the setting off, the tags are left alone.
	e.set.s.WriteTagsUploaded = false
	src2 := mediatest.Sine(t, e.incoming, "fedcba9876543210.mp3", 1, map[string]string{"title": "brut"})
	g, _ := e.st.InsertUploadedFile(ctx, store.File{Path: src2})
	e.makeLocal(t, g, store.LocalTrack{Title: "Autre", Artist: "Moi"})
	placed := e.place(t, g.ID)
	if pr, err := tools.Probe(ctx, placed.Path); err != nil || pr.Tags.Title != "brut" {
		t.Errorf("tags rewritten with setting off: %+v, %v", pr.Tags, err)
	}
}

// fsHooks replaces the link and rename functions for one test.
func fsHooks(t *testing.T, link, rename func(string, string) error) {
	t.Helper()
	ol, or := linkFile, renameFile
	t.Cleanup(func() { linkFile, renameFile = ol, or })
	if link != nil {
		linkFile = link
	}
	if rename != nil {
		renameFile = rename
	}
}

var errCrossDevice = errors.New("invalid cross-device link")

// isTemp tells the organizer's temporary copies apart.
func isTemp(path string) bool { return strings.HasPrefix(filepath.Base(path), ".musichub-move-") }

func TestOrganizeCopiesAcrossDisks(t *testing.T) {
	e := newOrgEnv(t, nil)
	// Another disk: the source cannot be linked nor renamed; the temporary
	// copy (same folder as the destination) can.
	fsHooks(t,
		func(o, n string) error {
			if isTemp(o) {
				return os.Link(o, n)
			}
			return &os.LinkError{Op: "link", Old: o, New: n, Err: errCrossDevice}
		},
		func(o, n string) error { return &os.LinkError{Op: "rename", Old: o, New: n, Err: errCrossDevice} })

	payload := strings.Repeat("0123456789", 10000)
	f := e.upload(t, payload)
	e.makeLocal(t, f, store.LocalTrack{Title: "Loin", Artist: "Ailleurs"})
	got := e.place(t, f.ID)
	if got.Path == f.Path || readFile(t, got.Path) != payload {
		t.Fatalf("copy = %q", got.Path)
	}
	if _, err := os.Stat(f.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source kept after a verified copy: %v", err)
	}
	if got.PlacingFrom != "" {
		t.Errorf("move still recorded as in progress: %q", got.PlacingFrom)
	}
	assertNoTemp(t, filepath.Dir(got.Path))

	// When the copy cannot get its name (links and renames both fail), the
	// source stays, no temporary file is left and the row keeps its path.
	fsHooks(t,
		func(o, n string) error { return &os.LinkError{Op: "link", Old: o, New: n, Err: errCrossDevice} },
		func(o, n string) error { return &os.LinkError{Op: "rename", Old: o, New: n, Err: errors.New("denied")} })
	g := e.upload(t, "precious")
	e.makeLocal(t, g, store.LocalTrack{Title: "Gardé", Artist: "Ailleurs"})
	if _, err := e.org.Place(context.Background(), g.ID); err == nil {
		t.Fatal("Place succeeded")
	}
	if readFile(t, g.Path) != "precious" {
		t.Error("source lost")
	}
	dst := filepath.Join(e.music, "Ailleurs", "Titres sans album", "Gardé.mp3")
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("partial destination: %v", err)
	}
	if row, _ := e.st.GetFile(context.Background(), g.ID); row.Path != g.Path || row.PlacingFrom != "" {
		t.Errorf("row path = %q (from %q), want %q", row.Path, row.PlacingFrom, g.Path)
	}
	assertNoTemp(t, filepath.Dir(dst))
}

func TestOrganizeNeverReplacesAppearingDestination(t *testing.T) {
	for _, tc := range []struct {
		name string
		link func(string, string) error
	}{
		// Links work: the link itself refuses the existing name.
		{"link", nil},
		// No links on this filesystem: the check before the rename refuses it.
		{"no link", func(o, n string) error {
			return &os.LinkError{Op: "link", Old: o, New: n, Err: errors.New("operation not permitted")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newOrgEnv(t, nil)
			dst := filepath.Join(e.music, "Moi", "Titres sans album", "Course.mp3")
			created := false
			inner := tc.link
			if inner == nil {
				inner = os.Link
			}
			// Someone else writes the destination right before the move.
			fsHooks(t, func(o, n string) error {
				if !created && n == dst {
					created = true
					os.WriteFile(dst, []byte("theirs"), 0o644)
				}
				return inner(o, n)
			}, nil)
			f := e.upload(t, "mine")
			e.makeLocal(t, f, store.LocalTrack{Title: "Course", Artist: "Moi"})
			got := e.place(t, f.ID)
			if readFile(t, dst) != "theirs" {
				t.Error("destination replaced")
			}
			if want := filepath.Join(filepath.Dir(dst), "Course (2).mp3"); got.Path != want || readFile(t, want) != "mine" {
				t.Errorf("placed at %q", got.Path)
			}
		})
	}
}

func TestRepairPlacingAfterCrash(t *testing.T) {
	ctx := context.Background()

	t.Run("source still there", func(t *testing.T) {
		e := newOrgEnv(t, nil)
		f := e.upload(t, "audio")
		e.makeLocal(t, f, store.LocalTrack{Title: "Coupé", Artist: "Moi"})
		dst := filepath.Join(e.music, "Moi", "Titres sans album", "Coupé.mp3")
		// The process stops right after recording the move, mid-copy.
		if err := e.st.BeginPlacing(ctx, f.ID, dst, f.Path); err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		os.WriteFile(filepath.Join(filepath.Dir(dst), ".musichub-move-123.mp3"), []byte("aud"), 0o644)

		if err := e.org.RepairPlacing(ctx); err != nil {
			t.Fatal(err)
		}
		row, _ := e.st.GetFile(ctx, f.ID)
		if row.Path != f.Path || row.PlacingFrom != "" || readFile(t, row.Path) != "audio" {
			t.Fatalf("row = %q from %q", row.Path, row.PlacingFrom)
		}
		// The leftover temporary file goes in the background clean-up.
		if n, err := e.org.RemoveTemps(ctx); err != nil || n != 1 {
			t.Fatalf("RemoveTemps = %d, %v", n, err)
		}
		assertNoTemp(t, filepath.Dir(dst))
		// The startup sweep places it again.
		if n := e.org.PlaceStranded(ctx, e.incoming); n != 1 {
			t.Errorf("re-placed = %d", n)
		}
		if row, _ := e.st.GetFile(ctx, f.ID); row.Path != dst || readFile(t, dst) != "audio" {
			t.Errorf("after sweep = %q", row.Path)
		}
	})

	t.Run("copy finished", func(t *testing.T) {
		e := newOrgEnv(t, nil)
		f := e.upload(t, "audio")
		e.makeLocal(t, f, store.LocalTrack{Title: "Fini", Artist: "Moi"})
		dst := filepath.Join(e.music, "Moi", "Titres sans album", "Fini.mp3")
		if err := e.st.BeginPlacing(ctx, f.ID, dst, f.Path); err != nil {
			t.Fatal(err)
		}
		// The copy got its name but the source was not removed yet.
		os.MkdirAll(filepath.Dir(dst), 0o755)
		os.WriteFile(dst, []byte("audio"), 0o644)
		if err := e.org.RepairPlacing(ctx); err != nil {
			t.Fatal(err)
		}
		row, _ := e.st.GetFile(ctx, f.ID)
		if row.Path != dst || row.PlacingFrom != "" || row.Status != store.FileLocal {
			t.Fatalf("row = %+v", row)
		}
		if _, err := os.Stat(f.Path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("source left: %v", err)
		}
	})

	t.Run("another file of the same size at the destination", func(t *testing.T) {
		e := newOrgEnv(t, nil)
		f := e.upload(t, "audio")
		e.makeLocal(t, f, store.LocalTrack{Title: "Autre", Artist: "Moi"})
		dst := filepath.Join(e.music, "Moi", "Titres sans album", "Autre.mp3")
		if err := e.st.BeginPlacing(ctx, f.ID, dst, f.Path); err != nil {
			t.Fatal(err)
		}
		// Someone else's file took the name (the link was refused), then
		// the process stopped before the row was restored.
		os.MkdirAll(filepath.Dir(dst), 0o755)
		os.WriteFile(dst, []byte("other"), 0o644)
		if err := e.org.RepairPlacing(ctx); err != nil {
			t.Fatal(err)
		}
		row, _ := e.st.GetFile(ctx, f.ID)
		if row.Path != f.Path || row.PlacingFrom != "" {
			t.Fatalf("row = %q from %q, want back on %q", row.Path, row.PlacingFrom, f.Path)
		}
		if readFile(t, f.Path) != "audio" || readFile(t, dst) != "other" {
			t.Error("a file was lost or changed")
		}
	})

	t.Run("destination is another name of the source", func(t *testing.T) {
		e := newOrgEnv(t, nil)
		f := e.upload(t, "audio")
		e.makeLocal(t, f, store.LocalTrack{Title: "Lien", Artist: "Moi"})
		dst := filepath.Join(e.music, "Moi", "Titres sans album", "Lien.mp3")
		if err := e.st.BeginPlacing(ctx, f.ID, dst, f.Path); err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.Link(f.Path, dst); err != nil {
			t.Skipf("hard links not supported here: %v", err)
		}
		if err := e.org.RepairPlacing(ctx); err != nil {
			t.Fatal(err)
		}
		if row, _ := e.st.GetFile(ctx, f.ID); row.Path != dst || row.PlacingFrom != "" {
			t.Fatalf("row = %+v", row)
		}
		if _, err := os.Stat(f.Path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("old name left: %v", err)
		}
		if readFile(t, dst) != "audio" {
			t.Error("content lost")
		}
	})
}

func TestSameContent(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(s), 0o644)
		return p
	}
	big := strings.Repeat("x", 200<<10)
	a, b := write("a", big), write("b", big)
	c := write("c", big[:len(big)-1]+"y")
	d := write("d", big[:100])
	for _, tc := range []struct {
		x, y string
		want bool
	}{{a, b, true}, {a, c, false}, {a, d, false}} {
		if got, err := sameContent(tc.x, tc.y); err != nil || got != tc.want {
			t.Errorf("sameContent(%s, %s) = %v, %v", filepath.Base(tc.x), filepath.Base(tc.y), got, err)
		}
	}
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), ".musichub-") {
			t.Errorf("temporary file left: %s", en.Name())
		}
	}
}

func TestScanDoesNotDuplicateOrganizedUpload(t *testing.T) {
	tools := realTools(t)
	e := newOrgEnv(t, tools)
	ctx := context.Background()
	scanner := NewScanner(e.st, tools, e.music, time.Now)
	scanner.IncomingDir = e.incoming

	src := mediatest.Sine(t, e.incoming, "00112233445566778899aabbccddeeff.mp3", 1, nil)
	info, _ := os.Stat(src)
	f, err := e.st.InsertUploadedFile(ctx, store.File{Path: src, Size: info.Size(), AddedAt: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	e.makeLocal(t, f, store.LocalTrack{Title: "Envoi", Artist: "Moi", Album: "Test", Position: 1})

	// Identified (here local) but not moved yet: a scan leaves it alone.
	if _, err := scanner.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if row, _ := e.st.GetFile(ctx, f.ID); row.Status != store.FileLocal {
		t.Fatalf("incoming file flagged: %s", row.Status)
	}

	// A scan runs right when the file lands in the music folder.
	scanning := true
	fsHooks(t, func(from, to string) error {
		if err := os.Link(from, to); err != nil {
			return err
		}
		if scanning {
			if _, err := scanner.Scan(ctx); err != nil {
				t.Errorf("scan during move: %v", err)
			}
		}
		return nil
	}, nil)
	placed := e.place(t, f.ID)
	scanning = false
	if _, err := scanner.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	all, err := e.st.ListFiles(ctx, store.FileFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("rows = %d, want 1: %+v", len(all), all)
	}
	got := all[0]
	if got.ID != f.ID || got.Origin != store.OriginUploaded || got.Status != store.FileLocal || got.Path != placed.Path ||
		!strings.HasPrefix(got.Path, e.music) {
		t.Errorf("row = %+v", got)
	}
}

func TestPlaceStrandedMovesUploadsLeftInIncoming(t *testing.T) {
	e := newOrgEnv(t, nil)
	pending := e.upload(t, "wait")
	stranded := e.upload(t, "go")
	e.makeLocal(t, stranded, store.LocalTrack{Title: "Parti", Artist: "Moi"})
	if n := e.org.PlaceStranded(context.Background(), e.incoming); n != 1 {
		t.Errorf("moved = %d, want 1", n)
	}
	if row, _ := e.st.GetFile(context.Background(), stranded.ID); !strings.HasPrefix(row.Path, e.music) {
		t.Errorf("stranded file still at %q", row.Path)
	}
	if row, _ := e.st.GetFile(context.Background(), pending.ID); row.Path != pending.Path || !bytes.Equal([]byte(readFile(t, row.Path)), []byte("wait")) {
		t.Errorf("pending file moved: %q", row.Path)
	}
}

type fakeRecordings map[string]musicbrainz.RecordingHit

func (f fakeRecordings) GetRecording(_ context.Context, id string) (musicbrainz.RecordingHit, error) {
	if h, ok := f[id]; ok {
		return h, nil
	}
	return musicbrainz.RecordingHit{}, musicbrainz.ErrNotFound
}

func TestOrganizeNamesIdentifiedFilesAfterTheRecording(t *testing.T) {
	w := &recordingWriter{}
	e := newOrgEnv(t, w)
	ctx := context.Background()
	const rec, album = "00000000-0000-4000-8000-0000000000d9", "00000000-0000-4000-8000-0000000000b9"
	e.org.Recordings = fakeRecordings{rec: {RecordingID: rec, Title: "Vrai Titre", ArtistName: "Vraie Artiste"}}
	// The album's track list (one release) does not hold the recording.
	if err := e.st.UpsertAlbum(ctx, store.Album{ID: album, Title: "Le Disque", ArtistName: "Vraie Artiste", FirstReleaseDate: "2010"}); err != nil {
		t.Fatal(err)
	}
	f := e.upload(t, "x")
	if err := e.st.LinkFile(ctx, f.ID, rec, album, 1, 4); err != nil {
		t.Fatal(err)
	}
	got := e.place(t, f.ID)
	if want := filepath.Join(e.music, "Vraie Artiste", "Le Disque (2010)", "04 - Vrai Titre.mp3"); got.Path != want {
		t.Errorf("path = %q, want %q", got.Path, want)
	}
	if tg := w.calls[got.Path]; tg.Title != "Vrai Titre" || tg.Artist != "Vraie Artiste" || tg.MBRecordingID != rec {
		t.Errorf("tags = %+v", tg)
	}

	// MusicBrainz unknown or unreachable: the file's own tags name it.
	g := e.upload(t, "y")
	e.st.UpsertScannedFile(ctx, store.File{Path: g.Path, Tags: media.Tags{Title: "Du Tag", Artist: "Tagueur"}, LastSeen: time.Now()})
	if err := e.st.LinkFile(ctx, g.ID, "00000000-0000-4000-8000-0000000000d8", "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if p := e.place(t, g.ID).Path; p != filepath.Join(e.music, "Tagueur", "Titres sans album", "Du Tag.mp3") {
		t.Errorf("fallback path = %q", p)
	}
}

type failingRecordings struct{ t *testing.T }

func (f failingRecordings) GetRecording(context.Context, string) (musicbrainz.RecordingHit, error) {
	f.t.Error("recording looked up again")
	return musicbrainz.RecordingHit{}, musicbrainz.ErrUnavailable
}

func TestOrganizeReusesTheFetchedRecording(t *testing.T) {
	e := newOrgEnv(t, nil)
	ctx := context.Background()
	const rec = "00000000-0000-4000-8000-0000000000d7"
	e.org.Recordings = failingRecordings{t}
	f := e.upload(t, "x")
	if err := e.st.LinkFile(ctx, f.ID, rec, "", 0, 0); err != nil {
		t.Fatal(err)
	}
	got, err := e.org.Place(musicbrainz.WithRecording(ctx, musicbrainz.RecordingHit{RecordingID: rec, Title: "Connu", ArtistName: "Déjà"}), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(e.music, "Déjà", "Titres sans album", "Connu.mp3"); got.Path != want {
		t.Errorf("path = %q, want %q", got.Path, want)
	}
}

func TestRemoveTempsKeepsOtherFilesAndStops(t *testing.T) {
	e := newOrgEnv(t, nil)
	dir := filepath.Join(e.music, "A", "B")
	os.MkdirAll(dir, 0o755)
	for _, n := range []string{".musichub-move-1.mp3", ".musichub-tags-2.flac", "song.mp3", ".other"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.org.RemoveTemps(cancelled); err == nil {
		t.Error("a cancelled clean-up must report it")
	}
	n, err := e.org.RemoveTemps(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("RemoveTemps = %d, %v; want 2", n, err)
	}
	assertNoTemp(t, dir)
	for _, keep := range []string{"song.mp3", ".other"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s removed: %v", keep, err)
		}
	}
}
