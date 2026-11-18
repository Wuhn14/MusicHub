package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Wuhn14/MusicHub/internal/media/mediatest"
)

func tools(t *testing.T) *Tools {
	t.Helper()
	mediatest.RequireTool(t, "ffmpeg")
	mediatest.RequireTool(t, "ffprobe")
	return New("ffmpeg", "ffprobe", "fpcalc")
}

func TestStatusReportsMissingTool(t *testing.T) {
	mediatest.RequireTool(t, "ffmpeg")
	tl := New("ffmpeg", "definitely-not-a-real-tool", "fpcalc")
	var found bool
	for _, s := range tl.Status(context.Background()) {
		switch s.Name {
		case "ffprobe":
			found = true
			if s.Available || s.Error == "" {
				t.Errorf("ffprobe: want unavailable with error, got %+v", s)
			}
		case "ffmpeg":
			if !s.Available || s.Version == "" || s.Path == "" {
				t.Errorf("ffmpeg: want available with version, got %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("no ffprobe status entry")
	}
}

func TestProbeReadsTagsAndDuration(t *testing.T) {
	tl := tools(t)
	dir := t.TempDir()
	for _, tc := range []struct {
		name     string
		lossless bool
	}{{"a.mp3", false}, {"a.flac", true}} {
		t.Run(tc.name, func(t *testing.T) {
			path := mediatest.Sine(t, dir, tc.name, 3, map[string]string{
				"artist":              "Test Artist",
				"album_artist":        "Test Album Artist",
				"album":               "Test Album",
				"title":               "Test Title",
				"date":                "2001",
				"track":               "3/12",
				"disc":                "1/2",
				"MUSICBRAINZ_TRACKID": "11111111-1111-1111-1111-111111111111",
			})
			p, err := tl.Probe(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if p.DurationMs < 2800 || p.DurationMs > 3400 {
				t.Errorf("duration %d ms", p.DurationMs)
			}
			if p.Lossless != tc.lossless || p.Codec == "" || p.SampleRate == 0 || p.Channels == 0 {
				t.Errorf("probe %+v", p)
			}
			g := p.Tags
			if g.Artist != "Test Artist" || g.AlbumArtist != "Test Album Artist" || g.Album != "Test Album" ||
				g.Title != "Test Title" || g.Track != 3 || g.Disc != 1 || g.Date != "2001" {
				t.Errorf("tags %+v", g)
			}
			if g.MBRecordingID != "11111111-1111-1111-1111-111111111111" {
				t.Errorf("recording id %q", g.MBRecordingID)
			}
		})
	}
}

func TestProbeRejectsNonAudio(t *testing.T) {
	tl := tools(t)
	path := filepath.Join(t.TempDir(), "fake.mp3")
	if err := os.WriteFile(path, []byte("this is not audio at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tl.Probe(context.Background(), path); !errors.Is(err, ErrNotAudio) {
		t.Fatalf("want ErrNotAudio, got %v", err)
	}
}

func TestTranscodeStartsAtOffset(t *testing.T) {
	tl := tools(t)
	dir := t.TempDir()
	src := mediatest.Sine(t, dir, "src.flac", 6, nil)
	var buf bytes.Buffer
	if err := tl.Transcode(context.Background(), src, 2000, 128, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
	out := filepath.Join(dir, "out.mp3")
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := tl.Probe(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	if p.DurationMs < 3000 || p.DurationMs > 5000 {
		t.Errorf("duration %d ms, want about 4000", p.DurationMs)
	}
}

func TestTranscodeStopsOnCancel(t *testing.T) {
	tl := tools(t)
	// A long source so ffmpeg is still busy when we cancel.
	src := mediatest.Sine(t, t.TempDir(), "long.wav", 600, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tl.Transcode(ctx, src, 0, 128, blockingWriter{}) }()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("want an error after cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Transcode did not return after cancel")
	}
}

// blockingWriter discards output slowly, like a stalled client.
type blockingWriter struct{}

func (blockingWriter) Write(p []byte) (int, error) {
	time.Sleep(50 * time.Millisecond)
	return len(p), nil
}

func TestFingerprint(t *testing.T) {
	mediatest.RequireTool(t, "fpcalc")
	tl := tools(t)
	src := mediatest.Sine(t, t.TempDir(), "fp.flac", 8, nil)
	fp, dur, err := tl.Fingerprint(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if fp == "" || dur < 7 || dur > 9 {
		t.Errorf("fingerprint %q duration %d", fp, dur)
	}
}

func TestWriteTagsRoundTrip(t *testing.T) {
	tl := tools(t)
	dir := t.TempDir()
	want := Tags{
		Artist: "Écrit Artist", AlbumArtist: "Écrit Album Artist", Album: "Written Album",
		Title: "Written Title", Date: "1999", Track: 4, Disc: 2,
		MBRecordingID:    "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		MBReleaseGroupID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		MBReleaseID:      "cccccccc-cccc-cccc-cccc-cccccccccccc",
		MBArtistID:       "dddddddd-dddd-dddd-dddd-dddddddddddd",
	}
	for _, name := range []string{"w.mp3", "w.flac", "w.ogg", "w.m4a"} {
		t.Run(name, func(t *testing.T) {
			path := mediatest.Sine(t, dir, name, 2, map[string]string{"title": "old"})
			if err := tl.WriteTags(context.Background(), path, want); err != nil {
				t.Fatal(err)
			}
			p, err := tl.Probe(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if p.Tags != want {
				t.Errorf("got %+v\nwant %+v", p.Tags, want)
			}
			if p.DurationMs < 1800 {
				t.Errorf("audio lost: %d ms", p.DurationMs)
			}
		})
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 4 {
		t.Errorf("leftover temp files: %v", entries)
	}
}

func TestWriteTagsKeepsFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	tl := tools(t)
	path := mediatest.Sine(t, t.TempDir(), "m.flac", 1, nil)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tl.WriteTags(context.Background(), path, Tags{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode %v, want 0644", info.Mode().Perm())
	}
}

func TestWriteTagsFailureLeavesOriginalAndNoTemp(t *testing.T) {
	tl := tools(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.mp3")
	content := []byte("not audio")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tl.WriteTags(context.Background(), path, Tags{Title: "x"}); err == nil {
		t.Fatal("want an error")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, content) {
		t.Error("original was modified")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestDashPrefixedPaths(t *testing.T) {
	mediatest.RequireTool(t, "fpcalc")
	tl := tools(t)
	dir := t.TempDir()
	mediatest.Sine(t, dir, "-dash.mp3", 4, nil)
	t.Chdir(dir)
	if _, err := tl.Probe(context.Background(), "-dash.mp3"); err != nil {
		t.Errorf("Probe: %v", err)
	}
	if _, _, err := tl.Fingerprint(context.Background(), "-dash.mp3"); err != nil {
		t.Errorf("Fingerprint: %v", err)
	}
	if err := tl.Transcode(context.Background(), "-dash.mp3", 0, 0, io.Discard); err == nil {
		t.Error("Transcode with 0 kbps should fail")
	}
	if err := tl.Transcode(context.Background(), "-dash.mp3", 0, 128, io.Discard); err != nil {
		t.Errorf("Transcode: %v", err)
	}
}

func TestWriteTagsKeepsOriginalWhenRemuxLosesAudio(t *testing.T) {
	tl := tools(t)
	dir := t.TempDir()
	path := mediatest.Sine(t, dir, "long.flac", 4, map[string]string{"title": "old"})
	short := mediatest.Sine(t, t.TempDir(), "short.flac", 1, nil)
	original, _ := os.ReadFile(path)
	// The remux comes out three seconds short.
	afterRemux = func(tmp string) {
		data, _ := os.ReadFile(short)
		os.WriteFile(tmp, data, 0o644)
	}
	t.Cleanup(func() { afterRemux = nil })
	if err := tl.WriteTags(context.Background(), path, Tags{Title: "new"}); err == nil {
		t.Fatal("want an error")
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, original) {
		t.Error("original was replaced")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestSameAudio(t *testing.T) {
	for _, tc := range []struct {
		before, after audioLayout
		want          bool
	}{
		{audioLayout{1, 200000}, audioLayout{1, 200000}, true},
		{audioLayout{1, 200000}, audioLayout{1, 200999}, true},
		{audioLayout{1, 200000}, audioLayout{1, 198999}, false},
		{audioLayout{2, 200000}, audioLayout{1, 200000}, false},
		{audioLayout{0, 0}, audioLayout{0, 0}, false},
	} {
		if got := sameAudio(tc.before, tc.after); got != tc.want {
			t.Errorf("sameAudio(%+v, %+v) = %v", tc.before, tc.after, got)
		}
	}
}
