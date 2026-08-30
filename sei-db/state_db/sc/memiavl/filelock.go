package memiavl

import (
	"path/filepath"

	"github.com/zbiljic/go-filelock"
)

type FileLock interface {
	Unlock() error
	Destroy() error
}

func LockFile(fname string) (FileLock, error) {
	path, err := filepath.Abs(fname)
	if err != nil {
		return nil, err
	}
	fl, err := filelock.New(path)
	if err != nil {
		return nil, err
	}
	if _, err := fl.TryLock(); err != nil {
		return nil, err
	}

	return fl, nil
}
