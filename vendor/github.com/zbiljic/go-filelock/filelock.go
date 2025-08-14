package filelock

import (
	"errors"
	"fmt"
	"sync"
)

const (
	// privateFileMode grants owner to read/write a file.
	privateFileMode = 0600
	// privateDirMode grants owner to make/remove files inside the directory.
	privateDirMode = 0700
)

// Various errors returned by this package
var (
	ErrNeedAbsPath = errors.New("absolute file path must be provided")
	// ErrLocked is returned if the backing file is already locked by some other
	// process.
	ErrLocked = errors.New("file already locked")
)

// TryLocker is a sync.Locker augmented with TryLock.
type TryLocker interface {
	sync.Locker
	// TryLock attempts to grab the lock, but does not hang if the lock is
	// actively held by another process.  Instead, it returns false.
	TryLock() bool
}

// TryLockerSafe is like TryLocker, but the methods can return an error and
// never panic.
type TryLockerSafe interface {
	fmt.Stringer
	// TryLock attempts to grab the lock, but does not hang if the lock is
	// actively held by another process.  Instead, it returns false.
	TryLock() (bool, error)
	// Lock blocks until it's able to grab the lock.
	Lock() error
	// Unlock releases the lock.  Should only be called when the lock is
	// held.
	Unlock() error
	// Must returns a TryLocker whose Lock, TryLock and Unlock methods
	// panic rather than return errors.
	Must() TryLocker
	// Destroy cleans up any possible resources.
	Destroy() error
}

type mustLock struct {
	l TryLockerSafe
}

// Lock implements sync.Locker.Lock.
func (m *mustLock) Lock() {
	if err := m.l.Lock(); err != nil {
		panic(err)
	}
}

// TryLock implements TryLocker.TryLock.
func (m *mustLock) TryLock() bool {
	got, err := m.l.TryLock()
	if err != nil {
		panic(err)
	}
	return got
}

// Unlock implements sync.Locker.Unlock.
func (m *mustLock) Unlock() {
	if err := m.l.Unlock(); err != nil {
		panic(err)
	}
}

// Check the interfaces are satisfied
var (
	_ TryLocker = &mustLock{}
)
