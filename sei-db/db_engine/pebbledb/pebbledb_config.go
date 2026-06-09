package pebbledb

import (
	"fmt"
	"time"
)

// Configuration for the PebbleDB database.
type PebbleDBConfig struct {
	// The directory to store the database files. This has no default value and must be provided.
	DataDir string
	// Whether to enable pebble-internal metrics.
	EnableMetrics bool
	// How often to scrape pebble-internal metrics.
	MetricsScrapeInterval time.Duration

	// --- Bulk-load tuning (all optional; zero value keeps production defaults) ---
	// These let a one-shot, restartable bulk importer trade durability and
	// read-shape for write throughput. They are zero in normal operation, so
	// Open behaves exactly as before unless a caller opts in.

	// DisableWAL turns off the Pebble write-ahead log. Unflushed memtable data is
	// lost on crash, so only use this for restartable bulk loads, and Flush()
	// before taking a checkpoint (a checkpoint cannot recover the memtable
	// without a WAL).
	DisableWAL bool
	// DisableAutomaticCompactions defers background compactions during the load.
	// If set, run a manual Compact afterward to restore read performance.
	DisableAutomaticCompactions bool
	// MemTableSize overrides the memtable size in bytes (0 = default). Larger
	// memtables mean fewer, larger L0 flushes and less compaction churn.
	MemTableSize uint64
	// L0CompactionThreshold overrides the L0 file count that triggers compaction
	// (0 = default).
	L0CompactionThreshold int
	// L0StopWritesThreshold overrides the L0 file count that stalls writes
	// (0 = default).
	L0StopWritesThreshold int
	// MemTableStopWritesThreshold overrides the number of queued memtables that
	// stalls writes (0 = default). Larger values absorb write bursts during a
	// bulk load.
	MemTableStopWritesThreshold int
	// MaxConcurrentCompactions overrides compaction concurrency (0 = default).
	// Mapped onto Pebble's CompactionConcurrencyRange upper bound so compactions
	// can fan out across cores during a load.
	MaxConcurrentCompactions int
}

// Default configuration for the PebbleDB database.
func DefaultConfig() PebbleDBConfig {
	return PebbleDBConfig{
		EnableMetrics:         true,
		MetricsScrapeInterval: 10 * time.Second,
	}
}

// Validates the configuration (basic sanity checks).
func (c *PebbleDBConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if c.EnableMetrics && c.MetricsScrapeInterval <= 0 {
		return fmt.Errorf("metrics scrape interval must be positive when metrics are enabled")
	}
	return nil
}
