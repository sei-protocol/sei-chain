/*
You can open a Group to keep restrictions on an AutoFile, like
the maximum size of each chunk, and/or the total amount of bytes
stored in the group.

The first file to be written in the Group.Dir is the head file.

	Dir/
	- <HeadPath>

Once the Head file reaches the size limit, it will be rotated.

	Dir/
	- <HeadPath>.000   // First rolled file
	- <HeadPath>       // New head path, starts empty.
										 // The implicit index is 001.

As more files are written, the index numbers grow...

	Dir/
	- <HeadPath>.000   // First rolled file
	- <HeadPath>.001   // Second rolled file
	- ...
	- <HeadPath>       // New head path

The Group can also be used to binary-search for some line,
assuming that marker lines are written occasionally.
*/
package wal 

import (
	"fmt"
	"io"
	"errors"
	"os"
	"golang.org/x/sys/unix"

	"github.com/tendermint/tendermint/libs/utils"
)

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
	if err:=unix.Flock(int(guard.Fd()),unix.LOCK_EX); err!=nil {
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
func (i *logInner) Read() (res []byte, err error) {
	defer func(){ if err!=nil { i.Reset() } }()
	for {
		reader,ok := i.reader.Get()
		if !ok { return nil, fmt.Errorf("not opened for read") }
		data,err := reader.ReadEntry()
		if err==nil {
			return data,nil
		}
		if i.fileOffset==0 {
			// Last entry of the last file may be truncated because file writes are not atomic.
			// TODO(gprusak): they COULD be atomic, if we used O_APPEND when writing AND used custom buffering.
			if errors.Is(err,errEOF) || errors.Is(err,errTruncated) {
				return nil,io.EOF 
			}
			return nil,err
		} 
		if !errors.Is(err,errEOF) {
			return nil,err
		}
		// Open the next file and retry.
		if err:=i.OpenForRead(i.fileOffset+1); err!=nil {
			return nil,err
		}
	}	
}

func (i *logInner) Append(entry []byte) (err error) {
	if writer,ok := i.writer.Get(); ok {
		if err:=writer.AppendEntry(entry); err!=nil {
			i.Reset()
			return err
		}
		return nil
	}
	return fmt.Errorf("not opened for append")
}

func (i *logInner) Sync() error {
	if writer,ok := i.writer.Get(); ok {
		if err:=writer.Sync(); err!=nil {
			i.Reset()
			return err
		}
		return nil
	}
	return fmt.Errorf("not opened for append")
}

// Close releases all resources unconditionally.
func (i *logInner) Close() {
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

// Thread-safe WAL
type Log struct {
	inner utils.Mutex[*utils.Option[*logInner]]
}

func NewLog(headPath string, cfg *Config) (*Log,error) {
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

// Opens the WAL for reading at a given offset (in files from the END of the log)
// Available offsets are in range [-n,0], where n is the number of fails in tail.
// Returns ErrBadOffset if fileOffset is outside of that range.
func (l *Log) OpenForRead(fileOffset int) error {
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.OpenForRead(fileOffset)
		}
	}
	return ErrClosed
}

func (l *Log) Read() ([]byte,error) {
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.Read()
		}
	}
	return nil, ErrClosed
}

func (l *Log) OpenForAppend() error {
	for inner := range l.inner.Lock() {
		if inner,ok := inner.Get(); ok {
			return inner.OpenForAppend()
		}
	}
	return ErrClosed
}

// Write writes entry to the log atomically. You need to call Sync afterwards
// to ensure that the write is persisted.
func (l *Log) Append(entry []byte) error {
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

// Close releases all resources unconditionally.
func (l *Log) Close() {
	for inner := range l.inner.Lock() {
		if i,ok := inner.Get(); ok {
			i.Close()
			*inner = utils.None[*logInner]()
		}
	}
}
