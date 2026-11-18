package upload

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wuhn14/MusicHub/internal/store"
)

func TestRecoverDeletes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	// Interrupted before the row was deleted: the file gets its name back.
	kept := filepath.Join(dir, "aaaa.mp3")
	if _, err := st.InsertUploadedFile(ctx, store.File{Path: kept}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(kept+DeletingSuffix, []byte("keep"), 0o644)
	// Interrupted after the row was deleted: the file goes.
	gone := filepath.Join(dir, "bbbb.mp3")
	os.WriteFile(gone+DeletingSuffix, []byte("drop"), 0o644)
	// Unrelated files are left alone.
	other := filepath.Join(dir, "cccc.mp3")
	os.WriteFile(other, []byte("other"), 0o644)

	restored, removed, err := RecoverDeletes(ctx, st, dir)
	if err != nil || restored != 1 || removed != 1 {
		t.Fatalf("RecoverDeletes = %d, %d, %v", restored, removed, err)
	}
	if b, err := os.ReadFile(kept); err != nil || string(b) != "keep" {
		t.Errorf("restored file: %q, %v", b, err)
	}
	for _, p := range []string{kept + DeletingSuffix, gone + DeletingSuffix} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still there: %v", filepath.Base(p), err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("unrelated file touched: %v", err)
	}
	if r, d, err := RecoverDeletes(ctx, st, filepath.Join(dir, "missing")); err != nil || r+d != 0 {
		t.Errorf("missing dir = %d, %d, %v", r, d, err)
	}
}
