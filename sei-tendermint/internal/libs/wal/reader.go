package wal

import (
	"bufio"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
)

var errEOF = errors.New("EOF")
var errCorrupted = errors.New("file corrupted")

var crc32c = crc32.MakeTable(crc32.Castagnoli)

type logReader struct {
	file      *os.File
	buf       *bufio.Reader
	bytesLeft int64
}

func openLogReader(path string) (*logReader, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, filePerms)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &logReader{
		file:      f,
		buf:       bufio.NewReader(f),
		bytesLeft: info.Size(),
	}, nil
}

func (r *logReader) read(n int64) ([]byte, error) {
	if r.bytesLeft < n {
		return nil, errCorrupted
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r.buf, data); err != nil {
		return nil, err
	}
	r.bytesLeft -= n
	return data, nil
}

func (r *logReader) ReadEntry() (data []byte, err error) {
	// Locking files on filesystem level is not really supported.
	// Therefore it is always possible that the file gets modified while we read it.
	// Hence we return a custom EOF error, so that it is distinguishable from EOF
	// returned by the file system.
	if r.bytesLeft == 0 {
		return nil, errEOF
	}
	header, err := r.read(headerSize)
	if err != nil {
		return nil, err
	}
	wantCRC := binary.BigEndian.Uint32(header[0:4])
	data, err = r.read(int64(binary.BigEndian.Uint32(header[4:8])))
	if err != nil {
		return nil, err
	}
	if gotCRC := crc32.Checksum(data, crc32c); gotCRC != wantCRC {
		return nil, errCorrupted
	}
	return data, nil
}

// Close unconditionally releases all the resources.
func (r *logReader) Close() {
	_ = r.file.Close()
}
