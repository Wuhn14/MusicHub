// Package media wraps the external audio tools MusicHub relies on: ffprobe
// (metadata), ffmpeg (transcoding and tag writing) and fpcalc (Chromaprint
// fingerprints). Every sub-process is started with exec.CommandContext and an
// argument list, never through a shell.
package media

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// ErrNotAudio is returned by Probe when a file has no audio stream.
var ErrNotAudio = errors.New("media: file is not audio")

const (
	probeTimeout       = 30 * time.Second
	fingerprintTimeout = 60 * time.Second
	writeTagsTimeout   = 120 * time.Second
	statusTimeout      = 10 * time.Second
	maxStderr          = 2048
)

// Tools runs the audio executables.
type Tools struct {
	ffmpeg, ffprobe, fpcalc string
}

// New returns Tools using the given executables (names looked up in PATH, or
// absolute paths).
func New(ffmpeg, ffprobe, fpcalc string) *Tools {
	return &Tools{ffmpeg: ffmpeg, ffprobe: ffprobe, fpcalc: fpcalc}
}

// ToolStatus describes one external tool.
type ToolStatus struct {
	Name      string
	Path      string
	Version   string
	Available bool
	Error     string
}

// Status reports whether each tool can be run and which version it is.
func (t *Tools) Status(ctx context.Context) []ToolStatus {
	return []ToolStatus{
		toolStatus(ctx, "ffmpeg", t.ffmpeg),
		toolStatus(ctx, "ffprobe", t.ffprobe),
		toolStatus(ctx, "fpcalc", t.fpcalc),
	}
}

func toolStatus(ctx context.Context, name, bin string) ToolStatus {
	st := ToolStatus{Name: name}
	path, err := exec.LookPath(bin)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Path = path
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-version").Output()
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Available = true
	line, _, _ := strings.Cut(string(out), "\n")
	st.Version = strings.TrimSpace(line)
	return st
}

// run executes bin with args and returns stdout and stderr. On failure the
// error carries the (truncated) stderr.
func run(ctx context.Context, timeout time.Duration, bin string, args ...string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, stderr.String(), ctx.Err()
		}
		return nil, stderr.String(), &toolError{tool: bin, err: err, stderr: stderr.String()}
	}
	return stdout.Bytes(), stderr.String(), nil
}

type toolError struct {
	tool   string
	err    error
	stderr string
}

func (e *toolError) Error() string {
	return e.tool + ": " + e.err.Error() + ": " + truncate(e.stderr)
}

func (e *toolError) Unwrap() error { return e.err }

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxStderr {
		s = s[:maxStderr] + "..."
	}
	return s
}
