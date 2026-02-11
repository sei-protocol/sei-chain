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

// NewMmap opens the file and creates a read-only memory mapping with MADV_RANDOM.
// MADV_RANDOM disables kernel readahead, which is optimal for the B+ tree random
// access patterns used during WAL replay and normal serving.
// Page cache warming is handled separately by prefetchSnapshot() using file I/O.
func NewMmap(path string) (*MmapFile, error) {
	mmapFile, err := newMmapFile(path)
	if err != nil {
		return nil, err
	}
	mmapFile.PrepareForRandomRead()
	return mmapFile, nil
}

// newMmapFile is the shared implementation that creates an mmap without any madvise hints.
func newMmapFile(path string) (*MmapFile, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	data, handle, err := Mmap(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	return &MmapFile{
		file:   file,
		data:   data,
		handle: handle,
	}, nil
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
