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
	"fmt"
	"errors"
	"os"
	"golang.org/x/sys/unix"

	"github.com/tendermint/tendermint/libs/utils"
)

const headerSize int64 = 8

const filePerms = os.FileMode(0600)

var ErrClosed error = errors.New("WAL closed")

type Config struct {
	FileSizeLimit      int64
	TotalSizeLimit     int64
}

func DefaultConfig() *Config {
	return &Config {
		FileSizeLimit:       10 * 1024 * 1024,       // 10MB
		TotalSizeLimit:      1 * 1024 * 1024 * 1024, // 1GB
	}
}

func lockPath(headPath string) string {
	return fmt.Sprintf("%s.lock",headPath)
}

func openLockFile(headPath string) (*os.File,error) {
	guard,err := os.OpenFile(lockPath(headPath),os.O_CREATE|os.O_RDONLY,filePerms)
	if err!=nil {
		return nil,err
	}
	if err:=unix.Flock(int(guard.Fd()),unix.LOCK_EX|unix.LOCK_NB); err!=nil {
		guard.Close()
		return nil, fmt.Errorf("unix.Flock(): %w",err)
	}
	return guard,nil
}

type logInner struct {
	cfg *Config
	lockFile *os.File
	view *logView
	fileOffset int
	reader utils.Option[*logReader]
	writer utils.Option[*logWriter]
}

// Read reads the next entry from the log.
// Returns io.EOF when the end of the log is reached.
func (i *logInner) Read() (res []byte, ok bool, err error) {
	for {
		reader,ok := i.reader.Get()
		if !ok { return nil, false, fmt.Errorf("not opened for read") }
		data,err := reader.ReadEntry()
		if err==nil {
			return data,true,nil
		}
		if i.fileOffset==0 {
			// Last entry of the last file may be truncated because file writes are not atomic.
			if errors.Is(err,errEOF) || errors.Is(err,errTruncated) {
				return nil,false,nil 
			}
			return nil,false,err
		} 
		if !errors.Is(err,errEOF) {
			return nil,false,err
		}
		// Open the next file and retry.
		if err:=i.OpenForRead(i.fileOffset+1); err!=nil {
			return nil,false,err
		}
	}	
}

func (i *logInner) Append(entry []byte) (err error) {
	for {
		writer,ok := i.writer.Get()
		if !ok {
			return fmt.Errorf("not opened for append")
		}
		if writer.bytesSize >= i.cfg.FileSizeLimit {
			if err:=writer.Sync(); err!=nil {
				return err
			}
			i.Reset()
			// Move head to tail.
			if err := i.view.Rotate(i.cfg); err!=nil {
				return fmt.Errorf("i.view.Rotate(): %w", err)
			}
			// Reopen for append.
			if err:=i.OpenForAppend(); err!=nil {
				return fmt.Errorf("i.OpenForAppend(): %w",err)
			}
			continue
		}
		return writer.AppendEntry(entry)
	}
}

func (i *logInner) Sync() error {
	if writer,ok := i.writer.Get(); ok {
		if err:=writer.Sync(); err!=nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("not opened for append")
}

func (i *logInner) Size() (int64,error) {
	size,err := i.view.TailSize()
	if err!=nil { return 0,err }
	if writer,ok := i.writer.Get(); ok {
		size += writer.bytesSize
	} else {
		// Head file does not have to exist.
		if fi,err:=os.Stat(i.view.headPath); err==nil {
			size += fi.Size()
		} else if !errors.Is(err,os.ErrNotExist) {
			return 0,fmt.Errorf("os.Stat(%q): %w",i.view.headPath,err)
		}
	}
	return size,nil
}

// Close releases all resources unconditionally.
func (i *logInner) Close() {
	if writer,ok := i.writer.Get(); ok {
		// Best effort syncing at close. No guarantees.
		_ = writer.Sync()
	}
	i.Reset()	
	i.lockFile.Close()
}

func (i *logInner) Reset() {
	if reader,ok := i.reader.Get(); ok {
		reader.Close()
		i.reader = utils.None[*logReader]()
	}
	if writer,ok := i.writer.Get(); ok {
		writer.Close()
		i.writer = utils.None[*logWriter]()
	}
}

func (i *logInner) OpenForAppend() error {
	i.Reset()
	w,err := openLogWriter(i.view.headPath)
	if err!=nil { return err }
	i.fileOffset = 0
	i.writer = utils.Some(w)
	return nil
}

func (i *logInner) OpenForRead(fileOffset int) error {
	i.Reset()
	path,err := i.view.PathByOffset(fileOffset)
	if err!=nil { return err }
	r,err := openLogReader(path)
	if err!=nil { return err }
	i.fileOffset = fileOffset
	i.reader = utils.Some(r)
	return nil
}

// Thread-safe WAL.
// Automatically closes the WAL if any operation returns an error.
// Holds a mutex on the WAL files while opened.
type Log struct {
	inner utils.Mutex[*utils.Option[*logInner]]
}

func OpenLog(headPath string, cfg *Config) (*Log,error) {
	lockFile,err := openLockFile(headPath)
	if err!=nil { return nil,fmt.Errorf("openLockFile(): %w",err) }
	view,err := loadLogView(headPath)
	if err!=nil {
		lockFile.Close()
		return nil,fmt.Errorf("loadLogView(): %w",err)
	}
	return &Log {
		inner: utils.NewMutex(utils.Alloc(utils.Some(&logInner {
			cfg: cfg,
			lockFile: lockFile,
			view: view,
		}))),
	},nil
}

func (l *Log) MinOffset() int {
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.view.firstIdx-inner.view.nextIdx
		}
	}
	return 0
}

// Opens the WAL for reading at a given offset (in files from the END of the log)
// Available offsets are in range [-n,0], where n is the number of fails in tail.
// Returns ErrBadOffset if fileOffset is outside of that range.
func (l *Log) OpenForRead(fileOffset int) (err error) {
	defer l.closeOnErr(&err)
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.OpenForRead(fileOffset)
		}
	}
	return ErrClosed
}

func (l *Log) Read() (res []byte, ok bool, err error) {
	defer l.closeOnErr(&err)
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.Read()
		}
	}
	return nil, false, ErrClosed
}

// Opens WAL for appending. 
func (l *Log) OpenForAppend() (err error) {
	defer l.closeOnErr(&err)
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.OpenForAppend()
		}
	}
	return ErrClosed
}

// Write writes entry to the log atomically. You need to call Sync afterwards
// to ensure that the write is persisted.
func (l *Log) Append(entry []byte) (err error) {
	defer l.closeOnErr(&err)
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.Append(entry)
		}
	}
	return ErrClosed
}

// Sync writes all buffered data to disk and calls fsync to ensure persistence.
func (l *Log) Sync() error {
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.Sync()
		}
	}
	return ErrClosed
}

// Returns the total size of the log in bytes.
func (l *Log) Size() (res int64,err error) {
	defer l.closeOnErr(&err)
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.Size()
		}
	}
	return 0,ErrClosed
}

// Close releases all resources unconditionally.
func (l *Log) Close() {
	for inner := range l.inner.Lock() {
		if i,ok := inner.Get(); ok {
			i.Close()
			*inner = utils.None[*logInner]()
		}
	}
}

// Closes Log iff *err!=nil.
func (l *Log) closeOnErr(err *error) {
	if *err!=nil {
		l.Close()
	}
}
