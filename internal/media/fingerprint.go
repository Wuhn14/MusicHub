package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Fingerprint computes the Chromaprint fingerprint of path with fpcalc and
// returns it with the audio duration in seconds.
func (t *Tools) Fingerprint(ctx context.Context, path string) (string, int, error) {
	out, _, err := run(ctx, fingerprintTimeout, t.fpcalc, "-json", path)
	if err != nil {
		return "", 0, err
	}
	var r struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return "", 0, fmt.Errorf("media: parse fpcalc output: %w", err)
	}
	if r.Fingerprint == "" {
		return "", 0, errors.New("media: fpcalc returned no fingerprint")
	}
	return r.Fingerprint, int(math.Round(r.Duration)), nil
}

// WriteTags rewrites the tags of path without re-encoding. Empty fields (and
// zero track/disc numbers) are left as they are. The file is remuxed to a
// temporary file in the same directory, which then replaces the original.
func (t *Tools) WriteTags(ctx context.Context, path string, tg Tags) error {
	ext := strings.ToLower(filepath.Ext(path))
	origExt := filepath.Ext(path)
	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".musichub-tags-*"+origExt)
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	tmpFile.Close()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()

	args := []string{"-hide_banner", "-loglevel", "error", "-i", path,
		"-map", "0", "-c", "copy", "-map_metadata", "0"}
	// Ogg/Opus keep their comments on the stream, and stream tags copied from
	// the input would win over global ones, so write both there.
	streamTags := ext == ".ogg" || ext == ".oga" || ext == ".opus"
	add := func(key, val string) {
		if val == "" {
			return
		}
		args = append(args, "-metadata", key+"="+val)
		if streamTags {
			args = append(args, "-metadata:s:a:0", key+"="+val)
		}
	}
	add("artist", tg.Artist)
	add("album_artist", tg.AlbumArtist)
	add("album", tg.Album)
	add("title", tg.Title)
	add("date", tg.Date)
	if tg.Track > 0 {
		add("track", strconv.Itoa(tg.Track))
	}
	if tg.Disc > 0 {
		add("disc", strconv.Itoa(tg.Disc))
	}
	add("MUSICBRAINZ_TRACKID", tg.MBRecordingID)
	add("MUSICBRAINZ_RELEASEGROUPID", tg.MBReleaseGroupID)
	add("MUSICBRAINZ_ALBUMID", tg.MBReleaseID)
	add("MUSICBRAINZ_ARTISTID", tg.MBArtistID)
	if ext == ".m4a" || ext == ".mp4" {
		// Without this the MP4 muxer drops tags it has no atom for (MusicBrainz ids).
		args = append(args, "-movflags", "use_metadata_tags")
	}
	args = append(args, "-y", tmp)

	if _, _, err := run(ctx, writeTagsTimeout, t.ffmpeg, args...); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}
