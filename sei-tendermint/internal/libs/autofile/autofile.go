package autofile

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

const (
	autoFilePerms       = os.FileMode(0600)
)

// ErrAutoFileClosed is reported when operations attempt to use an autofile
// after it has been closed.
var ErrAutoFileClosed = errors.New("autofile is closed")

// AutoFile automatically closes and re-opens file for writing. The file is
// automatically setup to close itself every 1s and upon receiving SIGHUP.
//
// This is useful for using a log file with the logrotate tool.
type AutoFile struct {
	ID   string
	Path string

	mtx    sync.Mutex // guards the fields below
	file   *os.File   // the underlying file (may be nil)
}

// OpenAutoFile creates an AutoFile in the path (with random ID). If there is
// an error, it will be of type *PathError or *ErrPermissionsChanged (if file's
// permissions got changed (should be 0600)).
func OpenAutoFile(path string) (*AutoFile, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, autoFilePerms)
	if err != nil {
		return nil, err
	}
	return &AutoFile{
		ID:          tmrand.Str(12) + ":" + path,
		Path:        path,
		file: file,
	},nil
}

// Close shuts down the service goroutine and marks af as invalid.  Operations
// on af after Close will report an error.
func (af *AutoFile) Close() error {
	return af.withLock(func() error {
		if af.file==nil { return nil }
		af.file.Close()
		af.file = nil
		return nil
	})
}

// withLock runs f while holding af.mtx, and reports any error it returns.
func (af *AutoFile) withLock(f func() error) error {
	af.mtx.Lock()
	defer af.mtx.Unlock()
	return f()
}

// Write writes len(b) bytes to the AutoFile. It returns the number of bytes
// written and an error, if any. Write returns a non-nil error when n !=
// len(b).
// Opens AutoFile if needed.
func (af *AutoFile) Write(b []byte) (int, error) {
	af.mtx.Lock()
	defer af.mtx.Unlock()
	return af.file.Write(b)
}

// Sync commits the current contents of the file to stable storage. Typically,
// this means flushing the file system's in-memory copy of recently written
// data to disk.
func (af *AutoFile) Sync() error {
	return af.withLock(func() error {
		return af.file.Sync()
	})
}

// Size returns the size of the AutoFile. It returns -1 and an error if fails
// get stats or open file.
// Opens AutoFile if needed.
func (af *AutoFile) Size() (int64, error) {
	af.mtx.Lock()
	defer af.mtx.Unlock()
	if af.file==nil { return 0,ErrAutoFileClosed }
	stat, err := af.file.Stat()
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}
