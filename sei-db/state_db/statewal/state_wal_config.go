package statewal

import (
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

// Configuration for a state WAL.
type Config struct {
	// The directory where the WAL writes its files.
	Path string

	// The size of the channel used to send work from the caller to the serialization goroutine.
	RequestBufferSize uint

	// The size of the channel used to send framed records from the underlying WAL's serialization to its
	// writer goroutine.
	WriteBufferSize uint

	// The size a WAL file may reach before it is sealed and a fresh one is opened. Because each block is
	// written as a single record, a file may exceed this by the size of one block's serialized changesets.
	// Must be greater than 0.
	TargetFileSize uint

	// When true, Flush calls fsync on the underlying file so that flushed data survives a power loss, not
	// merely a process crash. When false, Flush only flushes the in-process buffer to the OS.
	FsyncOnFlush bool

	// The number of blocks an iterator's reader thread may prefetch ahead of the consumer. A larger value
	// keeps the reader busy while the consumer processes blocks, which matters for startup replay speed.
	// Must be greater than 0.
	IteratorPrefetchSize uint
}

// Constructor for a default state WAL configuration.
func DefaultConfig(path string) *Config {
	s := seiwal.DefaultConfig(path)
	return &Config{
		Path:                 path,
		RequestBufferSize:    16,
		WriteBufferSize:      s.WriteBufferSize,
		TargetFileSize:       s.TargetFileSize,
		FsyncOnFlush:         s.FsyncOnFlush,
		IteratorPrefetchSize: s.IteratorPrefetchSize,
	}
}

// Validate the configuration, returning nil if valid, or an error describing the problem if invalid.
func (c *Config) Validate() error {
	return c.toSeiwalConfig().Validate()
}

// toSeiwalConfig maps this configuration onto the underlying generic WAL's configuration.
func (c *Config) toSeiwalConfig() *seiwal.Config {
	return &seiwal.Config{
		Path:                 c.Path,
		WriteBufferSize:      c.WriteBufferSize,
		TargetFileSize:       c.TargetFileSize,
		FsyncOnFlush:         c.FsyncOnFlush,
		IteratorPrefetchSize: c.IteratorPrefetchSize,
	}
}
