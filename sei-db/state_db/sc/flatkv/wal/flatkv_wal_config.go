package wal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// Configuration for a flatKV WAL.
type FlatKVWALConfig struct {
	// The directory where the WAL writes its files.
	Path string

	// The size of the channel used to send work from the caller to the serialization goroutine.
	RequestBufferSize uint

	// The size of the channel used to send serialized records from the serialization goroutine to the
	// writer goroutine.
	WriteBufferSize uint

	// The size a WAL file may reach before it is sealed and a fresh one is opened. Rotation only happens on
	// block boundaries, so a file may exceed this by the size of a single block. Must be greater than 0.
	TargetFileSize uint

	// When true, Flush calls fsync on the underlying file so that flushed data survives a power loss, not
	// merely a process crash. When false, Flush only flushes the in-process buffer to the OS.
	FsyncOnFlush bool
}

// Constructor for a default flatKV WAL configuration.
func DefaultFlatKVWALConfig(path string) *FlatKVWALConfig {
	return &FlatKVWALConfig{
		Path:              path,
		RequestBufferSize: 16,
		WriteBufferSize:   16,
		TargetFileSize:    64 * unit.MB,
		FsyncOnFlush:      true,
	}
}

// Validate the configuration, returning nil if valid, or an error describing the problem if invalid.
func (c *FlatKVWALConfig) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("path is required")
	}
	if c.TargetFileSize == 0 {
		// A zero target would seal and rotate a fresh file after every single block.
		return fmt.Errorf("target file size must be greater than 0")
	}
	return nil
}
