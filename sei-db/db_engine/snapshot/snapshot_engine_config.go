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

	// The maximum number of hashed-but-unflushed snapshots (snapshots whose diffs have not yet
	// been written to the underlying DB) tolerated before Commit() blocks. This backpressure
	// engages only when the underlying DB is the bottleneck. It intentionally does NOT bound
	// unhashed or unreleased snapshots: the engine only receives hashes from an external
	// workflow, and the caller is responsible for pausing execution if hashing or release falls
	// behind (see SnapshotEngine.Snapshot).
	MaxUnflushedVersions uint64

	// Target size, in bytes, of a write batch when flushing snapshot data to the underlying DB.
	// A batch is committed once it reaches this size at a version boundary; batches only ever
	// split between versions (each committed batch must leave the DB at a consistent version with
	// its hash), so a single version whose diff exceeds this produces one oversized batch.
	//
	// Flush throughput plateaus above roughly 1 MB. Keep this well below half the underlying DB's
	// memtable size: pebble diverts batches larger than that onto its flushable-batch slow path,
	// which hurts read amplification and compaction shape.
	TargetBytesPerFlush uint64

	// A special metadata key where the DB stores its hash.
	//
	// This key is owned by the engine and is reserved. For performance reasons the write path
	// does not check for it, so writing it through Set/Delete/BatchSet, or reading it through
	// Get/BatchGet, is undefined behavior: flushes overwrite user writes to this key, and a
	// cached read of it can go permanently stale. Iterators DO expose this key (see
	// Snapshot.Iterator).
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
		MaxUnflushedVersions:         4,
		TargetBytesPerFlush:          unit.MB * 4,
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
	if c.MaxUnflushedVersions == 0 {
		return fmt.Errorf("MaxUnflushedVersions must be greater than 0")
	}
	if c.TargetBytesPerFlush == 0 {
		return fmt.Errorf("TargetBytesPerFlush must be greater than 0")
	}
	if c.HashKey == "" {
		return fmt.Errorf("HashKey must be non-empty")
	}
	return nil
}
