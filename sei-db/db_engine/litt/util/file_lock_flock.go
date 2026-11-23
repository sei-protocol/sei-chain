//go:build unix && !linux

package util

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockFile takes an exclusive flock on file without waiting. It returns errLockHeld when another open file
// holds a lock on it.
func tryLockFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // file descriptors fit in an int
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errLockHeld
	}
	return err
}
