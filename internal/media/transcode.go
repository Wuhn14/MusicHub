package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Transcode streams the first audio stream of path, starting at startMs, as
// MP3 at kbps into w. It is bounded only by ctx: cancelling ctx kills ffmpeg.
func (t *Tools) Transcode(ctx context.Context, path string, startMs, kbps int, w io.Writer) error {
	if kbps <= 0 {
		return fmt.Errorf("media: invalid bitrate %d kbps", kbps)
	}
	if startMs < 0 {
		startMs = 0
	}
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", fmt.Sprintf("%d.%03d", startMs/1000, startMs%1000),
		"-i", safePath(path),
		"-vn", "-map", "0:a:0",
		"-c:a", "libmp3lame", "-b:a", fmt.Sprintf("%dk", kbps),
		"-f", "mp3", "pipe:1",
	}
	cmd := exec.CommandContext(ctx, t.ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stdout = w
	cmd.Stderr = &stderr
	// If the writer is stuck, do not wait forever for the copy after the kill.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &toolError{tool: "ffmpeg", err: err, stderr: stderr.String()}
	}
	return nil
}
