package memiavl

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/ledgerwatch/erigon-lib/mmap"
	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
)

// MmapFile manage the resources of a mmap-ed file
type MmapFile struct {
	file *os.File
	data []byte
	// mmap handle for windows (this is used to close mmap)
	handle *[mmap.MaxMapSize]byte
}

// NewMmap opens the file and creates a read-only memory mapping.
// By default, applies MADV_SEQUENTIAL + MADV_WILLNEED to enable aggressive kernel readahead.
// This significantly improves cold-start replay performance (5-6x speedup).
func NewMmap(path string) (*MmapFile, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	data, handle, err := Mmap(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	mmapFile := &MmapFile{
		file:   file,
		data:   data,
		handle: handle,
	}

	// Apply madvise hints for optimal replay performance
	// This enables kernel readahead and reduces page fault latency
	mmapFile.PrepareForSequentialRead()

	return mmapFile, nil
}

func (m *MmapFile) PrepareForSequentialRead() {
	if len(m.data) > 0 {
		// Set SEQUENTIAL + WILLNEED for optimal sequential access with kernel readahead
		_ = unix.Madvise(m.data, unix.MADV_SEQUENTIAL)
		_ = unix.Madvise(m.data, unix.MADV_WILLNEED)
	}
}

func (m *MmapFile) PrepareForRandomRead() {
	if len(m.data) > 0 {
		// Switch to RANDOM access mode to disable readahead for random access patterns
		_ = unix.Madvise(m.data, unix.MADV_RANDOM)
	}
}

// Close closes the file and mmap handles
func (m *MmapFile) Close() error {
	var err error
	if m.handle != nil {
		err = mmap.Munmap(m.data, m.handle)
	}
	return errors.Join(err, m.file.Close())
}

// Data returns the mmap-ed buffer
func (m *MmapFile) Data() []byte {
	return m.data
}

func Mmap(f *os.File) ([]byte, *[mmap.MaxMapSize]byte, error) {
	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return nil, nil, err
	}

	return mmap.Mmap(f, int(fi.Size()))
}
