package utils

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ClonePebbleDir copies one PebbleDB directory. Immutable .sst files are
// hard-linked; everything else is byte-copied. The LOCK file is skipped, as the
// destination takes its own.
//
// A subdirectory is refused rather than skipped: the default layout has none,
// and a configured WAL directory inside the database would otherwise make the
// clone silently incomplete.
func ClonePebbleDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			return fmt.Errorf("clone %q: unexpected subdirectory %q in PebbleDB directory", src, name)
		}
		if name == "LOCK" {
			continue
		}

		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		if strings.HasSuffix(name, ".sst") {
			if linkErr := os.Link(srcPath, dstPath); linkErr == nil {
				continue
			}
			// Fall back to a copy if hardlinks are not available.
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}
	return SyncDir(dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // paths come from internal DB layout
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst) //nolint:gosec // paths come from internal DB layout
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// SyncDir persists a directory's own entries, which an fsync of the files in it
// does not cover.
func SyncDir(path string) error {
	dir, err := os.Open(path) //nolint:gosec // paths come from internal DB layout
	if err != nil {
		return fmt.Errorf("open directory %q for sync: %w", path, err)
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		syncErr = fmt.Errorf("sync directory %q: %w", path, syncErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close directory %q after sync: %w", path, closeErr)
	}
	return errors.Join(syncErr, closeErr)
}
