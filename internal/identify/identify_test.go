package identify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/acoustid"
	"github.com/Wuhn14/MusicHub/internal/acoustid/acoustidtest"
	"github.com/Wuhn14/MusicHub/internal/catalog"
	"github.com/Wuhn14/MusicHub/internal/catalog/mbtest"
	"github.com/Wuhn14/MusicHub/internal/catalog/musicbrainz"
	"github.com/Wuhn14/MusicHub/internal/media"
	"github.com/Wuhn14/MusicHub/internal/media/mediatest"
	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
)

// Invented identifiers of the mbtest fixtures.
const (
	recGlassTide = mbtest.RecordingGlassTide // disc 2, track 3 of albumLanterns
	albumLantern = mbtest.AlbumTwoDisc
	recLive      = "00000000-0000-4000-8000-0000000000d2"
)

// clock is a settable fake clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type fakeSettings struct{ s settings.Settings }

func (f *fakeSettings) Get(context.Context) (settings.Settings, error) { return f.s, nil }

// fakeFP returns a fixed fingerprint, as fpcalc would for a known song.
type fakeFP struct {
	mu    sync.Mutex
	fp    string
	dur   int
	calls int
}

func (f *fakeFP) Fingerprint(context.Context, string) (string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.fp, f.dur, nil
}

// fakeAcoustID returns fixed matches.
type fakeAcoustID struct {
	matches []acoustid.Match
	err     error
}

func (f *fakeAcoustID) Lookup(context.Context, string, string, int) ([]acoustid.Match, error) {
	return f.matches, f.err
}

// fakeFinder answers searches with fixed hits (or an error) and delegates
// lookups to inner.
type fakeFinder struct {
	inner RecordingFinder
	hits  []musicbrainz.RecordingHit
	err   error
	// onSearch, when set, runs inside each search (to simulate a concurrent
	// change while a remote call is in flight).
	onSearch func()
	mu       sync.Mutex
	searches int
}

func (f *fakeFinder) FindRecordings(context.Context, musicbrainz.RecordingQuery, int) ([]musicbrainz.RecordingHit, error) {
	f.mu.Lock()
	f.searches++
	f.mu.Unlock()
	if f.onSearch != nil {
		f.onSearch()
	}
	return f.hits, f.err
}

func (f *fakeFinder) GetRecording(ctx context.Context, id string) (musicbrainz.RecordingHit, error) {
	return f.inner.GetRecording(ctx, id)
}

func (f *fakeFinder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.searches
}

type env struct {
	st    *store.Store
	mb    *mbtest.Server
	ac    *acoustidtest.Server
	mbc   *musicbrainz.Client
	set   *fakeSettings
	fp    *fakeFP
	clock *clock
	deps  Deps
	dir   string
	n     int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return newEnvAt(t, filepath.Join(t.TempDir(), "test.db"))
}

func newEnvAt(t *testing.T, dbPath string) *env {
	t.Helper()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	mb := mbtest.New()
	t.Cleanup(mb.Close)
	ac := acoustidtest.New()
	t.Cleanup(ac.Close)
	clk := &clock{t: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)}
	mbc := musicbrainz.New(mb.URL, "MusicHub-test/0", 0)
	e := &env{
		st: st, mb: mb, ac: ac, mbc: mbc, clock: clk,
		set: &fakeSettings{s: settings.Defaults()},
		fp:  &fakeFP{fp: acoustidtest.FingerprintGlassTide, dur: 201},
		dir: t.TempDir(),
	}
	e.deps = Deps{
		Store: st, Tools: e.fp, AcoustID: acoustid.New(ac.URL+"/v2", 0), MB: mbc,
		Catalog: catalog.New(st, mbc, clk.Now), Settings: e.set, Now: clk.Now,
	}
	return e
}

// addFile records a pending file. Without a path, an empty placeholder is
// created in the temporary folder.
func (e *env) addFile(t *testing.T, f store.File) store.File {
	t.Helper()
	if f.Path == "" {
		e.n++
		f.Path = filepath.Join(e.dir, fmt.Sprintf("f%d.mp3", e.n))
		if err := os.WriteFile(f.Path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f.LastSeen = e.clock.Now()
	f.Status = store.FilePending
	out, err := e.st.UpsertScannedFile(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *env) identify(t *testing.T, fileID string) Result {
	t.Helper()
	res, err := New(e.deps).Identify(context.Background(), fileID)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	return res
}

func (e *env) file(t *testing.T, id string) store.File {
	t.Helper()
	f, err := e.st.GetFile(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func glassTideTags() media.Tags {
	return media.Tags{Artist: "Lumen Harbor", Title: "Glass Tide", Album: "Paper Lanterns", Track: 9, Disc: 1}
}

func TestIdentifyByTagID(t *testing.T) {
	e := newEnv(t)
	e.set.s.AcoustIDKey = "test-key"
	f := e.addFile(t, store.File{DurationMs: 30000, Tags: media.Tags{MBRecordingID: recGlassTide, Track: 9, Disc: 1}})

	res := e.identify(t, f.ID)
	if res.Status != store.FileIdentified || res.Source != store.SourceTag || res.RecordingID != recGlassTide || res.AlbumID != albumLantern {
		t.Fatalf("result = %+v", res)
	}
	g := e.file(t, f.ID)
	// Disc and position come from the release, not from the tags.
	if g.Status != store.FileIdentified || g.RecordingID != recGlassTide || g.AlbumID != albumLantern || g.Disc != 2 || g.Position != 3 {
		t.Errorf("file = %+v", g)
	}
	if e.ac.Requests() != 0 || e.fp.calls != 0 {
		t.Errorf("AcoustID used (%d requests, %d fingerprints) although the tags had the id", e.ac.Requests(), e.fp.calls)
	}
	// The album is now in the catalog cache, with its tracks.
	al, err := e.st.GetAlbum(context.Background(), albumLantern)
	if err != nil || len(al.Tracks) == 0 {
		t.Errorf("cached album = %+v, %v", al, err)
	}
}

func TestIdentifyByAcoustID(t *testing.T) {
	e := newEnv(t)
	e.set.s.AcoustIDKey = "test-key"
	// Tags too poor for a search: only AcoustID can find it.
	f := e.addFile(t, store.File{DurationMs: 204000, Tags: media.Tags{Title: "piste 1"}})

	res := e.identify(t, f.ID)
	if res.Status != store.FileIdentified || res.Source != store.SourceAcoustID || res.RecordingID != recGlassTide || res.AlbumID != albumLantern {
		t.Fatalf("result = %+v", res)
	}
	if last := e.ac.Last(); last.Form.Get("fingerprint") != acoustidtest.FingerprintGlassTide || last.Form.Get("duration") != "201" {
		t.Errorf("AcoustID request = %+v", last.Form)
	}

	// Same fingerprint, but the file is 9 s longer than the recording: no
	// automatic match, the AcoustID results become candidates.
	g := e.addFile(t, store.File{DurationMs: 210000, Tags: media.Tags{Title: "piste 2"}})
	res = e.identify(t, g.ID)
	if res.Status != store.FilePending || res.Candidates != 2 {
		t.Fatalf("result with a duration gap = %+v", res)
	}
	cands, _ := e.st.ListCandidates(context.Background(), g.ID)
	if len(cands) != 2 || cands[0].RecordingID != recGlassTide || cands[0].Score != 97 || cands[0].Source != store.SourceAcoustID ||
		cands[0].AlbumID != albumLantern || cands[0].Album != "Paper Lanterns" || cands[1].Score != 55 {
		t.Errorf("candidates = %+v", cands)
	}
}

func TestAcoustIDSkippedWithoutKey(t *testing.T) {
	e := newEnv(t)
	f := e.addFile(t, store.File{DurationMs: 202000, Tags: glassTideTags()})
	res := e.identify(t, f.ID)
	if res.Status != store.FileIdentified || res.Source != store.SourceSearch {
		t.Fatalf("result = %+v", res)
	}
	if e.ac.Requests() != 0 || e.fp.calls != 0 {
		t.Errorf("AcoustID used without a key: %d requests, %d fingerprints", e.ac.Requests(), e.fp.calls)
	}

	// A refused key skips the step and the search still runs.
	e.set.s.AcoustIDKey = acoustidtest.BadKey
	g := e.addFile(t, store.File{DurationMs: 202000, Tags: glassTideTags()})
	res = e.identify(t, g.ID)
	if res.Status != store.FileIdentified || res.Source != store.SourceSearch {
		t.Fatalf("result with a refused key = %+v", res)
	}
	if e.ac.Requests() != 1 {
		t.Errorf("AcoustID requests = %d, want 1", e.ac.Requests())
	}
}

func TestIdentifyBySearchOnlyWhenUnambiguous(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	// The fake answers Glass Tide (score 100, 3:21) then a live version (62).
	t.Run("clear winner", func(t *testing.T) {
		f := e.addFile(t, store.File{DurationMs: 203500, Tags: glassTideTags()})
		res := e.identify(t, f.ID)
		if res.Status != store.FileIdentified || res.Source != store.SourceSearch || res.RecordingID != recGlassTide || res.AlbumID != albumLantern {
			t.Fatalf("result = %+v", res)
		}
	})
	t.Run("duration off by more than 3 s", func(t *testing.T) {
		f := e.addFile(t, store.File{DurationMs: 204500, Tags: glassTideTags()})
		if res := e.identify(t, f.ID); res.Status != store.FilePending || res.Candidates != 2 {
			t.Fatalf("result = %+v", res)
		}
		cands, _ := e.st.ListCandidates(ctx, f.ID)
		if len(cands) != 2 || cands[0].RecordingID != recGlassTide || cands[0].Source != store.SourceSearch ||
			cands[0].Artist == "" || cands[1].RecordingID != recLive || cands[1].Score != 62 {
			t.Errorf("candidates = %+v", cands)
		}
	})
	t.Run("unknown file duration", func(t *testing.T) {
		f := e.addFile(t, store.File{Tags: glassTideTags()})
		if res := e.identify(t, f.ID); res.Status != store.FilePending {
			t.Fatalf("result = %+v", res)
		}
	})

	hit := func(id string, score int) musicbrainz.RecordingHit {
		return musicbrainz.RecordingHit{RecordingID: id, Title: "Glass Tide", ArtistName: "Lumen Harbor", LengthMs: 201000, Score: score,
			Releases: []musicbrainz.ReleaseRef{{ReleaseGroupID: albumLantern, Title: "Paper Lanterns", PrimaryType: "Album", Date: "2003"}}}
	}
	for _, tc := range []struct {
		name string
		hits []musicbrainz.RecordingHit
		want string
	}{
		{"second hit too close", []musicbrainz.RecordingHit{hit(recGlassTide, 98), hit(recLive, 90)}, store.FilePending},
		{"top score below 95", []musicbrainz.RecordingHit{hit(recGlassTide, 94)}, store.FilePending},
		{"single hit", []musicbrainz.RecordingHit{hit(recGlassTide, 96)}, store.FileIdentified},
		{"lead of exactly 10", []musicbrainz.RecordingHit{hit(recGlassTide, 99), hit(recLive, 89)}, store.FileIdentified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.deps.MB = &fakeFinder{inner: e.mbc, hits: tc.hits}
			f := e.addFile(t, store.File{DurationMs: 201000, Tags: glassTideTags()})
			if res := e.identify(t, f.ID); res.Status != tc.want {
				t.Fatalf("result = %+v, want status %s", res, tc.want)
			}
		})
	}
}

func TestPendingKeepsFiveCandidates(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.set.s.AcoustIDKey = "test-key"
	rg := func(id, title string) []acoustid.RGRef {
		return []acoustid.RGRef{{ID: id, Title: title, Type: "Album"}}
	}
	e.deps.AcoustID = &fakeAcoustID{matches: []acoustid.Match{
		{Score: 0.85, RecordingID: "r1", Title: "One", DurationSec: 200, ReleaseGroups: rg("g1", "Album One")},
		{Score: 0.60, RecordingID: "r2", Title: "Two", DurationSec: 200},
		{Score: 0.40, RecordingID: "r6", Title: "Six", DurationSec: 200},
	}}
	e.deps.MB = &fakeFinder{inner: e.mbc, hits: []musicbrainz.RecordingHit{
		{RecordingID: "r3", Title: "Three", ArtistName: "C", LengthMs: 200000, Score: 90},
		{RecordingID: "r4", Title: "Four", ArtistName: "D", LengthMs: 200000, Score: 80},
		{RecordingID: "r1", Title: "One", ArtistName: "A", LengthMs: 200000, Score: 70},
		{RecordingID: "r5", Title: "Five", ArtistName: "E", LengthMs: 200000, Score: 50},
	}}
	f := e.addFile(t, store.File{DurationMs: 200000, Tags: glassTideTags()})
	res := e.identify(t, f.ID)
	if res.Status != store.FilePending || res.Candidates != 5 {
		t.Fatalf("result = %+v", res)
	}
	cands, err := e.st.ListCandidates(ctx, f.ID)
	if err != nil || len(cands) != 5 {
		t.Fatalf("candidates = %+v, %v", cands, err)
	}
	wantIDs := []string{"r3", "r1", "r4", "r2", "r5"}
	wantScores := []int{90, 85, 80, 60, 50}
	for i, c := range cands {
		if c.RecordingID != wantIDs[i] || c.Score != wantScores[i] || c.Rank != i+1 {
			t.Errorf("candidate %d = %+v, want %s (%d)", i, c, wantIDs[i], wantScores[i])
		}
	}
	// r1 keeps its AcoustID score and source, with the artist from the search.
	if c := cands[1]; c.Source != store.SourceAcoustID || c.Artist != "A" || c.AlbumID != "g1" || c.Album != "Album One" {
		t.Errorf("merged candidate = %+v", c)
	}
	if g := e.file(t, f.ID); g.Status != store.FilePending || g.RecordingID != "" {
		t.Errorf("file = %+v", g)
	}
}

func TestAlbumChoice(t *testing.T) {
	rel := func(rg, title, date string, secondary ...string) musicbrainz.ReleaseRef {
		return musicbrainz.ReleaseRef{ReleaseGroupID: rg, Title: title, PrimaryType: "Album", SecondaryTypes: secondary, Date: date}
	}
	releases := []musicbrainz.ReleaseRef{
		rel("comp", "Best Of", "1990-01-01", "Compilation"),
		rel("live", "Live in Town", "1995-01-01", "Live"),
		{ReleaseGroupID: "studio2", Title: "Été Brûlant", PrimaryType: "Album", Date: "2001-06-01", Disc: 1, Position: 4},
		{ReleaseGroupID: "studio2", Title: "Été Brûlant", PrimaryType: "Album", Date: "2000-03-01", Disc: 2, Position: 7},
		rel("studio1", "First Light", "1998-02-02"),
	}
	cases := []struct {
		name      string
		releases  []musicbrainz.ReleaseRef
		acoust    []acoustid.RGRef
		tagRG     string
		tagAlbum  string
		wantID    string
		wantDisc  int
		wantTrack int
	}{
		{name: "release group from the tags", releases: releases, tagRG: "live", tagAlbum: "First Light", wantID: "live"},
		{name: "tag release group without the recording is ignored", releases: releases, tagRG: "elsewhere", wantID: "studio1"},
		{name: "release group from AcoustID", releases: releases, acoust: []acoustid.RGRef{{ID: "comp"}, {ID: "unknown"}}, tagAlbum: "First Light", wantID: "comp"},
		{name: "several AcoustID groups narrow the choice", releases: releases, acoust: []acoustid.RGRef{{ID: "comp"}, {ID: "live"}}, wantID: "comp"},
		{name: "title equal to the album tag", releases: releases, tagAlbum: "  ete BRULANT ", wantID: "studio2", wantDisc: 2, wantTrack: 7},
		{name: "oldest studio album", releases: releases, tagAlbum: "Something Else", wantID: "studio1"},
		{name: "oldest when all have a secondary type", releases: releases[:2], wantID: "comp"},
		{name: "AcoustID groups only", acoust: []acoustid.RGRef{{ID: "x", Title: "X", SecondaryTypes: []string{"Live"}}, {ID: "y", Title: "Y"}}, wantID: "y"},
		{name: "nothing known", wantID: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseAlbum(tc.releases, tc.acoust, tc.tagRG, tc.tagAlbum)
			if got.id != tc.wantID || got.disc != tc.wantDisc || got.position != tc.wantTrack {
				t.Errorf("chooseAlbum = %+v, want %s %d/%d", got, tc.wantID, tc.wantDisc, tc.wantTrack)
			}
		})
	}
}

// addAudio generates a short sine file and records it as a file of the given
// origin and quality, already identified as Glass Tide when linked is true.
func (e *env) addAudio(t *testing.T, name, origin string, lossless bool, bitrate int, linked bool) store.File {
	t.Helper()
	path := mediatest.Sine(t, e.dir, name, 1, nil)
	tags := media.Tags{MBRecordingID: recGlassTide}
	f := e.addFile(t, store.File{Path: path, Origin: origin, Lossless: lossless, BitRate: bitrate, Size: 1000, DurationMs: 1000, Tags: tags})
	if linked {
		if err := e.st.LinkFile(context.Background(), f.ID, recGlassTide, albumLantern, 2, 3); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestDuplicatesBest(t *testing.T) {
	mediatest.RequireTool(t, "ffmpeg")

	t.Run("uploaded FLAC against dropped MP3", func(t *testing.T) {
		e := newEnv(t)
		mp3 := e.addAudio(t, "dropped.mp3", store.OriginDropped, false, 320000, true)
		flac := e.addAudio(t, "uploaded.flac", store.OriginUploaded, true, 900000, false)
		res := e.identify(t, flac.ID)
		if res.Status != store.FileIdentified || res.Duplicate != DupHiddenExisting {
			t.Fatalf("result = %+v", res)
		}
		if g := e.file(t, mp3.ID); g.Status != store.FileHidden || !exists(mp3.Path) {
			t.Errorf("dropped MP3 = %s, on disk %v; want hidden and never deleted", g.Status, exists(mp3.Path))
		}
		if g := e.file(t, flac.ID); g.Status != store.FileIdentified {
			t.Errorf("FLAC status = %s", g.Status)
		}
	})
	t.Run("dropped MP3 arriving after an uploaded FLAC", func(t *testing.T) {
		e := newEnv(t)
		flac := e.addAudio(t, "uploaded.flac", store.OriginUploaded, true, 900000, true)
		mp3 := e.addAudio(t, "dropped.mp3", store.OriginDropped, false, 320000, false)
		res := e.identify(t, mp3.ID)
		if res.Duplicate != DupHiddenNew {
			t.Fatalf("result = %+v", res)
		}
		if g := e.file(t, mp3.ID); g.Status != store.FileHidden || !exists(mp3.Path) {
			t.Errorf("dropped MP3 = %s, on disk %v", g.Status, exists(mp3.Path))
		}
		if g := e.file(t, flac.ID); g.Status != store.FileIdentified || !exists(flac.Path) {
			t.Errorf("FLAC = %s, on disk %v", g.Status, exists(flac.Path))
		}
	})
	t.Run("uploaded loser is deleted", func(t *testing.T) {
		e := newEnv(t)
		low := e.addAudio(t, "low.mp3", store.OriginUploaded, false, 128000, true)
		high := e.addAudio(t, "high.mp3", store.OriginDropped, false, 320000, false)
		res := e.identify(t, high.ID)
		if res.Duplicate != DupReplacedExisting {
			t.Fatalf("result = %+v", res)
		}
		if _, err := e.st.GetFile(context.Background(), low.ID); !errors.Is(err, store.ErrNotFound) || exists(low.Path) {
			t.Errorf("uploaded loser: row err %v, on disk %v; want both gone", err, exists(low.Path))
		}
	})
	t.Run("tie keeps the existing file", func(t *testing.T) {
		e := newEnv(t)
		old := e.addAudio(t, "old.mp3", store.OriginDropped, false, 320000, true)
		dup := e.addAudio(t, "new.mp3", store.OriginDropped, false, 320000, false)
		if res := e.identify(t, dup.ID); res.Duplicate != DupHiddenNew {
			t.Fatalf("result = %+v", res)
		}
		if g := e.file(t, old.ID); g.Status != store.FileIdentified {
			t.Errorf("existing = %s", g.Status)
		}
	})
}

func TestDuplicatesKeepExisting(t *testing.T) {
	mediatest.RequireTool(t, "ffmpeg")

	t.Run("keep-existing", func(t *testing.T) {
		e := newEnv(t)
		e.set.s.Duplicates = settings.DuplicatesKeepExisting
		mp3 := e.addAudio(t, "dropped.mp3", store.OriginDropped, false, 128000, true)
		flac := e.addAudio(t, "uploaded.flac", store.OriginUploaded, true, 900000, false)
		res := e.identify(t, flac.ID)
		if res.Duplicate != DupDiscardedNew {
			t.Fatalf("result = %+v", res)
		}
		if _, err := e.st.GetFile(context.Background(), flac.ID); !errors.Is(err, store.ErrNotFound) || exists(flac.Path) {
			t.Errorf("uploaded newcomer: row err %v, on disk %v; want both gone", err, exists(flac.Path))
		}
		if g := e.file(t, mp3.ID); g.Status != store.FileIdentified {
			t.Errorf("existing = %s", g.Status)
		}
	})
	t.Run("keep-both", func(t *testing.T) {
		e := newEnv(t)
		e.set.s.Duplicates = settings.DuplicatesKeepBoth
		mp3 := e.addAudio(t, "dropped.mp3", store.OriginDropped, false, 128000, true)
		flac := e.addAudio(t, "uploaded.flac", store.OriginUploaded, true, 900000, false)
		if res := e.identify(t, flac.ID); res.Duplicate != "" {
			t.Fatalf("result = %+v", res)
		}
		for _, id := range []string{mp3.ID, flac.ID} {
			if g := e.file(t, id); g.Status != store.FileIdentified {
				t.Errorf("%s status = %s", g.Path, g.Status)
			}
		}
	})
}

func TestWorkerRetriesThenPending(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.set.s.AcoustIDKey = "test-key"
	// AcoustID answers (one weak match) but the search is unreachable.
	e.deps.AcoustID = &fakeAcoustID{matches: []acoustid.Match{{Score: 0.5, RecordingID: "r1", Title: "One", DurationSec: 200}}}
	finder := &fakeFinder{inner: e.mbc, err: fmt.Errorf("search: %w", musicbrainz.ErrUnavailable)}
	e.deps.MB = finder
	w := NewWorker(New(e.deps), e.st)
	f := e.addFile(t, store.File{DurationMs: 200000, Tags: glassTideTags()})
	w.Enqueue(f.ID)
	w.Enqueue(f.ID) // idempotent

	if s := w.Status(); s.Queued != 1 {
		t.Fatalf("status = %+v, want 1 queued", s)
	}
	for i, delay := range retryDelays {
		w.processDue(ctx)
		if finder.count() != i+1 {
			t.Fatalf("searches after attempt %d = %d", i+1, finder.count())
		}
		j, err := e.st.GetJob(ctx, JobKind, f.ID)
		if err != nil || j.Attempts != i+1 || !j.RunAfter.Equal(e.clock.Now().Add(delay)) {
			t.Fatalf("job after attempt %d = %+v, %v; want retry in %s", i+1, j, err, delay)
		}
		// Nothing runs before the delay.
		e.clock.Advance(delay - time.Second)
		w.processDue(ctx)
		if finder.count() != i+1 {
			t.Fatalf("job ran before its delay (attempt %d)", i+1)
		}
		e.clock.Advance(time.Second)
	}
	w.processDue(ctx)
	if finder.count() != len(retryDelays)+1 {
		t.Fatalf("searches = %d, want %d", finder.count(), len(retryDelays)+1)
	}
	if _, err := e.st.GetJob(ctx, JobKind, f.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("job still queued after the last failure: %v", err)
	}
	if g := e.file(t, f.ID); g.Status != store.FilePending {
		t.Errorf("file status = %s, want pending", g.Status)
	}
	// The candidates found before the failure are kept.
	if cands, _ := e.st.ListCandidates(ctx, f.ID); len(cands) != 1 || cands[0].RecordingID != "r1" {
		t.Errorf("candidates = %+v", cands)
	}
	if s := w.Status(); s.Queued != 0 || s.Failed != 1 || s.Done != 0 {
		t.Errorf("status = %+v", s)
	}
}

func TestJobsSurviveRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "musichub.db")
	ctx := context.Background()

	// First run: the files are queued, then the process stops before the
	// worker gets to them.
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	queued, _ := st.UpsertScannedFile(ctx, store.File{Path: "/m/queued.mp3", Status: store.FilePending,
		Tags: media.Tags{MBRecordingID: recGlassTide}})
	backlog, _ := st.UpsertScannedFile(ctx, store.File{Path: "/m/backlog.mp3", Status: store.FilePending,
		Tags: media.Tags{MBRecordingID: recGlassTide}})
	local, _ := st.UpsertScannedFile(ctx, store.File{Path: "/m/local.mp3", Status: store.FilePending,
		Tags: media.Tags{MBRecordingID: recGlassTide}})
	st.SetFileStatus(ctx, local.ID, store.FileLocal)
	earlier := func() time.Time { return time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC) }
	NewWorker(New(Deps{Store: st, Now: earlier}), st).Enqueue(queued.ID)
	st.Close()

	// Second run: the queued job and the never-queued pending file are both
	// identified; the local file is left alone.
	e := newEnvAt(t, dbPath)
	e.set.s.Duplicates = settings.DuplicatesKeepBoth
	w := NewWorker(New(e.deps), e.st)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); w.Run(runCtx) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		a, b := e.file(t, queued.ID), e.file(t, backlog.ID)
		if a.Status == store.FileIdentified && b.Status == store.FileIdentified {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not identified after restart: %s, %s", a.Status, b.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if g := e.file(t, local.ID); g.Status != store.FileLocal || g.RecordingID != "" {
		t.Errorf("local file = %+v", g)
	}
	if s := w.Status(); s.Queued != 0 || s.Done != 2 || s.Failed != 0 {
		t.Errorf("status = %+v", s)
	}
}

func TestSkipsNonPendingFiles(t *testing.T) {
	e := newEnv(t)
	for _, status := range []string{store.FileLocal, store.FileHidden, store.FileMissing, store.FileUnreadable, store.FileIdentified} {
		f := e.addFile(t, store.File{Tags: media.Tags{MBRecordingID: recGlassTide}})
		e.st.SetFileStatus(context.Background(), f.ID, status)
		if _, err := New(e.deps).Identify(context.Background(), f.ID); !errors.Is(err, ErrSkipped) {
			t.Errorf("%s: err = %v, want ErrSkipped", status, err)
		}
	}
	if n := e.mb.Requests(); n != 0 {
		t.Errorf("MusicBrainz requests = %d, want 0", n)
	}
}

func TestStatusChangeDuringIdentification(t *testing.T) {
	ctx := context.Background()
	confident := []musicbrainz.RecordingHit{{RecordingID: recGlassTide, Title: "Glass Tide", ArtistName: "Lumen Harbor",
		LengthMs: 201000, Score: 100, Releases: []musicbrainz.ReleaseRef{{ReleaseGroupID: albumLantern, Title: "Paper Lanterns", Date: "2003", Disc: 2, Position: 3}}}}

	for _, tc := range []struct {
		name, status string
		hits         []musicbrainz.RecordingHit
	}{
		{"missing before the link", store.FileMissing, confident},
		{"hidden before the candidates", store.FileHidden, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			var f store.File
			e.deps.MB = &fakeFinder{inner: e.mbc, hits: tc.hits, onSearch: func() {
				// The scanner (or the user) changes the file while MusicBrainz answers.
				if err := e.st.SetFileStatus(ctx, f.ID, tc.status); err != nil {
					t.Error(err)
				}
			}}
			f = e.addFile(t, store.File{DurationMs: 201000, Tags: glassTideTags()})
			if _, err := New(e.deps).Identify(ctx, f.ID); !errors.Is(err, ErrSkipped) {
				t.Fatalf("Identify err = %v, want ErrSkipped", err)
			}
			g := e.file(t, f.ID)
			if g.Status != tc.status || g.RecordingID != "" || g.AlbumID != "" {
				t.Errorf("file = %s %q %q, want untouched %s", g.Status, g.RecordingID, g.AlbumID, tc.status)
			}
			if cands, _ := e.st.ListCandidates(ctx, f.ID); len(cands) != 0 {
				t.Errorf("candidates written for a non-pending file: %+v", cands)
			}
		})
	}
}

func TestDuplicatesIgnoreStaleFiles(t *testing.T) {
	mediatest.RequireTool(t, "ffmpeg")
	ctx := context.Background()
	e := newEnv(t)
	// The existing uploaded copy vanished from the disk behind our back: it
	// must not be "replaced" (its row deleted) on the strength of a stale row.
	old := e.addAudio(t, "old.mp3", store.OriginUploaded, false, 128000, true)
	if err := os.Remove(old.Path); err != nil {
		t.Fatal(err)
	}
	flac := e.addAudio(t, "new.flac", store.OriginDropped, true, 900000, false)
	res := e.identify(t, flac.ID)
	if res.Status != store.FileIdentified || res.Duplicate != "" {
		t.Fatalf("result = %+v", res)
	}
	if g, err := e.st.GetFile(ctx, old.ID); err != nil || g.Status != store.FileIdentified {
		t.Errorf("stale existing file = %+v, %v; want left alone", g, err)
	}
}

func TestEnqueueDuringRunIsKept(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	var w *Worker
	var f store.File
	once := sync.Once{}
	finder := &fakeFinder{inner: e.mbc, onSearch: func() {
		// The scanner sees the file change while it is being identified.
		once.Do(func() { w.Enqueue(f.ID) })
	}}
	e.deps.MB = finder
	w = NewWorker(New(e.deps), e.st)
	f = e.addFile(t, store.File{DurationMs: 201000, Tags: glassTideTags()})
	w.Enqueue(f.ID)
	w.processDue(ctx)
	if n := finder.count(); n != 2 {
		t.Fatalf("searches = %d, want 2 (the Enqueue during the run must trigger a second run)", n)
	}
	if s := w.Status(); s.Queued != 0 || s.Done != 2 {
		t.Errorf("status = %+v", s)
	}
}

// fakePlacer records the files it was asked to place.
type fakePlacer struct {
	mu  sync.Mutex
	ids []string
	err error
}

func (p *fakePlacer) Place(ctx context.Context, fileID string) (store.File, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ctx.Err() != nil {
		return store.File{}, ctx.Err()
	}
	p.ids = append(p.ids, fileID)
	return store.File{ID: fileID}, p.err
}

func TestPlacedAfterIdentificationChoiceAndLocal(t *testing.T) {
	e := newEnv(t)
	pl := &fakePlacer{err: errors.New("disk full")}
	e.deps.Placer = pl
	e.set.s.Duplicates = settings.DuplicatesKeepBoth
	ctx := context.Background()

	// Automatic identification: placed, and a placing failure is not an
	// identification failure.
	auto := e.addFile(t, store.File{DurationMs: 30000, Tags: media.Tags{MBRecordingID: recGlassTide}})
	if res := e.identify(t, auto.ID); res.Status != store.FileIdentified {
		t.Fatalf("result = %+v", res)
	}
	// Left pending: not placed.
	pending := e.addFile(t, store.File{DurationMs: 30000})
	e.identify(t, pending.ID)

	// Chosen by the user, with an explicit album.
	chosen := e.addFile(t, store.File{DurationMs: 30000})
	res, err := New(e.deps).Choose(ctx, chosen.ID, recGlassTide, albumLantern)
	if err != nil || res.Status != store.FileIdentified || res.AlbumID != albumLantern || res.Source != SourceManual {
		t.Fatalf("Choose = %+v, %v", res, err)
	}
	if g := e.file(t, chosen.ID); g.Disc != 2 || g.Position != 3 {
		t.Errorf("chosen file = %+v", g)
	}
	if _, err := New(e.deps).Choose(ctx, chosen.ID, recGlassTide, ""); !errors.Is(err, store.ErrNotPending) {
		t.Errorf("second Choose err = %v", err)
	}
	if _, err := New(e.deps).Choose(ctx, pending.ID, "00000000-0000-4000-8000-0000000000ff", ""); !errors.Is(err, musicbrainz.ErrNotFound) {
		t.Errorf("unknown recording err = %v", err)
	}

	// Made local.
	local := e.addFile(t, store.File{})
	lt, f, err := New(e.deps).CreateLocal(ctx, local.ID, store.LocalTrack{Title: "T", Artist: "A"})
	if err != nil || lt.ID == "" || f.Status != store.FileLocal || f.LocalTrackID != lt.ID {
		t.Fatalf("CreateLocal = %+v, %+v, %v", lt, f, err)
	}
	if _, _, err := New(e.deps).CreateLocal(ctx, local.ID, store.LocalTrack{Title: "T", Artist: "A"}); !errors.Is(err, store.ErrNotPending) {
		t.Errorf("second CreateLocal err = %v", err)
	}

	pl.mu.Lock()
	defer pl.mu.Unlock()
	want := []string{auto.ID, chosen.ID, local.ID}
	if fmt.Sprint(pl.ids) != fmt.Sprint(want) {
		t.Errorf("placed = %v, want %v", pl.ids, want)
	}
}

func TestNotPlacedWhenTheNewcomerLoses(t *testing.T) {
	mediatest.RequireTool(t, "ffmpeg")
	e := newEnv(t)
	pl := &fakePlacer{}
	e.deps.Placer = pl
	e.addAudio(t, "old.mp3", store.OriginDropped, false, 320000, true)
	dup := e.addAudio(t, "new.mp3", store.OriginUploaded, false, 128000, false)
	if res := e.identify(t, dup.ID); res.Duplicate != DupDiscardedNew {
		t.Fatalf("result = %+v", res)
	}
	if len(pl.ids) != 0 {
		t.Errorf("discarded file placed: %v", pl.ids)
	}
}

// TestConcurrentDuplicatesKeepOneCopy links two uploaded copies of the same
// recording at the same time: whatever the order, exactly one must survive.
func TestConcurrentDuplicatesKeepOneCopy(t *testing.T) {
	mediatest.RequireTool(t, "ffmpeg")

	for _, policy := range []string{settings.DuplicatesKeepExisting, settings.DuplicatesBest} {
		t.Run(policy, func(t *testing.T) {
			for round := 0; round < 5; round++ {
				e := newEnv(t)
				e.set.s.Duplicates = policy
				// Same quality: under "best" this is a tie.
				a := e.addAudio(t, fmt.Sprintf("a%d.mp3", round), store.OriginUploaded, false, 320000, false)
				b := e.addAudio(t, fmt.Sprintf("b%d.mp3", round), store.OriginUploaded, false, 320000, false)
				idf := New(e.deps)
				var linked sync.WaitGroup
				linked.Add(2)
				// Both files are linked before either resolves duplicates.
				idf.afterLink = func() { linked.Done(); linked.Wait() }
				var wg sync.WaitGroup
				errs := make([]error, 2)
				for i, id := range []string{a.ID, b.ID} {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, errs[i] = idf.Identify(context.Background(), id)
					}()
				}
				wg.Wait()
				for _, err := range errs {
					if err != nil {
						t.Fatalf("Identify: %v", err)
					}
				}
				n := 0
				for _, f := range []store.File{a, b} {
					if exists(f.Path) {
						n++
						if g := e.file(t, f.ID); g.Status != store.FileIdentified {
							t.Errorf("survivor status = %s", g.Status)
						}
					}
				}
				if n != 1 {
					t.Fatalf("round %d: %d copies left on disk, want exactly 1", round, n)
				}
			}
		})
	}
}
