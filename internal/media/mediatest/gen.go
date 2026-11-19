// Package mediatest generates audio files for tests, so the repository never
// ships real music.
package mediatest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// RequireTool skips the test when the named tool is not in PATH. When
// MUSICHUB_REQUIRE_TOOLS=1 (set in CI) the test fails instead.
func RequireTool(t testing.TB, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		if os.Getenv("MUSICHUB_REQUIRE_TOOLS") == "1" {
			t.Fatalf("%s is required but not found in PATH", name)
		}
		t.Skipf("%s not found in PATH", name)
	}
}

// Sine writes a sine-wave audio file of the given length into dir and returns
// its path. The container and codec are inferred from the file extension.
// tags are written as ffmpeg -metadata key=value pairs.
func Sine(t testing.TB, dir, name string, seconds int, tags map[string]string) string {
	t.Helper()
	RequireTool(t, "ffmpeg")
	path := filepath.Join(dir, name)
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=" + strconv.Itoa(seconds)}
	for k, v := range tags {
		args = append(args, "-metadata", k+"="+v)
	}
	args = append(args, "-y", path)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate %s: %v\n%s", name, err, out)
	}
	return path
}
