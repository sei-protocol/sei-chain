package autofile

import (
	"bufio"
	"errors"
	"hash/crc32"
	"encoding/binary"
	"os"
	"fmt"
)

var errEOF = errors.New("EOF")
var errTruncated = errors.New("file truncated")
var errCorrupted = errors.New("file corrupted")

var crc32c = crc32.MakeTable(crc32.Castagnoli)

type fileWriter struct {
	err error
	file *os.File
	buf *bufio.Writer
	bytesSize int64
}

func verifyFile(path string) (int64,error) {
	r,err := newFileReader(path)
	if err!=nil { return 0,err }
	defer r.Close()
	totalSize := r.bytesLeft
	realSize := int64(0) 
	for {
		if _,err := r.Read(); err!=nil {
			if errors.Is(err,errEOF) || errors.Is(err,errTruncated) {
				 return realSize,nil
			}
			return 0,err
		}
		realSize = totalSize-r.bytesLeft
	}
}

func newFileWriter(path string) (res *fileWriter, resErr error) {
	// Read the whole file and if the last entry is truncated, remove it.
	realSize,err := verifyFile(path)
	if err!=nil { return nil,err }
	f,err := os.OpenFile(path,os.O_WRONLY,filePerms)
	if err!=nil { return nil,err }
	defer func() { if resErr!=nil { f.Close() } }()
	if err:=f.Truncate(realSize); err!=nil {
		return nil,fmt.Errorf("f.Truncate(): %w",err)
	}
	if err!=nil { return nil,err }
	return &fileWriter {
		file: f,
		buf: bufio.NewWriterSize(f, 4096*10),
		bytesSize: realSize,
	},nil
}

func (w *fileWriter) Write(data []byte) (err error) {
	if w.err!=nil { return err }
	defer func() { if err!=nil { w.err = err } }()
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], crc32.Checksum(data, crc32c))
	binary.BigEndian.PutUint32(header[4:8], uint32(len(data)))
	if _,err := w.buf.Write(header[:]); err!=nil {
		return err
	}
	if _,err := w.buf.Write(data); err!=nil {
		return err
	}
	return nil
}

func (w *fileWriter) Sync() (err error) {
	if w.err!=nil { return err }
	defer func() { if err!=nil { w.err = err } }()
	if err:=w.buf.Flush(); err!=nil { return err }
	return w.file.Sync()
}

func (w *fileWriter) Close() {
	w.file.Close()
}

type fileReader struct {
	err error
	file *os.File
	buf *bufio.Reader
	bytesLeft int64
}

func newFileReader(path string) (*fileReader,error) {
	f,err := os.OpenFile(path,os.O_CREATE|os.O_RDONLY,filePerms)
	if err!=nil { return nil,err }
	info,err := f.Stat()
	if err!=nil {
		f.Close()
		return nil,err
	}
	return &fileReader {
		file: f,
		buf: bufio.NewReader(f),
		bytesLeft: info.Size(),
	},nil
}

func (r *fileReader) read(n int64) ([]byte,error) {
	if r.bytesLeft < n {
		return nil,errTruncated
	}
	data := make([]byte,n)
	if _, err := r.buf.Read(data); err!=nil {
		return nil,err
	}
	r.bytesLeft -= n 
	return data,nil
}

func (r *fileReader) Read() (data []byte, err error) {
	// Cache the last error to prevent reading from a broken file.
	if r.err!=nil { return nil,r.err }
	defer func(){ if err!=nil { r.err = err } }()
	// Locking files on filesystem level is not really supported.
	// Therefore it is always possible that the file gets modified while we read it.
	// Hence we return a custom EOF error, so that it is distinguishable from EOF
	// returned by the file system.
	if r.bytesLeft == 0 {
		return nil,errEOF
	}
	header,err := r.read(8)
	if err!=nil { return nil,err }
	wantCRC := binary.BigEndian.Uint32(header[:4])
	data,err = r.read(int64(binary.BigEndian.Uint32(header[4:])))
	if err!=nil { return nil,err }
	if gotCRC := crc32.Checksum(data, crc32c); gotCRC!=wantCRC {
		return nil,errCorrupted
	}
	return data,nil
}

func (r *fileReader) Close() {
	_ = r.file.Close()
}


