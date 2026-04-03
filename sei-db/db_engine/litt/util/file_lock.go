package util

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Layr-Labs/eigensdk-go/logging"
)

// FileLock represents a file-based lock
type FileLock struct {
	logger logging.Logger
	path   string
	file   *os.File
}

// IsProcessAlive checks if a process with the given PID is still running
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Send signal 0 to check if process exists
	// This doesn't actually send a signal, just checks if we can send one
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}

	// Check the specific error
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch {
		case errors.Is(errno, syscall.ESRCH):
			// No such process
			return false
		case errors.Is(errno, syscall.EPERM):
			// Permission denied, but process exists
			return true
		default:
			// Other error, assume process exists to be safe
			return true
		}
	}

	// Unknown error, assume process exists to be safe
	return true
}

// parseLockFile parses a lock file and returns the PID if valid
func parseLockFile(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read lock file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PID: ") {
			pidStr := strings.TrimPrefix(line, "PID: ")
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				return 0, fmt.Errorf("invalid PID in lock file: %s", pidStr)
			}
			return pid, nil
		}
	}

	return 0, fmt.Errorf("no PID found in lock file")
}

// NewFileLock attempts to create a lock file at the specified path. Fails if another process has already created a
// lock file. Useful for situations where a process wants to hold a mutual exclusion lock on a resource.
// The caller is responsible for calling Release() to release the lock.
func NewFileLock(logger logging.Logger, path string, fsync bool) (*FileLock, error) {
	path, err := SanitizePath(path)
	if err != nil {
		return nil, fmt.Errorf("sanitize path failed: %v", err)
	}

	// Try to create the lock file exclusively (O_EXCL ensures it fails if file exists)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			// Lock file exists, check if it's stale
			if pid, parseErr := parseLockFile(path); parseErr == nil {
				if !IsProcessAlive(pid) {
					// Process is dead, remove stale lock file and try again
					if removeErr := os.Remove(path); removeErr != nil {
						return nil, fmt.Errorf("failed to remove stale lock file %s: %w", path, removeErr)
					}

					// Try to create the lock file again
					file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
					if err != nil {
						return nil, fmt.Errorf("failed to create lock file after removing stale lock %s: %w",
							path, err)
					}
				} else {
					// Process is still alive, cannot acquire lock
					debugInfo := ""
					content, readErr := os.ReadFile(path)
					if readErr == nil {
						debugInfo = fmt.Sprintf(" (existing lock info: %s)", strings.TrimSpace(string(content)))
					} else {
						debugInfo = fmt.Sprintf(" (failed to read existing lock file: %v)", readErr)
					}
					return nil, fmt.Errorf("lock file already exists and process %d is still running: %s%s",
						pid, path, debugInfo)
				}
			} else {
				// Cannot parse lock file, treat as existing lock with debug info
				debugInfo := ""
				if content, readErr := os.ReadFile(path); readErr == nil {
					debugInfo = fmt.Sprintf(" (existing lock info: %s)", strings.TrimSpace(string(content)))
				}
				return nil, fmt.Errorf("lock file already exists: %s%s", path, debugInfo)
			}
		} else {
			return nil, fmt.Errorf("failed to create lock file %s: %w", path, err)
		}
	}

	// Write process ID and timestamp to the lock file for debugging
	lockInfo := fmt.Sprintf("PID: %d\nTimestamp: %s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	_, err = file.WriteString(lockInfo)
	if err != nil {
		// Close and remove the file if we can't write to it
		secondaryErr := file.Close()
		if secondaryErr != nil {
			logger.Errorf("failed to close lock file %s after write error: %v", path, secondaryErr)
		}
		secondaryErr = os.Remove(path)
		if secondaryErr != nil {
			logger.Errorf("failed to remove lock file %s after write error: %v", path, secondaryErr)
		}
		return nil, fmt.Errorf("failed to write to lock file %s: %w", path, err)
	}

	if fsync {
		err = file.Sync()
		if err != nil {
			// Close and remove the file if we can't sync it
			secondaryErr := file.Close()
			if secondaryErr != nil {
				logger.Errorf("failed to close lock file %s after sync error: %v", path, secondaryErr)
			}
			secondaryErr = os.Remove(path)
			if secondaryErr != nil {
				logger.Errorf("failed to remove lock file %s after sync error: %v", path, secondaryErr)
			}
			return nil, fmt.Errorf("failed to sync lock file %s: %w", path, err)
		}
	}

	return &FileLock{
		logger: logger,
		path:   path,
		file:   file,
	}, nil
}

// Release releases the file lock by closing and removing the lock file.
// This is a no-op if the lock is already released.
func (fl *FileLock) Release() {
	if fl.file == nil {
		return
	}

	// Close the file first
	err := fl.file.Close()
	fl.file = nil

	if err != nil {
		fl.logger.Errorf("failed to close lock file %s: %w", fl.path, err)
		return
	}

	// Remove the lock file
	err = os.Remove(fl.path)
	if err != nil {
		fl.logger.Errorf("failed to remove lock file %s: %w", fl.path, err)
		return
	}
}

// Path returns the path of the lock file
func (fl *FileLock) Path() string {
	return fl.path
}

// Create a lock on multiple directories. Returns a function that can be used to release all locks.
func LockDirectories(
	logger logging.Logger,
	directories []string,
	lockFileName string,
	fsync bool) (func(), error) {

	locks := make([]*FileLock, 0, len(directories))
	for _, dir := range directories {
		lockFilePath := path.Join(dir, lockFileName)
		lock, err := NewFileLock(logger, lockFilePath, fsync)
		if err != nil {
			// Release all previously acquired locks before returning an error
			for _, l := range locks {
				l.Release()
			}
			return nil, fmt.Errorf("failed to acquire lock on directory %s: %v", dir, err)
		}
		locks = append(locks, lock)
	}

	return func() {
		for _, lock := range locks {
			lock.Release()
		}
	}, nil
}
