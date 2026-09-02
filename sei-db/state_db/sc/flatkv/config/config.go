package config

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// Config defines configuration for the FlatKV (EVM) commit store.
type Config struct {
	// DataDir is the root directory for the FlatKV data files.
	// Must be set before calling Validate().
	DataDir string

	// Fsync controls whether every view manager's flush is fsync'd. It overwrites each store
	// config's FlushSync, so the four databases are always synced alike. The state WAL is
	// unaffected and always writes NoSync.
	Fsync bool `mapstructure:"fsync"`

	// AsyncWriteBuffer defines the size of the async write buffer for data DBs.
	// Set <= 0 for synchronous writes.
	AsyncWriteBuffer int `mapstructure:"async-write-buffer"`

	// SnapshotInterval defines how often (in blocks) a PebbleDB checkpoint
	// snapshot is taken. 0 disables auto-snapshots.
	// Without periodic snapshots the WAL grows unbounded and every restart
	// replays the entire history from snapshot-0.
	SnapshotInterval uint32 `mapstructure:"snapshot-interval"`

	// SnapshotKeepRecent defines how many old snapshots to keep besides the
	// latest one. 0 means keep only the current snapshot (no old snapshots).
	// Ignored entirely when ExternalPruning is set.
	SnapshotKeepRecent uint32 `mapstructure:"snapshot-keep-recent"`

	// MaxSnapshotLagBlocks is how many committed blocks may queue up behind a snapshot that is still
	// being written before Commit blocks. A value below 1 is treated as 1.
	//
	// A snapshot being written holds every database pinned at its own height, so no later block can
	// reach disk until it completes, and each one is retained in memory meanwhile. This bounds how far
	// that can run, trading a pause in block production for the memory the backlog would otherwise
	// consume. It bounds blocks rather than bytes, so it mitigates exhaustion rather than preventing it.
	MaxSnapshotLagBlocks uint32 `mapstructure:"max-snapshot-lag-blocks"`

	// ExternalPruning hands retention to the StorageGarbageCollector: the store stops pruning its
	// own snapshots (SnapshotKeepRecent) and stops truncating the state WAL.
	//
	// Not read from app.toml. It is set by whatever constructs the collector, since it is only
	// correct when this store is registered with a running one.
	//
	// With it on, snapshots are retained by height rather than by count, so the number kept becomes
	// RollbackWindow / SnapshotInterval instead of SnapshotKeepRecent + 1.
	ExternalPruning bool `mapstructure:"-"`

	// EnablePebbleMetrics defines if the Pebble metrics should be enabled.
	EnablePebbleMetrics bool `mapstructure:"enable-pebble-metrics"`

	// EnableReadWriteMetrics emits simple estimated read/write counters for FlatKV's Pebble DBs.
	EnableReadWriteMetrics bool `mapstructure:"enable-read-write-metrics"`

	// AccountDBConfig defines the PebbleDB configuration for the account database.
	AccountDBConfig pebbledb.PebbleDBConfig

	// AccountStoreConfig defines the view manager configuration for the account database. The store
	// owns this database's read cache and write staging, so its MaxSize is that database's cache budget.
	AccountStoreConfig view.ViewManagerConfig

	// CodeDBConfig defines the PebbleDB configuration for the code database.
	CodeDBConfig pebbledb.PebbleDBConfig

	// CodeStoreConfig defines the view manager configuration for the code database. The store
	// owns this database's read cache and write staging, so its MaxSize is that database's cache budget.
	CodeStoreConfig view.ViewManagerConfig

	// StorageDBConfig defines the PebbleDB configuration for the storage database.
	StorageDBConfig pebbledb.PebbleDBConfig

	// StorageStoreConfig defines the view manager configuration for the storage database. The store
	// owns this database's read cache and write staging, so its MaxSize is that database's cache budget.
	StorageStoreConfig view.ViewManagerConfig

	// MiscDBConfig defines the PebbleDB configuration for the misc database.
	MiscDBConfig pebbledb.PebbleDBConfig

	// MiscStoreConfig defines the view manager configuration for the misc database. The store
	// owns this database's read cache and write staging, so its MaxSize is that database's cache budget.
	MiscStoreConfig view.ViewManagerConfig

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
	// HashEngineConfig configures the pipeline that hashes each committed block.
	HashEngineConfig lthash.Config

	// FinalizationQueueSize is how many sealed blocks may be waiting to have their hashes recorded
	// before Commit blocks.
	//
	// A block waiting here holds a reservation on its own views, and a held reservation stops its
	// database's flush frontier, so this bounds how much of the pipeline stays resident.
	FinalizationQueueSize uint32 `mapstructure:"finalization-queue-size"`

	// HashChanSize is the depth of the channel block hashes are published on.
	//
	// Headroom for a consumer that reads later than it commits, not a memory bound: a block's views are
	// released before its hash is published. A consumer that stops reading entirely stalls commit.
	HashChanSize uint32 `mapstructure:"hash-chan-size"`

	// LtHashThreadsPerCore * runtime.NumCPU() (clamped to at least 1). LtHash
	// computation is CPU-bound, so ~1 worker per core is a sensible default.
	LtHashThreadsPerCore float64
}

// defaultStoreConfig returns the view manager defaults for one database, named for the database's
// directory so metrics and per-database hash bookkeeping can tell the stores apart.
func defaultStoreConfig(name string) view.ViewManagerConfig {
	return *view.DefaultViewManagerConfig(name, ktype.MetaKeyPrefix)
}

// DefaultConfig returns Config with safe default values.
func DefaultConfig() *Config {
	cfg := &Config{
		Fsync:                     false,
		AsyncWriteBuffer:          0,
		SnapshotInterval:          10000,
		SnapshotKeepRecent:        1,
		MaxSnapshotLagBlocks:      64,
		EnablePebbleMetrics:       true,
		AccountDBConfig:           pebbledb.DefaultConfig(),
		AccountStoreConfig:        defaultStoreConfig("account"),
		CodeDBConfig:              pebbledb.DefaultConfig(),
		CodeStoreConfig:           defaultStoreConfig("code"),
		StorageDBConfig:           pebbledb.DefaultConfig(),
		StorageStoreConfig:        defaultStoreConfig("storage"),
		MiscDBConfig:              pebbledb.DefaultConfig(),
		MiscStoreConfig:           defaultStoreConfig("misc"),
		ReaderThreadsPerCore:      2.0,
		ReaderConstantThreadCount: 0,
		ReaderPoolQueueSize:       1024,
		MiscPoolThreadsPerCore:    4.0,
		MiscConstantThreadCount:   0,
		LtHashThreadsPerCore:      1.0,
		HashEngineConfig:          *lthash.DefaultConfig(),
		FinalizationQueueSize:     64,
		HashChanSize:              1024,
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
