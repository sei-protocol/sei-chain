package hashlog

import (
	"fmt"
	"os"
	"path/filepath"
)

// This file provides the two filesystem helpers the hashlog package needs
// (atomic rename + recursive directory creation with optional fsync). On main
// these live in sei-db/db_engine/litt/util (part of LittDB), but that package
// is not present on this branch, so the two functions are vendored here to keep
// the hashlog package self-contained.

// syncPath fsyncs the file or directory at path.
func syncPath(path string) error {
	f, err := os.Open(path) //nolint:gosec // path derived from the caller-supplied hashlog dir
	if err != nil {
		return fmt.Errorf("failed to open %s for sync: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to sync %s: %w", path, err)
	}
	return f.Close()
}

// atomicRename renames oldPath to newPath. When fsync is true it also fsyncs
// the destination's parent directory so the rename is durable.
func atomicRename(oldPath string, newPath string, fsync bool) error {
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}
	if fsync {
		if err := syncPath(filepath.Dir(newPath)); err != nil {
			return err
		}
	}
	return nil
}

// ensureDirectoryExists makes sure dirPath exists, creating any missing parent
// directories. When fsync is true, each newly created directory (and its
// parent) is fsynced. If the path already exists it must be a directory.
func ensureDirectoryExists(dirPath string, fsync bool) error {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for %s: %w", dirPath, err)
	}

	// Walk up to the first existing ancestor, recording the dirs to create.
	var pathsToCreate []string
	currentPath := absPath
	for {
		info, statErr := os.Stat(currentPath)
		if statErr == nil {
			if !info.IsDir() {
				return fmt.Errorf("path %s exists but is not a directory", currentPath)
			}
			break
		}
		if !os.IsNotExist(statErr) {
			return fmt.Errorf("failed to check path %s: %w", currentPath, statErr)
		}
		pathsToCreate = append(pathsToCreate, currentPath)
		parentPath := filepath.Dir(currentPath)
		if parentPath == currentPath {
			break // reached filesystem root
		}
		currentPath = parentPath
	}

	// Create from the top-most missing ancestor down.
	for i := len(pathsToCreate) - 1; i >= 0; i-- {
		dirToCreate := pathsToCreate[i]
		if err := os.Mkdir(dirToCreate, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dirToCreate, err)
		}
		if fsync {
			if err := syncPath(dirToCreate); err != nil {
				return fmt.Errorf("failed to sync newly created directory %s: %w", dirToCreate, err)
			}
			parent := filepath.Dir(dirToCreate)
			if err := syncPath(parent); err != nil {
				return fmt.Errorf("failed to sync parent directory %s: %w", parent, err)
			}
		}
	}
	return nil
}
