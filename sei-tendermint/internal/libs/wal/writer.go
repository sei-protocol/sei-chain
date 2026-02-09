package wal

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

type logWriter struct {
	file      *os.File
	buf       *bufio.Writer
	bytesSize int64
}

// Returns the size of the file, ignoring the last truncated entry.
// WARNING it needs to read the whole file.
func realFileSize(path string) (int64, error) {
	r, err := openLogReader(path)
	if err != nil {
		return 0, err
	}
	defer r.Close()
	totalSize := r.bytesLeft
	realSize := int64(0)
	for {
		if _, err := r.ReadEntry(); err != nil {
			if errors.Is(err, errEOF) || errors.Is(err, errCorrupted) {
				return realSize, nil
			}
			return 0, err
		}
		realSize = totalSize - r.bytesLeft
	}
}

func sync(path string) error {
	dirFile, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer func() { _ = dirFile.Close() }()
	return dirFile.Sync()
}

func openLogWriter(path string) (res *logWriter, resErr error) {
	// Read the whole file and find the non-corrupted prefix.
	realSize, err := realFileSize(path)
	if err != nil {
		return nil, fmt.Errorf("realFileSize(): %w", err)
	}
	// Sync the directory containing the file:
	// realFileSize() may have created a file if it didn't exist.
	// In that case we need the directory synced, so that file's inode
	// is not lost in case of crash.
	if err := sync(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("sync(directory): %w", err)
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY, filePerms)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resErr != nil {
			_ = f.Close()
		}
	}()
	// Truncate the file to non-corrupted prefix and sync.
	// It is still not 100% corruption-proof,
	// but we would need a rolling checksums to fix that,
	// which would require redesigning the WAL format.
	if err := f.Truncate(realSize); err != nil {
		return nil, fmt.Errorf("f.Truncate(): %w", err)
	}
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("f.Sync(): %w", err)
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return nil, fmt.Errorf("f.SeekEnd(): %w", err)
	}
	return &logWriter{
		file:      f,
		buf:       bufio.NewWriterSize(f, 4096*10),
		bytesSize: realSize,
	}, nil
}

func (w *logWriter) AppendEntry(entry []byte) (err error) {
	var header [headerSize]byte
	binary.BigEndian.PutUint32(header[0:4], crc32.Checksum(entry, crc32c))
	binary.BigEndian.PutUint32(header[4:8], uint32(len(entry))) //nolint:gosec // WAL entries are bounded by max message size; no overflow risk
	if _, err := w.buf.Write(header[:]); err != nil {
		return err
	}
	if _, err := w.buf.Write(entry); err != nil {
		return err
	}
	w.bytesSize += int64(len(header) + len(entry))
	return nil
}

func (w *logWriter) Sync() (err error) {
	if err := w.buf.Flush(); err != nil {
		return err
	}
	return w.file.Sync()
}

// Close unconditionally releases all the resources.
func (w *logWriter) Close() {
	_ = w.file.Close()
}
