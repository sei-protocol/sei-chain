package litt

import (
	"fmt"
	"math"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// MaxShardingFactor is the largest legal value for TableConfig.ShardingFactor. Both the shard ID (in the on-disk
// Address) and the per-segment sharding factor (in the segment metadata file) are encoded as a single byte,
// which structurally caps the sharding factor at 2^8 - 1 = 255.
const MaxShardingFactor = 255

// Config is configuration for a litt.DB.
type Config struct {
	// The paths where the database will store its files. If the path does not exist, it will be created.
	// If more than one path is provided, then the database will do its best to spread out the data across
	// the paths. If the database is restarted, it will attempt to load data from all paths. Note: the number
	// of paths should not exceed the sharding factor, or else data may not be split across all paths.
	//
	// Most of the time, providing exactly one path is sufficient. If the data should be spread across multiple
	// drives, then providing multiple permits that. The number of provided paths should be a small number, perhaps
	// a few dozen paths at most. Providing an excessive number of paths may lead to degraded performance.
	//
	// Providing zero paths will cause the DB to return an error at startup.
	Paths []string

	// The type of the keymap. Choices are keymap.MemKeymapType and keymap.PebbleDBKeymapType.
	// Default is keymap.PebbleDBKeymapType.
	KeymapType keymap.KeymapType

	// The size of the control channel for the segment manager. The default is 64.
	ControlChannelSize int

	// The target size for segments. The default is math.MaxUint32.
	TargetSegmentFileSize uint32

	// The maximum number of keys in a segment. The default is 50,000. For workloads with moderately large values
	// (i.e. in the kb+ range), this threshold is unlikely to be relevant. For workloads with very small values,
	// this constant prevents a segment from accumulating too many keys. A segment with too many keys may have
	// undesirable properties such as a very large key file and very slow garbage collection (since no kv-pair in
	// a segment can be deleted until the entire segment is deleted).
	MaxSegmentKeyCount uint32

	// The desired maximum size for a key file. The default is 2 MB. When a key file exceeds this size, the segment
	// will close the current segment and begin writing to a new one. For workloads with moderately large values,
	// this threshold is unlikely to be relevant. For workloads with very small values, this constant prevents a key
	// file from growing too large. A key file with too many keys may have undesirable properties such as very slow
	// garbage collection (since no kv-pair in a segment can be deleted until the entire segment is deleted).
	TargetSegmentKeyFileSize uint64

	// The period between garbage collection runs. The default is 10 seconds. GC is cheap on the control loop
	// (keymap deletes happen asynchronously on the keymap manager), so it runs frequently to avoid letting a
	// backlog of collectable segments build up.
	GCPeriod time.Duration

	// The size of the keymap deletion batch for garbage collection. The default is 10,000.
	GCBatchSize uint64

	// If true, then flush operations will call fsync on the underlying file to ensure data is flushed out of the
	// operating system's buffer and onto disk. Setting this to false means that even after flushing data,
	// there may be data loss in the advent of an OS/hardware crash.
	//
	// The default is true.
	//
	// Enabling fsync may have performance implications, although this strongly depends on the workload. For large
	// batches that are flushed infrequently, benchmark data suggests that the impact is minimal. For small batches
	// that are flushed frequently, the difference can be severe. For example, when enabled in unit tests that do
	// super tiny and frequent flushes, the difference in performance was an order of magnitude.
	Fsync bool

	// If enabled, the database will return an error if a key is written but that key is already present in
	// the database. Updating existing keys is illegal and may result in unexpected behavior, and so this check
	// acts as a safety mechanism against this sort of illegal operation. Unfortunately, if using a keymap other
	// than keymap.MemKeymapType, performing this check may be very expensive. By default, this is false.
	DoubleWriteProtection bool

	// If enabled, collect DB metrics and export them via the global OTel MeterProvider. By default, this is false.
	// When enabled, the database configures a Prometheus exporter on the global provider and serves /metrics on
	// MetricsPort.
	MetricsEnabled bool

	// The port to use for the metrics server. Ignored if MetricsEnabled is false. The default is 9101.
	MetricsPort int

	// The interval at which various DB metrics are updated. The default is 1 second.
	MetricsUpdateInterval time.Duration

	// If empty, snapshotting is disabled. If not empty, then this directory is used by the database to publish a
	// rolling sequence of "snapshots". Using the data in the snapshot directory, an external process can safely
	// get a consistent read-only views of the database.
	//
	// The snapshot directory will contain symbolic links to segment files that are safe for external processes to
	// read/copy. If, at any point in time, an external process takes all data in the snapshot directory and loads
	// it into a new LittDB instance, then that instance will have a consistent view of the database. (Note that there
	// are some steps required to load this data into a new database instance.)
	//
	// Since data may be spread across multiple physical volumes, it is not possible to create a directory with hard
	// linked files for all configurations (short of making cost-prohibitive copies). Each symbolic link in the
	// snapshot directory points to a file that MUST be garbage collected by whatever external process is making use
	// of database snapshots. Failing to clean up the hard linked files referenced by the symlinks will result in a
	// disk space leak.
	SnapshotDirectory string

	// If true, then purge all lock files prior to starting the database. This is potentially dangerous, as it will
	// permit multiple databases to be opened against the same data directories. If ever there are two LittDB
	// instances running against the same data directories, data corruption is almost a certainty.
	PurgeLocks bool

	// If Flush() is called more frequently than this interval, the flushes may be batched together to improve
	// performance. If this is set to zero, then no batching is performed and all flushes are executed immediately.
	MinimumFlushInterval time.Duration

	// The capacity of the buffered channel feeding the asynchronous keymap manager. Keymap puts and deletes are
	// scheduled (not executed) on the Flush() and GC paths; this bounds how many operations may be queued for the
	// keymap before backpressure is applied, which in turn bounds how far the keymap may lag behind the segments.
	// The default is 1024.
	KeymapManagerChannelSize int

	// The maximum number of keys the asynchronous keymap manager coalesces into a single keymap Put or Delete.
	// Larger values amortize the keymap's per-write fsync across more keys under load; the cap bounds the size
	// and latency of any single operation. The default is 10000.
	KeymapManagerMaxBatchSize int

	// The maximum time the asynchronous keymap manager accumulates scheduled work before applying a partial batch.
	// The manager prefers to coalesce work into full batches (see KeymapManagerMaxBatchSize), but if a full batch
	// does not accumulate within this interval it applies whatever it has, bounding how long a key may wait before
	// it is written into the keymap. The default is 1 second.
	KeymapManagerMaxInterval time.Duration

	// The maximum number of garbage-collected keys the keymap manager will buffer awaiting deletion. Deletes are
	// drained incrementally and always yield to latency-critical puts, so a large garbage-collection burst does
	// not stall writes; this is the high-water mark at which the manager stops accepting new work (backpressuring
	// producers via a full channel) until the backlog drains to half. Bounds the manager's peak memory. The
	// default is 1000000.
	KeymapManagerMaxBufferedDeletes uint64

	// The capacity of the channel on which the keymap manager publishes its deletion watermark to the control
	// loop (which gates garbage collection of segment files). Sends are fire-and-forget: if the channel is full
	// the update is dropped. The watermark is monotonic so a drop is always safe (it never causes a premature
	// file deletion), but a dropped value is only superseded by a subsequent, higher publish — so a single
	// pass that collects more than this many segments before the control loop drains may defer reclaiming some
	// files until a later collection. Sizing this at or above the largest expected single-pass collection keeps
	// reclamation complete in one pass (relevant to explicit RunGC). The default is 1024.
	KeymapManagerWatermarkChannelSize int

	// The capacity of the channel over which the control loop hands sealed segments to the GC manager (the GC
	// manager keeps its own local view of sealed segments rather than reading the control loop's segment map).
	// A segment is sent the moment it is sealed; the GC manager drains the channel between collection passes, so
	// this only needs to absorb the seals that occur during a single pass. If it fills, the control loop applies
	// brief backpressure to writes until the GC manager drains it. The default is 1024.
	GCSegmentChannelSize int
}

// DefaultConfig returns a Config with default values.
func DefaultConfig(paths ...string) (*Config, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one path must be provided")
	}

	config := DefaultConfigNoPaths()
	config.Paths = paths

	return config, nil
}

// DefaultConfigNoPaths returns a Config with default values, and does not require any paths to be provided.
// If paths are not set prior to use, then the DB will return an error at startup.
func DefaultConfigNoPaths() *Config {
	return &Config{
		GCPeriod:                          10 * time.Second,
		GCBatchSize:                       10_000,
		KeymapType:                        keymap.PebbleDBKeymapType,
		ControlChannelSize:                64,
		TargetSegmentFileSize:             math.MaxUint32,
		MaxSegmentKeyCount:                50_000,
		TargetSegmentKeyFileSize:          2 * unit.MB,
		Fsync:                             true,
		DoubleWriteProtection:             false,
		MetricsEnabled:                    false,
		MetricsPort:                       9101,
		MetricsUpdateInterval:             time.Second,
		PurgeLocks:                        false,
		KeymapManagerChannelSize:          1024,
		KeymapManagerMaxBatchSize:         10_000,
		KeymapManagerMaxInterval:          time.Second,
		KeymapManagerMaxBufferedDeletes:   1_000_000,
		KeymapManagerWatermarkChannelSize: 1024,
		GCSegmentChannelSize:              1024,
	}
}

// SanitizePaths replaces any paths that start with '~' with the user's home directory.
func (c *Config) SanitizePaths() error {
	for i, path := range c.Paths {
		var err error
		c.Paths[i], err = util.SanitizePath(path)
		if err != nil {
			return fmt.Errorf("error sanitizing path %s: %w", path, err)
		}
	}

	if c.SnapshotDirectory != "" {
		var err error
		c.SnapshotDirectory, err = util.SanitizePath(c.SnapshotDirectory)
		if err != nil {
			return fmt.Errorf("error sanitizing snapshot directory %s: %w", c.SnapshotDirectory, err)
		}
	}

	return nil
}

// Validate performs a sanity check on the configuration, returning an error if any of the configuration
// settings are invalid. The config returned by DefaultConfig() is guaranteed to pass this check if unmodified.
func (c *Config) Validate() error {
	if len(c.Paths) == 0 {
		return fmt.Errorf("at least one path must be provided")
	}
	if c.GCBatchSize == 0 {
		return fmt.Errorf("gc batch size must be at least 1")
	}
	if c.ControlChannelSize == 0 {
		return fmt.Errorf("control channel size must be at least 1")
	}
	if c.TargetSegmentFileSize == 0 {
		return fmt.Errorf("target segment file size must be at least 1")
	}
	if c.MaxSegmentKeyCount == 0 {
		return fmt.Errorf("max segment key count must be at least 1")
	}
	if c.TargetSegmentKeyFileSize == 0 {
		return fmt.Errorf("target segment key file size must be at least 1")
	}
	if c.GCPeriod == 0 {
		return fmt.Errorf("gc period must be at least 1")
	}
	if c.MetricsEnabled && c.MetricsUpdateInterval == 0 {
		return fmt.Errorf("metrics update interval must be at least 1 if metrics are enabled")
	}
	if c.KeymapManagerChannelSize < 1 {
		return fmt.Errorf("keymap write channel size must be at least 1")
	}
	if c.KeymapManagerMaxBatchSize < 1 {
		return fmt.Errorf("keymap write max batch size must be at least 1")
	}
	if c.KeymapManagerMaxInterval <= 0 {
		return fmt.Errorf("keymap write max interval must be greater than zero")
	}
	if c.KeymapManagerMaxBufferedDeletes < 1 {
		return fmt.Errorf("keymap max buffered deletes must be at least 1")
	}
	if c.KeymapManagerWatermarkChannelSize < 1 {
		return fmt.Errorf("keymap watermark channel size must be at least 1")
	}
	if c.GCSegmentChannelSize < 1 {
		return fmt.Errorf("gc segment channel size must be at least 1")
	}

	return nil
}
