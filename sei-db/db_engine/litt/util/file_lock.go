package util

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"golang.org/x/sys/unix"
)

// FileLock represents a file-based lock
type FileLock struct {
	logger *slog.Logger
	path   string
	file   *os.File

	// closed records whether Release has been called.
	closed utils.CloseMarker[FileLock]
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

// lockOpenFile opens the file at path, creating it if needed, and takes an exclusive flock on it without waiting.
// It fails if another open file holds the flock. A file that no running process holds is taken, whatever it
// contains.
func lockOpenFile(path string) (*os.File, error) {
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600) //nolint:gosec // caller-supplied lock file path
		if err != nil {
			return nil, fmt.Errorf("failed to create lock file %s: %w", path, err)
		}

		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // file descriptors fit in an int
		if err != nil {
			_ = file.Close()
			if errors.Is(err, unix.EWOULDBLOCK) {
				return nil, fmt.Errorf("lock file already exists and another process holds it: %s%s",
					path, describeLockFile(path))
			}
			return nil, fmt.Errorf("failed to lock file %s: %w", path, err)
		}

		// A holder that released between our open and our flock removed the file, so the flock we hold
		// is on a file that the next process cannot find. Lock the file now at path instead.
		atPath, err := isFileAtPath(file, path)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if atPath {
			return file, nil
		}
		_ = file.Close()
	}
}

// isFileAtPath reports whether file is the file that path names now.
func isFileAtPath(file *os.File, path string) (bool, error) {
	openInfo, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("failed to stat lock file %s: %w", path, err)
	}
	pathInfo, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to stat lock file %s: %w", path, err)
	}
	return os.SameFile(openInfo, pathInfo), nil
}

// describeLockFile returns the content of the lock file at path for an error message.
func describeLockFile(path string) string {
	content, err := os.ReadFile(path) //nolint:gosec // caller-supplied lock file path
	if err != nil {
		return fmt.Sprintf(" (failed to read existing lock file: %v)", err)
	}
	return fmt.Sprintf(" (existing lock info: %s)", strings.TrimSpace(string(content)))
}

// NewFileLock acquires an exclusive lock on the file at path, creating the file if needed, and writes the process
// ID and a timestamp into it. It fails if another FileLock, in this or another process, holds the file. A lock
// file left by a process that stopped without Release does not block it.
// The caller is responsible for calling Release() to release the lock.
func NewFileLock(logger *slog.Logger, path string, fsync bool) (*FileLock, error) {
	path, err := SanitizePath(path)
	if err != nil {
		return nil, fmt.Errorf("sanitize path failed: %v", err)
	}

	file, err := lockOpenFile(path)
	if err != nil {
		return nil, err
	}

	// Write process ID and timestamp to the lock file for debugging
	lockInfo := fmt.Sprintf("PID: %d\nTimestamp: %s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	err = file.Truncate(0)
	if err == nil {
		_, err = file.WriteString(lockInfo)
	}
	if err != nil {
		removeAndClose(logger, file, path, " after write error")
		return nil, fmt.Errorf("failed to write to lock file %s: %w", path, err)
	}

	if fsync {
		err = file.Sync()
		if err != nil {
			removeAndClose(logger, file, path, " after sync error")
			return nil, fmt.Errorf("failed to sync lock file %s: %w", path, err)
		}
	}

	lock := &FileLock{
		logger: logger,
		path:   path,
		file:   file,
	}
	lock.closed = utils.MustClose(lock, "littdb file lock")
	return lock, nil
}

// removeAndClose removes the lock file at path and then closes file, which releases its flock. It logs each
// failure, with when appended to the message.
func removeAndClose(logger *slog.Logger, file *os.File, path string, when string) {
	// Removing first means that a process that takes the flock after the close finds no file at path and
	// locks a new one, so no two processes hold a lock on the file that path names.
	if err := os.Remove(path); err != nil {
		logger.Error("failed to remove lock file"+when, "path", path, "error", err)
	}
	if err := file.Close(); err != nil {
		logger.Error("failed to close lock file"+when, "path", path, "error", err)
	}
}

// Release releases the file lock by removing and closing the lock file.
// This is a no-op if the lock is already released.
func (fl *FileLock) Release() {
	if fl.file == nil {
		return
	}
	fl.closed.Close(fl)

	removeAndClose(fl.logger, fl.file, fl.path, "")
	fl.file = nil
}

// Path returns the path of the lock file
func (fl *FileLock) Path() string {
	return fl.path
}

// Create a lock on multiple directories. Returns a function that can be used to release all locks.
func LockDirectories(
	logger *slog.Logger,
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
