// +build solaris

package filelock

import (
	"os"
	"path/filepath"
	"syscall"
)

type lock struct {
	path string
	fd   int
	file *os.File
}

// New creates a new lock
func New(path string) (TryLockerSafe, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrNeedAbsPath
	}
	file, err := os.OpenFile(path, os.O_WRONLY, privateFileMode)
	if err != nil {
		return nil, err
	}
	l := &lock{
		path: path,
		fd:   int(file.Fd()),
		file: file,
	}
	return l, nil
}

func (l *lock) String() string {
	return filepath.Base(l.path)
}

// TryLock acquires exclusivity on the lock without blocking
func (l *lock) TryLock() (bool, error) {
	var lock syscall.Flock_t
	lock.Start = 0
	lock.Len = 0
	lock.Pid = 0
	lock.Type = syscall.F_WRLCK
	lock.Whence = 0
	lock.Pid = 0
	err := syscall.FcntlFlock(uintptr(l.fd), syscall.F_SETLK, &lock)
	if err != nil {
		if err == syscall.EAGAIN {
			return false, ErrLocked
		}
		return false, err
	}
	return true, nil
}

// Lock acquires exclusivity on the lock without blocking
func (l *lock) Lock() error {
	var lock syscall.Flock_t
	lock.Start = 0
	lock.Len = 0
	lock.Type = syscall.F_WRLCK
	lock.Whence = 0
	lock.Pid = 0
	return syscall.FcntlFlock(uintptr(l.fd), syscall.F_SETLK, &lock)
}

// Unlock unlocks the lock
func (l *lock) Unlock() error {
	var lock syscall.Flock_t
	lock.Start = 0
	lock.Len = 0
	lock.Type = syscall.F_UNLCK
	lock.Whence = 0
	err := syscall.FcntlFlock(uintptr(l.fd), syscall.F_SETLK, &lock)
	if err != nil && err == syscall.EAGAIN {
		return ErrLocked
	}
	return err
}

// Must implements TryLockerSafe.Must.
func (l *lock) Must() TryLocker {
	return &mustLock{l}
}

func (l *lock) Destroy() error {
	return l.file.Close()
}

// Check the interfaces are satisfied
var (
	_ TryLockerSafe = &lock{}
)
