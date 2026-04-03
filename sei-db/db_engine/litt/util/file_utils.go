package util

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Layr-Labs/eigenda/core"
)

// SwapFileExtension is the file extension used for temporary swap files created during atomic writes.
const SwapFileExtension = ".swap"

// IsSymlink checks if the given path is a symlink.
func IsSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // Path does not exist, so it can't be a symlink
		}
		return false, fmt.Errorf("failed to stat path %s: %w", path, err)
	}

	return info.Mode()&os.ModeSymlink != 0, nil
}

// ErrIfSymlink checks if the given path is a symlink and returns an error if it is.
func ErrIfSymlink(path string) error {
	isSymlink, err := IsSymlink(path)
	if err != nil {
		return fmt.Errorf("failed to check if path %s is a symlink: %w", path, err)
	}
	if isSymlink {
		return fmt.Errorf("path %s is a symlink, but it should not be", path)
	}
	return nil
}

// IsDirectory checks if the given path is a directory. Returns false if the path is not a directory or does not exist.
func IsDirectory(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Path does not exist, so it can't be a directory
			return false, nil
		}
		return false, fmt.Errorf("failed to stat path %s: %w", path, err)
	}
	return info.IsDir(), nil
}

// SanitizePath returns a sanitized version of the given path, doing things like expanding
// "~" to the user's home directory, converting to absolute path, normalizing slashes, etc.
func SanitizePath(path string) (string, error) {
	if len(path) > 0 && path[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}

		if len(path) == 1 {
			path = homeDir
		} else if len(path) > 1 && path[1] == '/' {
			path = homeDir + path[1:]
		}
	}

	path = filepath.Clean(path)
	path = filepath.ToSlash(path)
	path, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	return path, nil
}

// DeleteOrphanedSwapFiles deletes any swap files in the given directory, i.e. files that end with ".swap".
func DeleteOrphanedSwapFiles(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", directory, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == SwapFileExtension {
			swapFilePath := filepath.Join(directory, entry.Name())
			if err := os.Remove(swapFilePath); err != nil {
				return fmt.Errorf("failed to remove swap file %s: %w", swapFilePath, err)
			}
		}
	}

	return nil
}

// AtomicWrite writes data to a file atomically. The parent directory must exist and be writable.
// If the destination file already exists, it will be overwritten.
//
// This method creates a temporary swap file in the same directory as the destination, but with SwapFileExtension
// appended to the filename. If there is a crash during this method's execution, it may leave this swap file behind.
func AtomicWrite(destination string, data []byte, fsync bool) error {

	swapPath := destination + SwapFileExtension

	// Write the data into the swap file.
	swapFile, err := os.Create(swapPath)
	if err != nil {
		return fmt.Errorf("failed to create swap file: %v", err)
	}

	_, err = swapFile.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to swap file: %v", err)
	}

	if fsync {
		// Ensure the data in the swap file is fully written to disk.
		err = swapFile.Sync()
		if err != nil {
			return fmt.Errorf("failed to sync swap file: %v", err)
		}
	}

	err = swapFile.Close()
	if err != nil {
		return fmt.Errorf("failed to close swap file: %v", err)
	}

	// Rename the swap file to the destination file.
	err = AtomicRename(swapPath, destination, fsync)
	if err != nil {
		return fmt.Errorf("failed to rename swap file: %v", err)
	}

	return nil
}

// AtomicRename renames a file from oldPath to newPath atomically.
func AtomicRename(oldPath string, newPath string, fsync bool) error {
	err := os.Rename(oldPath, newPath)
	if err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	parentDirectory := filepath.Dir(newPath)

	// Ensure that the rename is committed to disk.
	dirFile, err := os.Open(parentDirectory)
	if err != nil {
		return fmt.Errorf("failed to open parent directory %s: %w", parentDirectory, err)
	}

	if fsync {
		err = dirFile.Sync()
		if err != nil {
			return fmt.Errorf("failed to sync parent directory %s: %w", parentDirectory, err)
		}
	}

	err = dirFile.Close()
	if err != nil {
		return fmt.Errorf("failed to close parent directory %s: %w", parentDirectory, err)
	}

	return nil
}

// ErrIfNotWritableFile verifies that a path is either a regular file with read+write permissions,
// or that it is legal to create a new regular file with read+write permissions in the parent directory.
//
// A file is considered to have the correct permissions/type if:
// - it exists and is a standard file with read+write permissions
// - if it does not exist but its parent directory has read+write permissions.
//
// The arguments for the function are the result of os.Stat(path). There is no need to do error checking on the
// result of os.Stat in the calling context (this method does it for you).
func ErrIfNotWritableFile(path string) (exists bool, size int64, err error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// The file does not exist. Check the parent.
			parentPath := filepath.Dir(path)
			parentInfo, err := os.Stat(parentPath)
			if err != nil {
				if os.IsNotExist(err) {
					return false, -1, fmt.Errorf("parent directory %s does not exist", parentPath)
				}
				return false, -1, fmt.Errorf(
					"failed to stat parent directory %s: %w", parentPath, err)
			}

			if !parentInfo.IsDir() {
				return false, -1, fmt.Errorf("parent directory %s is not a directory", parentPath)
			}

			if parentInfo.Mode()&0700 != 0700 {
				return false, -1, fmt.Errorf(
					"parent directory %s has insufficient permissions", parentPath)
			}

			return false, -1, nil
		} else {
			return false, 0, fmt.Errorf("failed to stat path %s: %w", path, err)
		}
	}

	// File exists. Check if it is a regular file and that it is readable+writeable.
	if info.IsDir() {
		return false, -1, fmt.Errorf("file %s is a directory", path)
	}
	if info.Mode()&0600 != 0600 {
		return false, -1, fmt.Errorf("file %s has insufficient permissions", path)
	}

	return true, info.Size(), nil
}

// ErrIfNotWritableDirectory checks if a directory exists and is writable, or if it doesn't exist but it would
// be legal to create it.
func ErrIfNotWritableDirectory(dirPath string) error {
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist, check parent permissions
			parentDir := filepath.Dir(dirPath)
			return ErrIfNotWritableDirectory(parentDir)
		}
		return fmt.Errorf("failed to access path '%s': %w", dirPath, err)
	}

	// Path exists, verify it's a directory with write permissions
	if !info.IsDir() {
		return fmt.Errorf("path '%s' exists but is not a directory", dirPath)
	}

	if info.Mode()&0200 == 0 {
		return fmt.Errorf("directory '%s' is not writable", dirPath)
	}

	return nil
}

// Returns an error if the given path exists, otherwise returns nil.
func ErrIfExists(path string) error {
	exists, err := Exists(path)
	if err != nil {
		return fmt.Errorf("failed to check if path %s exists: %w", path, err)
	}
	if exists {
		return fmt.Errorf("path %s already exists", path)
	}
	return nil
}

// Returns an error if the given path does not exist, otherwise returns nil.
func ErrIfNotExists(path string) error {
	exists, err := Exists(path)
	if err != nil {
		return fmt.Errorf("failed to check if path %s exists: %w", path, err)
	}
	if !exists {
		return fmt.Errorf("path %s does not exist", path)
	}
	return nil
}

// Exists checks if a file or directory exists at the given path. More aesthetically pleasant than os.Stat.
func Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("error checking if path %s exists: %w", path, err)
}

// SyncFile syncs a file/directory
func SyncPath(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open path for sync: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync path: %w", err)
	}

	return nil
}

// SyncParentPath syncs the parent directory of the given path.
func SyncParentPath(path string) error {
	return SyncPath(filepath.Dir(path))
}

// CopyRegularFile copies a regular file from src to dst. If a file already exists at dst, it will be removed
// before copying.
func CopyRegularFile(src string, dst string, fsync bool) error {
	// Ensure parent directory exists
	if err := EnsureParentDirectoryExists(dst, fsync); err != nil {
		return err
	}

	// Open source file
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer core.CloseLogOnError(in, src, nil)

	// If there is already a file at the destination, remove it.
	// This ensures we don't have issues with file permissions or existing symlinks
	exists, err := Exists(dst)
	if err != nil {
		return fmt.Errorf("failed to check if destination file %s exists: %w", dst, err)
	}
	if exists {
		err = os.Remove(dst)
		if err != nil {
			return fmt.Errorf("failed to remove existing destination file %s: %w", dst, err)
		}
	}

	// Create destination file
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer core.CloseLogOnError(out, dst, nil)

	// Copy content
	if _, err = io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to copy file content from %s to %s: %w", src, dst, err)
	}

	// Sync if requested
	if fsync {
		if err = SyncPath(dst); err != nil {
			return fmt.Errorf("failed to sync destination file %s: %w", dst, err)
		}
		if err = SyncParentPath(dst); err != nil {
			return fmt.Errorf("failed to sync parent directory of %s: %w", dst, err)
		}
	}

	return nil
}

// EnsureParentDirectoryExists ensures the parent directory of the given path exists and is writable.
// Creates parent directories if they don't exist.
func EnsureParentDirectoryExists(path string, fsync bool) error {
	return EnsureDirectoryExists(filepath.Dir(path), fsync)
}

// EnsureDirectoryExists ensures a directory exists with the given permissions.
// If the directory already exists, it verifies it has write permissions.
// If fsync is true, all newly created directories are synced to disk.
func EnsureDirectoryExists(dirPath string, fsync bool) error {
	// Convert to absolute path to ensure clean processing
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for %s: %w", dirPath, err)
	}

	// Find the first ancestor that exists
	pathsToCreate := []string{}
	currentPath := absPath

	for {
		// Check if current path exists
		info, err := os.Stat(currentPath)
		if err == nil {
			// Path exists, verify it's a directory
			if !info.IsDir() {
				return fmt.Errorf("path %s exists but is not a directory", currentPath)
			}
			break // Found existing ancestor
		}

		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to check path %s: %w", currentPath, err)
		}

		// Path doesn't exist, add to list of paths to create
		pathsToCreate = append(pathsToCreate, currentPath)

		// Move to parent directory
		parentPath := filepath.Dir(currentPath)
		if parentPath == currentPath {
			// Reached filesystem root. filepath.Dir("/") returns "/", so we stop here.
			break
		}
		currentPath = parentPath
	}

	// Create directories from top-level to bottom-level and possibly sync each one
	for i := len(pathsToCreate) - 1; i >= 0; i-- {
		dirToCreate := pathsToCreate[i]

		// Create the directory
		if err := os.Mkdir(dirToCreate, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dirToCreate, err)
		}

		if fsync {
			// Sync the newly created directory
			if err := SyncPath(dirToCreate); err != nil {
				return fmt.Errorf("failed to sync newly created directory %s: %w", dirToCreate, err)
			}

			// Also sync the parent directory to ensure the directory entry is persisted
			parentDir := filepath.Dir(dirToCreate)
			if err := SyncPath(parentDir); err != nil {
				return fmt.Errorf("failed to sync parent directory %s: %w", parentDir, err)
			}
		}
	}

	return nil
}

// DeepDelete deletes a regular file. If the file is a symlink, the symlink and the file pointed to by the symlink
// are both deleted. This method can delete an empty directory, but will return an error if asked to delete a
// non-empty directory. For the sake of simplicity, this method does not traverse chain of symlinks. If the symlink
// points to another symlink, it will only delete original symlink and the symlink that the original symlink points to.
func DeepDelete(path string) error {
	isSymlink, err := IsSymlink(path)
	if err != nil {
		return fmt.Errorf("failed to check if path %s is a symlink: %w", path, err)
	}

	if isSymlink {
		// remove the file where the symlink points
		actualFile, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("failed to read symlink %s: %w", path, err)
		}
		if err := os.Remove(actualFile); err != nil {
			return fmt.Errorf("failed to remove actual file %s: %w", actualFile, err)
		}
	}

	err = os.Remove(path)
	if err != nil {
		return fmt.Errorf("failed to remove file %s: %w", path, err)
	}

	return nil
}
