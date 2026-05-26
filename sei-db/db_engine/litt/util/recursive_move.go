package util

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// RecursiveMove transfers files/directory trees from the source to the destination.
//
// If preserveOriginal is false, then the files at the source will be deleted when this method returns.
// If preserveOriginal is true, then this function will leave behind a copy of the original files at the source.
//
// This function does not support symlinks. It will return an error if it encounters any symlinks in the source path.
func RecursiveMove(
	source string,
	destination string,
	preserveOriginal bool,
	fsync bool,
) error {
	// Sanitize paths
	source, err := SanitizePath(source)
	if err != nil {
		return fmt.Errorf("failed to sanitize source path: %w", err)
	}

	destination, err = SanitizePath(destination)
	if err != nil {
		return fmt.Errorf("failed to sanitize destination path: %w", err)
	}

	// Verify source exists
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("source path %s does not exist: %w", source, err)
	}

	// Verify destination parent directory is writable
	if err := ErrIfNotWritableDirectory(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("destination parent directory not writable: %w", err)
	}

	// If source is a file, handle it directly
	if !sourceInfo.IsDir() {
		return moveFile(source, destination, preserveOriginal, fsync)
	}

	// Source is a directory, handle recursively
	return recursiveMoveDirectory(source, destination, preserveOriginal, fsync)
}

// moveFile handles moving a single file
func moveFile(source string, destination string, preserveOriginal bool, fsync bool) error {
	// Ensure parent directory exists
	if err := EnsureParentDirectoryExists(destination, fsync); err != nil {
		return fmt.Errorf("failed to ensure parent directory exists: %w", err)
	}

	// If not preserving original, try to move the file first (regardless of deep mode)
	if !preserveOriginal {
		// Try simple rename first (works if on same filesystem)
		if err := os.Rename(source, destination); err == nil {
			if fsync {
				if err := SyncPath(filepath.Dir(destination)); err != nil {
					return fmt.Errorf("failed to sync destination parent directory: %w", err)
				}
				if err := SyncPath(filepath.Dir(source)); err != nil {
					return fmt.Errorf("failed to sync source parent directory: %w", err)
				}
			}

			return nil
		}
		// Rename failed (likely different filesystem), fall back to copy+delete
	}

	err := ErrIfSymlink(source)
	if err != nil {
		return fmt.Errorf("symlinks not supported: %w", err)
	}

	// Copy the file
	if err := CopyRegularFile(source, destination, fsync); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// Sync if requested
	if fsync {
		if err := SyncPath(destination); err != nil {
			return fmt.Errorf("failed to sync destination file: %w", err)
		}
		// sync parent directory
		if err := SyncPath(filepath.Dir(destination)); err != nil {
			return fmt.Errorf("failed to sync parent directory: %w", err)
		}
	}

	// Remove source if not preserving original
	if !preserveOriginal {
		if err := os.Remove(source); err != nil {
			return fmt.Errorf("failed to remove source file: %w", err)
		}
	}

	return nil
}

// recursiveMoveDirectory handles moving a directory and its contents
func recursiveMoveDirectory(
	source string,
	destination string,
	preserveOriginal bool,
	fsync bool,
) error {

	// Create destination directory if it doesn't exist
	if err := EnsureDirectoryExists(destination, fsync); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Walk through source directory
	err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk path %s: %w", path, err)
		}

		// Skip the root directory itself
		if path == source {
			return nil
		}

		// Calculate relative path and destination path
		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		destPath := filepath.Join(destination, relPath)

		err = ErrIfSymlink(path)
		if err != nil {
			return fmt.Errorf("symlinks not supported: %w", err)
		}

		if d.IsDir() {
			// Create directory at destination
			if err := EnsureDirectoryExists(destPath, fsync); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
		} else {
			// Move the file
			if err := moveFile(path, destPath, preserveOriginal, fsync); err != nil {
				return fmt.Errorf("failed to copy regular file: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Sync destination directory if requested
	if fsync {
		if err := SyncPath(destination); err != nil {
			return fmt.Errorf("failed to sync destination directory: %w", err)
		}
	}

	// Remove source directory if not preserving original
	if !preserveOriginal {
		if err := os.RemoveAll(source); err != nil {
			return fmt.Errorf("failed to remove source directory: %w", err)
		}
		if fsync {
			if err := SyncPath(filepath.Dir(source)); err != nil {
				return fmt.Errorf("failed to sync parent directory of source: %w", err)
			}
		}
	}

	return nil
}
