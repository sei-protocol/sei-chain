package hashlog

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// Configuration for a HashLogger.
type HashLoggerConfig struct {
	// The location where the HashLogger writes its output.
	Path string

	// The software version currently running on this node.
	Version string

	// The size of the channel for sending work to the hasher thread.
	HashBufferSize uint

	// The size of the channel for sending work to the writer thread.
	WriteBufferSize uint

	// The size of the channel for sending notifications to the control loop.
	ControlBufferSize uint

	// If we don't receive all information within this many blocks, flush to disk regardless.
	// The hash logger buffers data until it has all info for a block, but we shouldn't buffer
	// it forever.
	MaxBlockDelay uint

	// The number of HashLog entries to retain on disk.
	BlocksToRetain uint

	// The size log files are allowed to get before we close one and open another.
	TargetFileSize uint
}

// DefaultHashLoggerConfig returns a default configuration for a HashLogger.
func DefaultHashLoggerConfig(path string, version string) *HashLoggerConfig {
	return &HashLoggerConfig{
		Path:              path,
		Version:           version,
		HashBufferSize:    16,
		WriteBufferSize:   16,
		ControlBufferSize: 16,
		MaxBlockDelay:     16,
		BlocksToRetain:    1_000_000,
		TargetFileSize:    unit.MB,
	}
}

// Validate checks that the HashLoggerConfig is valid.
func (c *HashLoggerConfig) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("Path is required")
	}
	if c.Version == "" {
		return fmt.Errorf("Version is required")
	}
	return nil
}
