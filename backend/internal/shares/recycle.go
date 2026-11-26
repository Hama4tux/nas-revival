package shares

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// RecycleResult counts what a cleanup removed.
type RecycleResult struct {
	RemovedFiles int   `json:"removedFiles"`
	RemovedBytes int64 `json:"removedBytes"`
}

// CleanupRecycle deletes files and symlinks in <path>/.recycle older than
// days, then empty directories (never .recycle itself). Symlinks are removed
// as links, never followed.
func CleanupRecycle(ctx context.Context, path string, days int, now time.Time) (RecycleResult, error) {
	var res RecycleResult
	root := filepath.Join(path, ".recycle")
	if fi, err := os.Lstat(root); err != nil || !fi.IsDir() {
		return res, nil
	}
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	var walk func(dir string) error
	walk = func(dir string) error {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, e := range ents {
			if err := ctx.Err(); err != nil {
				return err
			}
			p := filepath.Join(dir, e.Name())
			fi, err := os.Lstat(p)
			if err != nil {
				continue
			}
			switch {
			case fi.IsDir():
				if err := walk(p); err != nil {
					return err
				}
				os.Remove(p) // only succeeds when empty
			case fi.Mode()&fs.ModeSymlink != 0 || fi.Mode().IsRegular():
				if fi.ModTime().Before(cutoff) && os.Remove(p) == nil {
					res.RemovedFiles++
					if fi.Mode().IsRegular() {
						res.RemovedBytes += fi.Size()
					}
				}
			}
		}
		return nil
	}
	return res, walk(root)
}
