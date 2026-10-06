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
	cfg.EnableMetrics = false
	cfg.BlockCacheSize = int64(8 * unit.MB)
	return cfg
}
