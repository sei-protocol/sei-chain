package util

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockFile takes an exclusive open file description lock on the whole of file without waiting. It returns
// errLockHeld when another open file holds a lock on it.
func tryLockFile(file *os.File) error {
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: io.SeekStart}
	err := unix.FcntlFlock(file.Fd(), unix.F_OFD_SETLK, &lock)
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EACCES) {
		return errLockHeld
	}
	return err
}
