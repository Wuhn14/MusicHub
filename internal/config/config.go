// Package config loads MusicHub settings from environment variables.
package config

import (
	"fmt"
	"strconv"
)

// Config holds the runtime settings of MusicHub.
type Config struct {
	Port           int
	DataDir        string
	MusicDir       string
	MusicBrainzURL string
	CoverArtURL    string
	// AcoustIDURL is the base URL of the AcoustID API (version 2).
	AcoustIDURL string
	// FFmpeg, FFprobe and FPcalc are the audio tool executables (name looked
	// up in PATH, or an absolute path).
	FFmpeg  string
	FFprobe string
	FPcalc  string
}

// Load builds a Config from getenv (typically os.Getenv), applying defaults.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:           8080,
		DataDir:        valueOr(getenv, "MUSICHUB_DATA", "./data"),
		MusicDir:       valueOr(getenv, "MUSICHUB_MUSIC", "./music"),
		MusicBrainzURL: valueOr(getenv, "MUSICHUB_MB_URL", "https://musicbrainz.org/ws/2"),
		CoverArtURL:    valueOr(getenv, "MUSICHUB_CAA_URL", "https://coverartarchive.org"),
		AcoustIDURL:    valueOr(getenv, "MUSICHUB_ACOUSTID_URL", "https://api.acoustid.org/v2"),
		FFmpeg:         valueOr(getenv, "MUSICHUB_FFMPEG", "ffmpeg"),
		FFprobe:        valueOr(getenv, "MUSICHUB_FFPROBE", "ffprobe"),
		FPcalc:         valueOr(getenv, "MUSICHUB_FPCALC", "fpcalc"),
	}
	if raw := getenv("MUSICHUB_PORT"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("MUSICHUB_PORT invalide %q : un nombre entre 1 et 65535 est attendu", raw)
		}
		cfg.Port = port
	}
	return cfg, nil
}

func valueOr(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}
