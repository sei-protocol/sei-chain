package config

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
)

const (
	DefaultSnapshotInterval   uint32 = 10000
	DefaultSnapshotKeepRecent uint32 = 1
)

// Config defines configuration for the FlatKV (EVM) commit store.
type Config struct {
	// DataDir is the root directory for the FlatKV data files.
	// Must be set before calling Validate().
	DataDir string

	// Fsync controls whether PebbleDB writes (data DBs + metadataDB) use fsync.
	// WAL always uses NoSync (matching memiavl); crash recovery relies on
	// WAL catchup, which is idempotent.
	// Default: false
	Fsync bool `mapstructure:"fsync"`

	// AsyncWriteBuffer defines the size of the async write buffer for data DBs.
	// Set <= 0 for synchronous writes.
	// Default: 0 (synchronous)
	AsyncWriteBuffer int `mapstructure:"async-write-buffer"`

	// SnapshotInterval defines how often (in blocks) a PebbleDB checkpoint
	// snapshot is taken. 0 disables auto-snapshots.
	// Without periodic snapshots the WAL grows unbounded and every restart
	// replays the entire history from snapshot-0.
	// Default: 10000
	SnapshotInterval uint32 `mapstructure:"snapshot-interval"`

	// SnapshotKeepRecent defines how many old snapshots to keep besides the
	// latest one. 0 means keep only the current snapshot (no old snapshots).
	// Ignored entirely when ExternalPruning is set.
	// Default: 1
	SnapshotKeepRecent uint32 `mapstructure:"snapshot-keep-recent"`

	// ExternalPruning hands retention to the StorageGarbageCollector: the store stops pruning its
	// own snapshots (SnapshotKeepRecent) and stops truncating the state WAL.
	//
	// Not read from app.toml. It is set by whatever constructs the collector, since it is only
	// correct when this store is registered with a running one.
	//
	// With it on, snapshots are retained by height rather than by count, so the number kept becomes
	// RollbackWindow / SnapshotInterval instead of SnapshotKeepRecent + 1.
	//
	// Default: false
	ExternalPruning bool `mapstructure:"-"`

	// EnablePebbleMetrics defines if the Pebble metrics should be enabled.
	// Default: true
	EnablePebbleMetrics bool `mapstructure:"enable-pebble-metrics"`

	// EnableReadWriteMetrics emits simple estimated read/write counters for FlatKV's Pebble DBs.
	// Default: false
	EnableReadWriteMetrics bool `mapstructure:"enable-read-write-metrics"`

	// AccountDBConfig defines the PebbleDB configuration for the account database.
	AccountDBConfig pebbledb.PebbleDBConfig

	// AccountStoreConfig defines the snapshot engine configuration for the account database. The store
	// owns this database's read cache and write staging, so its MaxSize is that database's cache budget.
	AccountStoreConfig snapshot.SnapshotEngineConfig

	// CodeDBConfig defines the PebbleDB configuration for the code database.
	CodeDBConfig pebbledb.PebbleDBConfig

	// CodeStoreConfig defines the snapshot engine configuration for the code database. The store
	// owns this database's read cache and write staging, so its MaxSize is that database's cache budget.
	CodeStoreConfig snapshot.SnapshotEngineConfig

	// StorageDBConfig defines the PebbleDB configuration for the storage database.
	StorageDBConfig pebbledb.PebbleDBConfig

	// StorageStoreConfig defines the snapshot engine configuration for the storage database. The store
	// owns this database's read cache and write staging, so its MaxSize is that database's cache budget.
	StorageStoreConfig snapshot.SnapshotEngineConfig

	// MiscDBConfig defines the PebbleDB configuration for the misc database.
	MiscDBConfig pebbledb.PebbleDBConfig

	// MiscStoreConfig defines the snapshot engine configuration for the misc database. The store
	// owns this database's read cache and write staging, so its MaxSize is that database's cache budget.
	MiscStoreConfig snapshot.SnapshotEngineConfig

	// MetadataDBConfig defines the PebbleDB configuration for the metadata database.
	MetadataDBConfig pebbledb.PebbleDBConfig

	// MetadataStoreConfig defines the snapshot engine configuration for the metadata database. The store
	// owns this database's read cache and write staging, so its MaxSize is that database's cache budget.
	MetadataStoreConfig snapshot.SnapshotEngineConfig

	// Controls the number of goroutines in the DB read pool. The number of threads in this pool is equal to
	// ReaderThreadsPerCore * runtime.NumCPU() + ReaderConstantThreadCount.
	ReaderThreadsPerCore float64

	// Controls the number of goroutines in the DB read pool. The number of threads in this pool is equal to
	// ReaderThreadsPerCore * runtime.NumCPU() + ReaderConstantThreadCount.
	ReaderConstantThreadCount int

	// Controls the size of the queue for work sent to the read pool.
	ReaderPoolQueueSize int

	// Controls the number of goroutines pre-allocated in the thread pool for miscellaneous operations.
	// The number of threads in this pool is equal to MiscThreadsPerCore * runtime.NumCPU() + MiscConstantThreadCount.
	MiscPoolThreadsPerCore float64

	// Controls the number of goroutines pre-allocated in the thread pool for miscellaneous operations.
	// The number of threads in this pool is equal to MiscThreadsPerCore * runtime.NumCPU() + MiscConstantThreadCount.
	MiscConstantThreadCount int

	// Controls the number of workers in the dedicated lattice-hash pool used to
	// compute per-module LtHashes during ApplyChangeSets. The worker count is
	// LtHashThreadsPerCore * runtime.NumCPU() (clamped to at least 1). LtHash
	// computation is CPU-bound, so ~1 worker per core is a sensible default.
	LtHashThreadsPerCore float64
}

// MetaKeyPrefix is the key namespace FlatKV reserves for per-database metadata, and which each
// snapshot engine owns: Finalize writes land under it and iteration filters it out. It matches
// ktype.MetaKeyPrefixBytes, restated here because ktype imports this package's siblings.
const MetaKeyPrefix = "_meta/"

// defaultStoreConfig returns the snapshot engine defaults for one database, named for the database's
// directory so metrics and per-database hash bookkeeping can tell the stores apart.
func defaultStoreConfig(name string) snapshot.SnapshotEngineConfig {
	return *snapshot.DefaultSnapshotEngineConfig(name, MetaKeyPrefix)
}

// DefaultConfig returns Config with safe default values.
func DefaultConfig() *Config {
	cfg := &Config{
		Fsync:                     false,
		AsyncWriteBuffer:          0,
		SnapshotInterval:          DefaultSnapshotInterval,
		SnapshotKeepRecent:        DefaultSnapshotKeepRecent,
		EnablePebbleMetrics:       true,
		AccountDBConfig:           pebbledb.DefaultConfig(),
		AccountStoreConfig:        defaultStoreConfig("account"),
		CodeDBConfig:              pebbledb.DefaultConfig(),
		CodeStoreConfig:           defaultStoreConfig("code"),
		StorageDBConfig:           pebbledb.DefaultConfig(),
		StorageStoreConfig:        defaultStoreConfig("storage"),
		MiscDBConfig:              pebbledb.DefaultConfig(),
		MiscStoreConfig:           defaultStoreConfig("misc"),
		MetadataDBConfig:          pebbledb.DefaultConfig(),
		MetadataStoreConfig:       defaultStoreConfig("metadata"),
		ReaderThreadsPerCore:      2.0,
		ReaderConstantThreadCount: 0,
		ReaderPoolQueueSize:       1024,
		MiscPoolThreadsPerCore:    4.0,
		MiscConstantThreadCount:   0,
		LtHashThreadsPerCore:      1.0,
	}

	cfg.AccountStoreConfig.MaxSize = unit.GB
	cfg.StorageStoreConfig.MaxSize = unit.GB * 4

	return cfg
}

// Copy returns a deep copy of the Config.
func (c *Config) Copy() *Config {
	//  The nested PebbleDB configs are value types, so a shallow struct copy is sufficient.
	cp := *c
	return &cp
}

// Validate checks that the configuration is sane and returns an error if it is not.
func (c *Config) Validate() error {
	if err := c.AccountStoreConfig.Validate(); err != nil {
		return fmt.Errorf("account store config is invalid: %w", err)
	}
	if err := c.CodeStoreConfig.Validate(); err != nil {
		return fmt.Errorf("code store config is invalid: %w", err)
	}
	if err := c.StorageStoreConfig.Validate(); err != nil {
		return fmt.Errorf("storage store config is invalid: %w", err)
	}
	if err := c.MiscStoreConfig.Validate(); err != nil {
		return fmt.Errorf("misc store config is invalid: %w", err)
	}
	if err := c.MetadataStoreConfig.Validate(); err != nil {
		return fmt.Errorf("metadata store config is invalid: %w", err)
	}
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if err := c.AccountDBConfig.Validate(); err != nil {
		return fmt.Errorf("account db config is invalid: %w", err)
	}
	if err := c.CodeDBConfig.Validate(); err != nil {
		return fmt.Errorf("code db config is invalid: %w", err)
	}
	if err := c.StorageDBConfig.Validate(); err != nil {
		return fmt.Errorf("storage db config is invalid: %w", err)
	}
	if err := c.MiscDBConfig.Validate(); err != nil {
		return fmt.Errorf("misc db config is invalid: %w", err)
	}
	if err := c.MetadataDBConfig.Validate(); err != nil {
		return fmt.Errorf("metadata db config is invalid: %w", err)
	}

	if c.ReaderThreadsPerCore <= 0 {
		return fmt.Errorf("reader threads per core must be greater than 0")
	}
	if c.ReaderConstantThreadCount < 0 {
		return fmt.Errorf("reader constant thread count must not be negative")
	}
	if c.ReaderPoolQueueSize < 0 {
		return fmt.Errorf("reader pool queue size must not be negative")
	}
	if c.MiscPoolThreadsPerCore < 0 {
		return fmt.Errorf("misc threads per core must not be negative")
	}
	if c.MiscConstantThreadCount < 0 {
		return fmt.Errorf("misc constant thread count must not be negative")
	}
	if c.LtHashThreadsPerCore < 0 {
		return fmt.Errorf("lthash threads per core must not be negative")
	}

	return nil
}
