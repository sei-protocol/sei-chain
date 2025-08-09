// +build windows

package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")

	errLocked = errors.New("The process cannot access the file because another process has locked a portion of the file.")
)

const (
	// see https://msdn.microsoft.com/en-us/library/windows/desktop/aa365203(v=vs.85).aspx
	flagLockExclusive       = 2
	flagLockFailImmediately = 1

	// see https://msdn.microsoft.com/en-us/library/windows/desktop/ms681382(v=vs.85).aspx
	errLockViolation syscall.Errno = 0x21
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
	file, err := open(path, os.O_WRONLY)
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
	return lockFile(syscall.Handle(l.fd), flagLockFailImmediately)
}

// Lock acquires exclusivity on the lock without blocking
func (l *lock) Lock() error {
	if _, err := lockFile(syscall.Handle(l.fd), 0); err != nil {
		return err
	}
	return nil
}

// Unlock unlocks the lock
func (l *lock) Unlock() error {
	err := unlockFileEx(syscall.Handle(l.fd), 0, 1, 0, &syscall.Overlapped{})
	return err
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
	var access uint32
	switch flag {
	case syscall.O_RDONLY:
		access = syscall.GENERIC_READ
	case syscall.O_WRONLY:
		access = syscall.GENERIC_WRITE
	case syscall.O_RDWR:
		access = syscall.GENERIC_READ | syscall.GENERIC_WRITE
	case syscall.O_WRONLY | syscall.O_CREAT:
		access = syscall.GENERIC_ALL
	default:
		panic(fmt.Errorf("flag %v is not supported", flag))
	}
	fd, err := syscall.CreateFile(&(syscall.StringToUTF16(path)[0]),
		access,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func lockFile(fd syscall.Handle, flags uint32) (bool, error) {
	var flag uint32 = flagLockExclusive
	flag |= flags
	if fd == syscall.InvalidHandle {
		return true, nil
	}
	err := lockFileEx(fd, flag, 0, 1, 0, &syscall.Overlapped{})
	if err == nil {
		return true, nil
	} else if err.Error() == errLocked.Error() {
		return false, ErrLocked
	} else if err != errLockViolation {
		return false, err
	}
	return true, nil
}

func lockFileEx(h syscall.Handle, flags, reserved, locklow, lockhigh uint32, ol *syscall.Overlapped) (err error) {
	r1, _, e1 := syscall.Syscall6(procLockFileEx.Addr(), 6, uintptr(h), uintptr(flags), uintptr(reserved), uintptr(locklow), uintptr(lockhigh), uintptr(unsafe.Pointer(ol)))
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func unlockFileEx(h syscall.Handle, reserved, locklow, lockhigh uint32, ol *syscall.Overlapped) (err error) {
	r1, _, e1 := syscall.Syscall6(procUnlockFileEx.Addr(), 5, uintptr(h), uintptr(reserved), uintptr(locklow), uintptr(lockhigh), uintptr(unsafe.Pointer(ol)), 0)
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

// Check the interfaces are satisfied
var (
	_ TryLockerSafe = &lock{}
)
