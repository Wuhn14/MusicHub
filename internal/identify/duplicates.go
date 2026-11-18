package identify

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"

	"github.com/Wuhn14/MusicHub/internal/settings"
	"github.com/Wuhn14/MusicHub/internal/store"
)

// Duplicate outcomes recorded in Result.Duplicate.
const (
	DupReplacedExisting = "replaced-existing" // an uploaded existing file was deleted
	DupDiscardedNew     = "discarded-new"     // the uploaded newcomer was deleted
	DupHiddenNew        = "hidden-new"        // the newcomer was hidden
	DupHiddenExisting   = "hidden-existing"   // an existing file was hidden
)

// better reports whether a is of strictly better quality than b: lossless
// first, then the higher bit rate, then the larger file.
func better(a, b store.File) bool {
	if a.Lossless != b.Lossless {
		return a.Lossless
	}
	if a.BitRate != b.BitRate {
		return a.BitRate > b.BitRate
	}
	return a.Size > b.Size
}

// resolveDuplicates applies the duplicates setting to the newly linked file
// f against the other identified files of the same recording. The loser of a
// comparison is deleted (disk and database) when it was uploaded, and hidden
// when it was dropped in the music folder: a dropped file is never deleted.
// Ties keep the existing file. It returns the last outcome ("" when nothing
// was done).
func resolveDuplicates(ctx context.Context, st *store.Store, f store.File, policy string) (string, error) {
	if policy == settings.DuplicatesKeepBoth {
		return "", nil
	}
	others, err := st.ListFiles(ctx, store.FileFilter{RecordingID: f.RecordingID, Status: store.FileIdentified})
	if err != nil {
		return "", err
	}
	outcome := ""
	for _, o := range others {
		if o.ID == f.ID {
			continue
		}
		newWins := policy != settings.DuplicatesKeepExisting && better(f, o)
		if !newWins {
			// The newcomer loses: nothing else to compare.
			return discard(ctx, st, f, DupDiscardedNew, DupHiddenNew)
		}
		if outcome, err = discard(ctx, st, o, DupReplacedExisting, DupHiddenExisting); err != nil {
			return outcome, err
		}
	}
	return outcome, nil
}

// discard removes the losing file: an uploaded file is deleted from the disk
// then from the database (deletedOutcome); a dropped one, or an uploaded one
// that cannot be removed from the disk, is hidden (hiddenOutcome).
func discard(ctx context.Context, st *store.Store, f store.File, deletedOutcome, hiddenOutcome string) (string, error) {
	if f.Origin == store.OriginUploaded {
		err := os.Remove(f.Path)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			if err := st.DeleteFile(ctx, f.ID); err != nil {
				return "", err
			}
			slog.Info("doublon supprimé", "file", f.ID, "path", f.Path)
			return deletedOutcome, nil
		}
		slog.Warn("doublon impossible à supprimer, il est masqué", "file", f.ID, "path", f.Path, "err", err)
	}
	if err := st.SetFileStatus(ctx, f.ID, store.FileHidden); err != nil {
		return "", err
	}
	slog.Info("doublon masqué", "file", f.ID, "path", f.Path)
	return hiddenOutcome, nil
}
