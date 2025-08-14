// +build !windows,!plan9,!solaris,!linux

package filelock

import (
	"fmt"
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
	file, err := open(path, os.O_CREATE|os.O_RDONLY)
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
	return flockTryLockFile(l.fd)
}

// Lock acquires exclusivity on the lock without blocking
func (l *lock) Lock() error {
	return flockLockFile(l.fd)
}

// Unlock unlocks the lock
func (l *lock) Unlock() error {
	return flockUnlockFile(l.fd)
}

// Must implements TryLockerSafe.Must.
func (l *lock) Must() TryLocker {
	return &mustLock{l}
}

func (l *lock) Destroy() error {
	return l.file.Close()
}

func open(path string, flag int) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("cannot open empty filename")
	}
	fd, err := syscall.Open(path, flag, privateFileMode)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// Check the interfaces are satisfied
var (
	_ TryLockerSafe = &lock{}
)
