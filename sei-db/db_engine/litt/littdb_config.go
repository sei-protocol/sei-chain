package litt

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/litt/disktable/keymap"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
	"github.com/docker/go-units"
	"github.com/prometheus/client_golang/prometheus"
)

// Config is configuration for a litt.DB.
type Config struct {
	// The context for the database. If nil, context.Background() is used.
	CTX context.Context

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

	// The logger for the database. If nil, a logger is built using the LoggerConfig.
	Logger logging.Logger

	// The logger configuration for the database. Ignored if Logger is not nil.
	LoggerConfig *common.LoggerConfig

	// The type of the keymap. Choices are keymap.MemKeymapType and keymap.LevelDBKeymapType.
	// Default is keymap.LevelDBKeymapType.
	KeymapType keymap.KeymapType

	// The default TTL for newly created tables (either ones with data on disk or new tables).
	// The default is 0 (no TTL). TTL can be set individually on each table by calling Table.SetTTL().
	TTL time.Duration

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

	// The period between garbage collection runs. The default is 5 minutes.
	GCPeriod time.Duration

	// The size of the keymap deletion batch for garbage collection. The default is 10,000.
	GCBatchSize uint64

	// The sharding factor for the database. If the sharding factor is greater than 1, then values will be spread
	// out across multiple files. (Note that individual values will always be written to a single file, but two
	// different values may be written to different files.) These shard files are spead evenly across the paths
	// provided in the Paths field. If the sharding factor is larger than the number of paths, then some paths will
	// have multiple shard files. If the sharding factor is smaller than the number of paths, then some paths may not
	// always have an actively written shard file.
	//
	// The default is 8. Must be at least 1.
	ShardingFactor uint32

	// The random number generator used for generating sharding salts. The default is a standard rand.New()
	// seeded by the current time.
	SaltShaker *rand.Rand

	// The size of the cache for tables that have not had their write cache size set. A write cache is used
	// to store recently written values for fast access. The default is 0 (no cache).
	// Cache size is in bytes, and includes the size of both the key and the value. Cache size can be set
	// individually on each table by calling Table.SetWriteCacheSize().
	WriteCacheSize uint64

	// The size of the cache for tables that have not had their read cache size set. A read cache is used
	// to store recently read values for fast access. The default is 0 (no cache).
	// Cache size is in bytes, and includes the size of both the key and the value. Cache size can be set
	// individually on each table by calling Table.SetReadCacheSize().
	ReadCacheSize uint64

	// The time source used by the database. This can be substituted for an artificial time source
	// for testing purposes. The default is time.Now.
	Clock func() time.Time

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

	// If enabled, collect DB metrics and export them to prometheus. By default, this is false.
	MetricsEnabled bool

	// The namespace to use for metrics. If empty, the default namespace "litt" is used.
	MetricsNamespace string

	// The prometheus registry to use for metrics. If nil and metrics are enabled, a new registry is created.
	MetricsRegistry *prometheus.Registry

	// The port to use for the metrics server. Ignored if MetricsEnabled is false or MetricsRegistry is not nil.
	// The default is 9101.
	MetricsPort int

	// The interval at which various DB metrics are updated. The default is 1 second.
	MetricsUpdateInterval time.Duration

	// A function that is called if the database experiences a non-recoverable error (e.g. data corruption,
	// a crashed goroutine, a full disk, etc.). If nil (the default), no callback is called. If called at all,
	// this method is called exactly once.
	FatalErrorCallback func(error)

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
	seed := time.Now().UnixNano()
	saltShaker := rand.New(rand.NewSource(seed))

	loggerConfig := common.DefaultLoggerConfig()

	return &Config{
		CTX:                      context.Background(),
		LoggerConfig:             loggerConfig,
		Clock:                    time.Now,
		GCPeriod:                 5 * time.Minute,
		GCBatchSize:              10_000,
		ShardingFactor:           8,
		SaltShaker:               saltShaker,
		KeymapType:               keymap.LevelDBKeymapType,
		ControlChannelSize:       64,
		TargetSegmentFileSize:    math.MaxUint32,
		MaxSegmentKeyCount:       50_000,
		TargetSegmentKeyFileSize: 2 * units.MiB,
		Fsync:                    true,
		DoubleWriteProtection:    false,
		MetricsEnabled:           false,
		MetricsNamespace:         "litt",
		MetricsPort:              9101,
		MetricsUpdateInterval:    time.Second,
		PurgeLocks:               false,
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

// SanityCheck performs a sanity check on the configuration, returning an error if any of the configuration
// settings are invalid. The config returned by DefaultConfig() is guaranteed to pass this check if unmodified.
func (c *Config) SanityCheck() error {
	if c.CTX == nil {
		return fmt.Errorf("context cannot be nil")
	}
	if len(c.Paths) == 0 {
		return fmt.Errorf("at least one path must be provided")
	}
	if c.Logger == nil && c.LoggerConfig == nil {
		return fmt.Errorf("logger or logger config must be provided")
	}
	if c.Clock == nil {
		return fmt.Errorf("time source cannot be nil")
	}
	if c.GCBatchSize == 0 {
		return fmt.Errorf("gc batch size must be at least 1")
	}
	if c.ShardingFactor == 0 {
		return fmt.Errorf("sharding factor must be at least 1")
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
	if c.SaltShaker == nil {
		return fmt.Errorf("salt shaker cannot be nil")
	}
	if (c.MetricsEnabled || c.MetricsRegistry != nil) && c.MetricsUpdateInterval == 0 {
		return fmt.Errorf("metrics update interval must be at least 1 if metrics are enabled")
	}

	return nil
}
