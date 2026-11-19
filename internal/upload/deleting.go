package upload

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wuhn14/MusicHub/internal/store"
)

// DeletingSuffix marks an uploaded file being deleted: the API renames it
// first, so nothing else uses it while its row is removed.
const DeletingSuffix = ".deleting"

// RecoverDeletes settles the deletions a stop interrupted in the incoming
// folder: a renamed file whose row still points at its original path gets
// that name back; one that no row references any more is removed. It returns
// how many files were restored and removed.
func RecoverDeletes(ctx context.Context, st *store.Store, dir string) (restored, removed int, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), DeletingSuffix) {
			continue
		}
		tomb := filepath.Join(dir, e.Name())
		orig := strings.TrimSuffix(tomb, DeletingSuffix)
		_, gerr := st.GetFileByPath(ctx, orig)
		switch {
		case gerr == nil:
			if _, err := os.Lstat(orig); err == nil {
				slog.Warn("envoi en cours de suppression : le nom d'origine est déjà pris, fichier gardé", "path", tomb)
				continue
			}
			if err := os.Rename(tomb, orig); err != nil {
				slog.Error("envoi en cours de suppression non restauré", "path", tomb, "err", err)
				continue
			}
			restored++
		case errors.Is(gerr, store.ErrNotFound):
			if err := os.Remove(tomb); err != nil {
				slog.Error("envoi supprimé resté sur le disque", "path", tomb, "err", err)
				continue
			}
			removed++
		default:
			return restored, removed, gerr
		}
	}
	return restored, removed, nil
}
