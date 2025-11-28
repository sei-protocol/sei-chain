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
	err error
	cfg *Config
	lockFile *os.File
	view *logView
	fileOffset int
	reader utils.Option[*logReader]
	writer utils.Option[*logWriter]
}

// Read reads the next entry from the log.
// Returns io.EOF when the end of the log is reached.
func (i *logInner) Read() ([]byte,error) {
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
		return writer.AppendEntry(entry)
	}
	return fmt.Errorf("not opened for append")
}

func (i *logInner) Sync() error {
	if writer,ok := i.writer.Get(); ok {
		return writer.Sync()
	}
	return fmt.Errorf("not opened for append")
}

// Close releases all resources unconditionally.
func (i *logInner) Close() {
	if reader,ok := i.reader.Get(); ok {
		reader.Close()
	}
	if writer,ok := i.writer.Get(); ok {
		writer.Close()
	}
	i.lockFile.Close()
}

func (i *logInner) Reset() error {
	if reader,ok := i.reader.Get(); ok {
		reader.Close()
		i.reader = utils.None[*logReader]()
	}
	if writer,ok := i.writer.Get(); ok {
		if err:=writer.Sync(); err!=nil {
			return err
		}
		writer.Close()
		i.writer = utils.None[*logWriter]()
	}
	return nil
}

func (i *logInner) OpenForAppend() error {
	if err:=i.Reset(); err!=nil { return err }
	w,err := openLogWriter(i.view.headPath)
	if err!=nil { return err }
	i.fileOffset = 0
	i.writer = utils.Some(w)
	return nil
}

func (i *logInner) OpenForRead(fileOffset int) error {
	path,err := i.view.PathByOffset(fileOffset)
	if err!=nil { return err }
	if err:=i.Reset(); err!=nil { return err }
	r,err := openLogReader(path)
	if err!=nil { return err }
	i.fileOffset = fileOffset
	i.reader = utils.Some(r)
	return nil
}

// Thread-safe log
type Log struct {
	inner utils.Mutex[*logInner]
}

func (l *Log) OpenForRead(offset int) error {
	for inner := range l.inner.Lock() {
		if inner.err!=nil { return inner.err }
		inner.err = inner.OpenForRead(offset)
		return inner.err
	}
	panic("unreachable")
}

func (l *Log) Read() ([]byte,error) {
	for inner := range l.inner.Lock() {
		if inner.err!=nil { return nil, inner.err }
		res,err := inner.Read()
		inner.err = err
		return res,err
	}
	panic("unreachable")
}

func (l *Log) OpenForAppend() error {
	for inner := range l.inner.Lock() {
		if inner.err!=nil { return inner.err }
		inner.err = inner.OpenForAppend()
		return inner.err
	}
	panic("unreachable")
}

// Write writes entry to the log atomically. You need to call Sync afterwards
// to ensure that the write is persisted.
func (l *Log) Append(entry []byte) error {
	for inner := range l.inner.Lock() {
		if inner.err!=nil { return inner.err }
		inner.err = inner.Append(entry)
		return inner.err
	}
	panic("unreachable")
}

// Sync writes all buffered data to disk and calls fsync to ensure persistence.
func (l *Log) Sync() error {
	for inner := range l.inner.Lock() {
		if inner.err!=nil { return inner.err }
		inner.err = inner.Sync()
		return inner.err
	}
	panic("unreachable")
}

// Close releases all resources unconditionally.
func (l *Log) Close() {
	for inner := range l.inner.Lock() {
		inner.Close()
	}
}
