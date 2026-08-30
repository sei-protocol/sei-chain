package pebbledb

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// DefaultTestConfig returns a PebbleDBConfig suitable for testing.
// Allocates a smaller block cache and disables metrics.
func DefaultTestConfig(t *testing.T) PebbleDBConfig {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.CacheSize = int64(16 * unit.MB)
	cfg.MemTableSize = 4 * unit.MB
	cfg.MemTableStopWritesThreshold = 2
	cfg.EnableMetrics = false
	return cfg
}
