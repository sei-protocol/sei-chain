package pebbledb

import (
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/dbcache"
)

// Configuration for the PebbleDB database.
type PebbleDBConfig struct {
	// The directory to store the database files. This has no default value and must be provided.
	DataDir string
	// The size of pebbleDB's internal block cache, in bytes.
	BlockCacheSize int
	// Whether to enable metrics.
	EnableMetrics bool
	// How often to scrape metrics (pebble internals + cache size).
	MetricsScrapeInterval time.Duration
	// Configuration for the cache layer.
	CacheConfig *dbcache.CacheConfig
}

// Default configuration for the PebbleDB database.
func DefaultConfig() *PebbleDBConfig {
	cacheConfig := dbcache.DefaultCacheConfig()

	return &PebbleDBConfig{
		BlockCacheSize: 512 * unit.MB,
		EnableMetrics:  true,
		CacheConfig:    cacheConfig,
	}
}

// Default configuration suitable for testing. Allocates much smaller cache sizes and disables metrics.
// DataDir defaults to t.TempDir(); callers that need a specific path can override it after calling.
func DefaultTestPebbleDBConfig(t *testing.T) *PebbleDBConfig {
	cfg := DefaultConfig()
	cfg.EnableMetrics = false
	cfg.BlockCacheSize = 16 * unit.MB
	cfg.DataDir = t.TempDir()
	cfg.CacheConfig = dbcache.DefaultTestCacheConfig()
	cfg.CacheConfig.MetricsName = "test"
	return cfg
}

// Validates the configuration (basic sanity checks).
func (c *PebbleDBConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if err := c.CacheConfig.Validate(); err != nil {
		return fmt.Errorf("cache config is invalid: %w", err)
	}
	if c.BlockCacheSize <= 0 {
		return fmt.Errorf("block cache size must be greater than 0")
	}
	return nil
}
