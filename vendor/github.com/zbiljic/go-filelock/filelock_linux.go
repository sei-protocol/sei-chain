// +build linux

package filelock

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// This used to call syscall.Flock() but that call fails with EBADF on NFS.
// An alternative is lockf() which works on NFS but that call lets a process
// lock the same file twice. Instead, use Linux's non-standard open file
// descriptor locks which will block if the process already holds the file lock.
//
// constants from /usr/include/bits/fcntl-linux.h
const (
	F_OFD_GETLK  = 37
	F_OFD_SETLK  = 37
	F_OFD_SETLKW = 38
)

var (
	wrlck = syscall.Flock_t{
		Type:   syscall.F_WRLCK,
		Whence: int16(io.SeekStart),
		Start:  0,
		Len:    0,
	}

	unlck = syscall.Flock_t{
		Type:   syscall.F_UNLCK,
		Whence: int16(io.SeekStart),
		Start:  0,
		Len:    0,
	}

	linuxTryLockFile = flockTryLockFile
	linuxLockFile    = flockLockFile
	linuxUnlockFile  = flockUnlockFile
)

func init() {
	// use open file descriptor locks if the system supports it
	getlk := syscall.Flock_t{Type: syscall.F_RDLCK}
	if err := syscall.FcntlFlock(0, F_OFD_GETLK, &getlk); err == nil {
		linuxTryLockFile = ofdTryLockFile
		linuxLockFile = ofdLockFile
		linuxUnlockFile = ofdUnlockFile
	}
}

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
	file, err := open(path, os.O_CREATE|os.O_RDWR)
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
	return linuxTryLockFile(l.fd)
}

// Lock acquires exclusivity on the lock without blocking
func (l *lock) Lock() error {
	return linuxLockFile(l.fd)
}

// Unlock unlocks the lock
func (l *lock) Unlock() error {
	return linuxUnlockFile(l.fd)
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
	f, err := os.OpenFile(path, flag, privateFileMode)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func ofdTryLockFile(fd int) (bool, error) {
	flock := wrlck
	if err := syscall.FcntlFlock(uintptr(fd), F_OFD_SETLK, &flock); err != nil {
		if err == syscall.EWOULDBLOCK {
			return false, ErrLocked
		}
		return false, err
	}
	return true, nil
}

func ofdLockFile(fd int) error {
	flock := wrlck
	return syscall.FcntlFlock(uintptr(fd), F_OFD_SETLKW, &flock)
}

func ofdUnlockFile(fd int) error {
	flock := unlck
	return syscall.FcntlFlock(uintptr(fd), F_OFD_SETLKW, &flock)
}

// Check the interfaces are satisfied
var (
	_ TryLockerSafe = &lock{}
)
