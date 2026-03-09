package pebbledb

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

// Default configuration suitable for testing. Allocates much smaller cache sizes and disables metrics.
// DataDir defaults to t.TempDir(); callers that need a specific path can override it after calling.
func DefaultTestConfig(t *testing.T) PebbleDBConfig {
	cfg := DefaultConfig()

	cfg.DataDir = t.TempDir()
	cfg.CacheSize = 16 * unit.MB
	cfg.PageCacheSize = 16 * unit.MB
	cfg.EnableMetrics = false

	return cfg
}
