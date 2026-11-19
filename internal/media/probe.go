package media

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// Tags are the metadata MusicHub cares about.
type Tags struct {
	Artist, AlbumArtist, Album, Title, Date string
	Track, Disc                             int
	MBRecordingID, MBReleaseGroupID         string
	MBReleaseID, MBArtistID                 string
}

// Probe is what ffprobe reports about an audio file.
type Probe struct {
	Format, Codec string
	BitRate       int
	SampleRate    int
	Channels      int
	DurationMs    int
	Lossless      bool
	Tags          Tags
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType  string            `json:"codec_type"`
		CodecName  string            `json:"codec_name"`
		SampleRate string            `json:"sample_rate"`
		Channels   int               `json:"channels"`
		BitRate    string            `json:"bit_rate"`
		Tags       map[string]string `json:"tags"`
	} `json:"streams"`
	Format struct {
		FormatName string            `json:"format_name"`
		Duration   string            `json:"duration"`
		BitRate    string            `json:"bit_rate"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
}

// FFprobeAvailable reports whether the ffprobe executable can be found.
func (t *Tools) FFprobeAvailable() bool {
	_, err := exec.LookPath(t.ffprobe)
	return err == nil
}

// Probe reads the technical properties and tags of an audio file. A file
// without an audio stream (or that ffprobe cannot parse) yields ErrNotAudio.
func (t *Tools) Probe(ctx context.Context, path string) (Probe, error) {
	out, stderr, err := run(ctx, probeTimeout, t.ffprobe,
		"-v", "error", "-print_format", "json", "-show_format", "-show_streams", "-i", safePath(path))
	if err != nil {
		if strings.Contains(stderr, "Invalid data found") {
			return Probe{}, ErrNotAudio
		}
		return Probe{}, err
	}
	var o ffprobeOutput
	if err := json.Unmarshal(out, &o); err != nil {
		return Probe{}, fmt.Errorf("media: parse ffprobe output: %w", err)
	}
	idx := -1
	for i, s := range o.Streams {
		if s.CodecType == "audio" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Probe{}, ErrNotAudio
	}
	s := o.Streams[idx]
	p := Probe{
		Format:     o.Format.FormatName,
		Codec:      s.CodecName,
		Channels:   s.Channels,
		SampleRate: atoi(s.SampleRate),
		BitRate:    atoi(s.BitRate),
		Lossless:   isLossless(s.CodecName),
	}
	if p.BitRate == 0 {
		p.BitRate = atoi(o.Format.BitRate)
	}
	if d, err := strconv.ParseFloat(o.Format.Duration, 64); err == nil && d >= 0 {
		p.DurationMs = int(math.Round(d * 1000))
	}
	// Tags may live on the stream (Ogg) or on the container (ID3, MP4);
	// container values win.
	tags := map[string]string{}
	for k, v := range s.Tags {
		tags[normKey(k)] = v
	}
	for k, v := range o.Format.Tags {
		tags[normKey(k)] = v
	}
	p.Tags = tagsFromMap(tags)
	return p, nil
}

func isLossless(codec string) bool {
	switch codec {
	case "flac", "alac", "wavpack", "ape":
		return true
	}
	return strings.HasPrefix(codec, "pcm_")
}

// normKey lowercases a tag key and drops separators, so "MusicBrainz Track Id",
// "MUSICBRAINZ_TRACKID" and "musicbrainz-trackid" all compare equal.
func normKey(k string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '_', '-':
			return -1
		}
		return r
	}, strings.ToLower(k))
}

func tagsFromMap(m map[string]string) Tags {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(m[k]); v != "" {
				return v
			}
		}
		return ""
	}
	return Tags{
		Artist:           get("artist"),
		AlbumArtist:      get("albumartist"),
		Album:            get("album"),
		Title:            get("title"),
		Date:             get("date", "year", "originaldate"),
		Track:            leadingInt(get("track", "tracknumber")),
		Disc:             leadingInt(get("disc", "discnumber")),
		MBRecordingID:    get("musicbrainztrackid", "musicbrainzrecordingid"),
		MBReleaseGroupID: get("musicbrainzreleasegroupid"),
		MBReleaseID:      get("musicbrainzalbumid", "musicbrainzreleaseid"),
		MBArtistID:       get("musicbrainzartistid"),
	}
}

// leadingInt parses "3" or "3/12" as 3; anything else is 0.
func leadingInt(s string) int {
	head, _, _ := strings.Cut(s, "/")
	n, err := strconv.Atoi(strings.TrimSpace(head))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// audioLayout is what a remux must keep: the number of audio streams and
// the duration.
type audioLayout struct {
	streams    int
	durationMs int
}

// layout reads the audio layout of a file with ffprobe.
func (t *Tools) layout(ctx context.Context, path string) (audioLayout, error) {
	out, _, err := run(ctx, probeTimeout, t.ffprobe,
		"-v", "error", "-print_format", "json", "-show_format", "-show_streams", "-i", safePath(path))
	if err != nil {
		return audioLayout{}, err
	}
	var o ffprobeOutput
	if err := json.Unmarshal(out, &o); err != nil {
		return audioLayout{}, fmt.Errorf("media: parse ffprobe output: %w", err)
	}
	var l audioLayout
	for _, s := range o.Streams {
		if s.CodecType == "audio" {
			l.streams++
		}
	}
	if d, err := strconv.ParseFloat(o.Format.Duration, 64); err == nil && d >= 0 {
		l.durationMs = int(math.Round(d * 1000))
	}
	return l, nil
}

// sameAudio reports whether a remuxed file kept the audio of the original:
// as many audio streams (at least one) and a duration within one second.
func sameAudio(before, after audioLayout) bool {
	d := before.durationMs - after.durationMs
	if d < 0 {
		d = -d
	}
	return before.streams > 0 && after.streams == before.streams && d <= 1000
}
