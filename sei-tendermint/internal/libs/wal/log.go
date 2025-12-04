/*
		Write-ahead log using files of bounded size for storage.
	  It appends entries to the <headPath> file until it reaches the limit size.
		Then it renames it to <headPath>.<sequential number> (a tail file) and creates new empty <headPath> file.
		It uses flock on an empty <HeadPath>.lock file to ensure exclusive access to the log files.

		Dir/
		- <HeadPath>.000   // First rolled file
		- <HeadPath>.001   // Second rolled file
		- ...
		- <HeadPath>       // Head file.
		- <HeadPath>.lock  // File used as a mutex.
*/
package wal

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"

	"github.com/tendermint/tendermint/libs/utils"
)

const headerSize int64 = 8

const filePerms = os.FileMode(0600)

var ErrClosed error = errors.New("WAL closed")

type Config struct {
	FileSizeLimit  int64
	TotalSizeLimit int64
}

func DefaultConfig() *Config {
	return &Config{
		FileSizeLimit:  10 * 1024 * 1024,       // 10MB
		TotalSizeLimit: 1 * 1024 * 1024 * 1024, // 1GB
	}
}

func lockPath(headPath string) string {
	return fmt.Sprintf("%s.lock", headPath)
}

func openLockFile(headPath string) (*os.File, error) {
	guard, err := os.OpenFile(lockPath(headPath), os.O_CREATE|os.O_RDONLY, filePerms)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(guard.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		guard.Close()
		return nil, fmt.Errorf("unix.Flock(): %w", err)
	}
	return guard, nil
}

// logInner is a non-threadsafe inner implementation of Log.
// It is invalidated whenever ANY of the method call returns an error.
// Log protects access to the invalidated logInner to avoid misuse.
type logInner struct {
	cfg        *Config
	lockFile   *os.File
	view       *logView
	writer     *logWriter
}

// ReadFile reads the whole log file at a given offset. 
func (i *logInner) ReadFile(fileOffset int) ([][]byte,error) {	
	path, err := i.view.PathByOffset(fileOffset)
	if err != nil {
		return nil,err
	}
	if err:=i.writer.Sync(); err!=nil {
		return nil,fmt.Errorf("i.Sync(): %w",err)
	}
	r, err := openLogReader(path)
	if err != nil {
		return nil,err
	}
	var entries [][]byte
	for {
		entry, err := r.ReadEntry()
		if err != nil {
			if errors.Is(err, errEOF) {
				return entries,nil
			}
			return nil,fmt.Errorf("r.ReadEntry(): %w",err)
		}
		entries = append(entries,entry)
	}
}

func (i *logInner) Append(entry []byte) (err error) {
	if limit := i.cfg.FileSizeLimit; limit > 0 && i.writer.bytesSize >= limit {
		// Sync and close head.
		if err:=i.writer.Sync(); err!=nil {
			return err
		}
		i.writer.Close()
		// Move head to tail.
		if err := i.view.Rotate(i.cfg); err != nil {
			return fmt.Errorf("i.view.Rotate(): %w", err)
		}
		// Reopen head.
		writer, err := openLogWriter(i.view.headPath)
		if err!=nil {
			return fmt.Errorf("openLogWriter(): %w",err)
		}
		i.writer = writer
	}
	return i.writer.AppendEntry(entry)
}

func (i *logInner) Size() (int64, error) {
	size, err := i.view.TailSize()
	if err != nil {
		return 0, err
	}
	size += i.writer.bytesSize
	return size, nil
}

// Close releases all resources unconditionally.
// It invalidates logInner object.
func (i *logInner) Close() {
	_ = i.writer.Sync() // Best effort syncing at close. No guarantees.
	i.writer.Close()
	i.lockFile.Close()
}

// non-threadsafe WAL.
// Automatically closes the WAL if any operation returns an error.
// Locks the WAL files while opened.
type Log struct {
	inner utils.Option[*logInner]
}

func OpenLog(headPath string, cfg *Config) (*Log, error) {
	lockFile, err := openLockFile(headPath)
	if err != nil {
		return nil, fmt.Errorf("openLockFile(): %w", err)
	}
	view, err := loadLogView(headPath)
	if err != nil {
		lockFile.Close()
		return nil, fmt.Errorf("loadLogView(): %w", err)
	}
	writer,err := openLogWriter(headPath)
	if err!=nil {
		lockFile.Close()
		return nil,fmt.Errorf("openLogWriter(): %w",err)
	}
	return &Log{
		inner: utils.Some(&logInner{
			cfg:      cfg,
			lockFile: lockFile,
			view:     view,
			writer:   writer,
		}),
	},nil
}

func (l *Log) MinOffset() int {
	if inner, ok := l.inner.Get(); ok {
		return inner.view.firstIdx - inner.view.nextIdx
	}
	return 0
}

// ReadFile reads all entries from a file at a given offset.
// Available offsets are from range [MinOffset(),0]
func (l *Log) ReadFile(fileOffset int) ([][]byte, error) {
	if inner, ok := l.inner.Get(); ok {
		return inner.ReadFile(fileOffset)
	}
	return nil, ErrClosed
}

// Append appends entry to the log atomically.
// You need to call Sync afterwards to ensure that the entry is persisted.
func (l *Log) Append(entry []byte) (err error) {
	defer l.closeOnErr(&err)
	if inner, ok := l.inner.Get(); ok {
		return inner.Append(entry)
	}
	return ErrClosed
}

// Sync writes all buffered data to disk and calls fsync to ensure persistence.
func (l *Log) Sync() (err error) {
	defer l.closeOnErr(&err)
	if inner, ok := l.inner.Get(); ok {
		return inner.writer.Sync()
	}
	return ErrClosed
}

// Returns the total size of the log in bytes.
func (l *Log) Size() (res int64, err error) {
	defer l.closeOnErr(&err)
	if inner, ok := l.inner.Get(); ok {
		return inner.Size()
	}
	return 0, ErrClosed
}

// Close releases all resources unconditionally.
func (l *Log) Close() {
	if i, ok := l.inner.Get(); ok {
		i.Close()
		l.inner = utils.None[*logInner]()
	}
}

// Closes Log iff *err!=nil.
func (l *Log) closeOnErr(err *error) {
	if *err != nil {
		l.Close()
	}
}
