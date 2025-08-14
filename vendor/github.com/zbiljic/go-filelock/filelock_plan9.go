// +build plan9

package filelock

import (
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type lock struct {
	path string
	file *os.File
}

// New creates a new lock
func New(path string) (TryLockerSafe, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrNeedAbsPath
	}
	l := &lock{path}
	return l, nil
}

func (l *lock) String() string {
	return filepath.Base(l.path)
}

// TryLock acquires exclusivity on the lock without blocking
func (l *lock) TryLock() (bool, error) {
	err := os.Chmod(l.path, syscall.DMEXCL|privateFileMode)
	if err != nil {
		return false, err
	}

	f, err := os.Open(l.path)
	if err != nil {
		return false, ErrLocked
	}

	l.file = f
	return true, nil
}

// Lock acquires exclusivity on the lock with blocking
func (l *lock) Lock() error {
	err := os.Chmod(l.path, syscall.DMEXCL|privateFileMode)
	if err != nil {
		return err
	}

	for {
		f, err := os.Open(l.path)
		if err == nil {
			l.file = f
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Unlock unlocks the lock
func (l *lock) Unlock() error {
	return l.file.Close()
}

// Must implements TryLockerSafe.Must.
func (l *lock) Must() TryLocker {
	return &mustLock{l}
}

func (l *lock) Destroy() error {
	return nil
}

// Check the interfaces are satisfied
var (
	_ TryLockerSafe = &lock{}
)
