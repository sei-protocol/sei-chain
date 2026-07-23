package snapshot

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

type SnapshotEngineConfig struct {
	// The number of shards in the snapshot engine. Must be a power of two and greater than 0.
	ShardCount uint64

	// The maximum size of the DB read-cache, in bytes. This constrains only the DB read-cache, not
	// snapshot data, since snapshots cannot be freed without compromising consistency semantics.
	MaxSize uint64

	// The estimated overhead per entry, in bytes. This is used to calculate the maximum size of the DB read-cache.
	// This value should be derived experimentally, and may differ between different builds and architectures.
	EstimatedOverheadPerEntry uint64

	// Name used as the "cache" attribute on OTel metrics. Must be non-empty.
	MetricsName string

	// Whether to enable OTel metrics collection.
	MetricsEnabled bool

	// How often to scrape cache size for metrics, in seconds.
	MetricsScrapeIntervalSeconds float64

	// The maximum number of unreleased snapshots that can be pending retirement before Snapshot() blocks.
	MaxUnretiredVersions uint64

	// Target number of keys per batch when flushing retired version data to the underlying DB.
	TargetKeysPerFlush int

	// A special metadata key where the DB stores its hash.
	HashKey string

	// Whether to fsync flushed data to the underlying DB on each flush commit. When false, flushes
	// are not individually fsync'd: on a hard OS/power crash the most recent unsynced flushes may be
	// lost (but the DB is not corrupted), and crash durability is instead provided by an upstream
	// fsync'd WAL / block replay. Set true for deployments that want per-flush durability regardless.
	FlushSync bool
}

// Default configuration for a production snapshot engine.
func DefaultSnapshotEngineConfig() *SnapshotEngineConfig {
	return &SnapshotEngineConfig{
		ShardCount:                   8,
		MaxSize:                      unit.GB / 2,
		EstimatedOverheadPerEntry:    256,
		MetricsEnabled:               true,
		MetricsScrapeIntervalSeconds: 10,
		MaxUnretiredVersions:         4,
		TargetKeysPerFlush:           1024 * 10,
		FlushSync:                    false,
	}
}

// Default configuration for unit tests. Main difference is that allocated space is much smaller by default.
func DefaultTestSnapshotEngineConfig() *SnapshotEngineConfig {
	config := DefaultSnapshotEngineConfig()
	config.MaxSize = unit.MB * 16
	config.MetricsEnabled = false
	config.HashKey = "_meta/hash"
	return config
}

func (c *SnapshotEngineConfig) MetricsScrapeInterval() time.Duration {
	return time.Duration(c.MetricsScrapeIntervalSeconds * float64(time.Second))
}

func (c *SnapshotEngineConfig) Validate() error {
	if c.ShardCount == 0 || (c.ShardCount&(c.ShardCount-1)) != 0 {
		return fmt.Errorf("ShardCount must be a power of two and greater than 0, got %d", c.ShardCount)
	}
	if c.MaxSize == 0 {
		return fmt.Errorf("MaxSize must be greater than 0")
	}
	if c.MaxSize < c.ShardCount {
		return fmt.Errorf("MaxSize (%d) must be >= ShardCount (%d)", c.MaxSize, c.ShardCount)
	}
	if c.EstimatedOverheadPerEntry == 0 {
		return fmt.Errorf("EstimatedOverheadPerEntry must be greater than 0")
	}
	if c.MetricsName == "" {
		return fmt.Errorf("MetricsName must be non-empty")
	}
	if c.MetricsEnabled && c.MetricsScrapeIntervalSeconds <= 0 {
		return fmt.Errorf("MetricsScrapeIntervalSeconds must be positive when MetricsEnabled is true")
	}
	if c.MaxUnretiredVersions == 0 {
		return fmt.Errorf("MaxUnretiredVersions must be greater than 0")
	}
	if c.TargetKeysPerFlush <= 0 {
		return fmt.Errorf("TargetKeysPerFlush must be positive")
	}
	if c.HashKey == "" {
		return fmt.Errorf("HashKey must be non-empty")
	}
	return nil
}
