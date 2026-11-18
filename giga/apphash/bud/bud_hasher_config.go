package bud

import "fmt"

// BUDHasherConfig configures a StandardBUDHasher.
type BUDHasherConfig struct {
	// The number of blocks that may wait to be sorted, and the number that may wait to be hashed, before Submit()
	// blocks. Higher values absorb longer stalls, at the cost of memory.
	BufferSize int
}

// DefaultBUDHasherConfig returns a BUDHasherConfig holding every default.
func DefaultBUDHasherConfig() BUDHasherConfig {
	return BUDHasherConfig{
		BufferSize: 8,
	}
}

// Validate returns an error if the configuration is invalid.
func (c *BUDHasherConfig) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be positive, got %d", c.BufferSize)
	}
	return nil
}
