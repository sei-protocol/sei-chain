package pebbledb

import (
	"testing"
)

// DefaultTestConfig returns a PebbleDBConfig suitable for testing.
// Allocates a smaller block cache and disables metrics.
func DefaultTestConfig(t *testing.T) PebbleDBConfig {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.EnableMetrics = false
	return cfg
}
