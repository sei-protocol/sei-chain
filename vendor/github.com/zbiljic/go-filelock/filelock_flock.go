// +build !windows,!plan9,!solaris

package filelock

import "syscall"

// flockTryLockFile tries to acquire an advisory lock on a file descriptor.
func flockTryLockFile(fd int) (bool, error) {
	err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		if err == syscall.EWOULDBLOCK {
			return false, ErrLocked
		}
		return false, err
	}
	return true, nil
}

// flockLockFile acquires an advisory lock on a file descriptor.
func flockLockFile(fd int) error {
	return syscall.Flock(fd, syscall.LOCK_EX)
}

// flockUnlockFile releases an advisory lock on a file descriptor.
func flockUnlockFile(fd int) error {
	return syscall.Flock(fd, syscall.LOCK_UN)
}
