package wal

import (
	"bufio"
	"errors"
	"hash/crc32"
	"encoding/binary"
	"os"
	"fmt"
)

type logWriter struct {
	file *os.File
	buf *bufio.Writer
	bytesSize int64
}

// Returns the size of the file, ignoring the last truncated entry.
// WARNING it needs to read the whole file.
func realFileSize(path string) (int64,error) {
	r,err := openLogReader(path)
	if err!=nil { return 0,err }
	defer r.Close()
	totalSize := r.bytesLeft
	realSize := int64(0) 
	for {
		if _,err := r.ReadEntry(); err!=nil {
			if errors.Is(err,errEOF) || errors.Is(err,errTruncated) {
				 return realSize,nil
			}
			return 0,err
		}
		realSize = totalSize-r.bytesLeft
	}
}

func openLogWriter(path string) (res *logWriter, resErr error) {
	// Read the whole file and if the last entry is truncated, remove it.
	realSize,err := realFileSize(path)
	if err!=nil { return nil,err }
	f,err := os.OpenFile(path,os.O_WRONLY,filePerms)
	if err!=nil { return nil,err }
	defer func() { if resErr!=nil { f.Close() } }()
	if err:=f.Truncate(realSize); err!=nil {
		return nil,fmt.Errorf("f.Truncate(): %w",err)
	}
	if err!=nil { return nil,err }
	return &logWriter {
		file: f,
		buf: bufio.NewWriterSize(f, 4096*10),
		bytesSize: realSize,
	},nil
}

func (w *logWriter) AppendEntry(entry []byte) (err error) {
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], crc32.Checksum(entry, crc32c))
	binary.BigEndian.PutUint32(header[4:8], uint32(len(entry)))
	if _,err := w.buf.Write(header[:]); err!=nil {
		return err
	}
	if _,err := w.buf.Write(entry); err!=nil {
		return err
	}
	w.bytesSize += int64(len(header) + len(entry))
	return nil
}

func (w *logWriter) Sync() (err error) {
	if err:=w.buf.Flush(); err!=nil { return err }
	return w.file.Sync()
}

// Close unconditionally releases all the resources.
func (w *logWriter) Close() {
	w.file.Close()
}


