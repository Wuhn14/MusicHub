package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Port:           8080,
		DataDir:        "./data",
		MusicDir:       "./music",
		MusicBrainzURL: "https://musicbrainz.org/ws/2",
		CoverArtURL:    "https://coverartarchive.org",
		FFmpeg:         "ffmpeg",
		FFprobe:        "ffprobe",
		FPcalc:         "fpcalc",
	}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"MUSICHUB_PORT":    "9000",
		"MUSICHUB_DATA":    "/data",
		"MUSICHUB_MUSIC":   "/music",
		"MUSICHUB_MB_URL":  "http://localhost:5000/ws/2",
		"MUSICHUB_CAA_URL": "http://localhost:5001",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Port:           9000,
		DataDir:        "/data",
		MusicDir:       "/music",
		MusicBrainzURL: "http://localhost:5000/ws/2",
		CoverArtURL:    "http://localhost:5001",
		FFmpeg:         "ffmpeg",
		FFprobe:        "ffprobe",
		FPcalc:         "fpcalc",
	}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	for _, port := range []string{"abc", "70000"} {
		if _, err := Load(env(map[string]string{"MUSICHUB_PORT": port})); err == nil {
			t.Errorf("port %q: expected an error", port)
		}
	}
}

func TestLoadToolPaths(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"MUSICHUB_FFMPEG":  "/opt/ff/ffmpeg",
		"MUSICHUB_FFPROBE": "/opt/ff/ffprobe",
		"MUSICHUB_FPCALC":  "/opt/ff/fpcalc",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FFmpeg != "/opt/ff/ffmpeg" || cfg.FFprobe != "/opt/ff/ffprobe" || cfg.FPcalc != "/opt/ff/fpcalc" {
		t.Errorf("got %q %q %q", cfg.FFmpeg, cfg.FFprobe, cfg.FPcalc)
	}
}
