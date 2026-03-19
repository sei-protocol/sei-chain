package flatkv

import (
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

const (
	DefaultSnapshotInterval   uint32 = 10000
	DefaultSnapshotKeepRecent uint32 = 2
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
	// Default: 2
	SnapshotKeepRecent uint32 `mapstructure:"snapshot-keep-recent"`

	// EnablePebbleMetrics defines if the Pebble metrics should be enabled.
	// Default: true
	EnablePebbleMetrics bool `mapstructure:"enable-pebble-metrics"`

	// AccountDBConfig defines the configuration for the account database.
	AccountDBConfig *pebbledb.PebbleDBConfig

	// CodeDBConfig defines the configuration for the code database.
	CodeDBConfig *pebbledb.PebbleDBConfig

	// StorageDBConfig defines the configuration for the storage database.
	StorageDBConfig *pebbledb.PebbleDBConfig

	// LegacyDBConfig defines the configuration for the legacy database.
	LegacyDBConfig *pebbledb.PebbleDBConfig

	// MetadataDBConfig defines the configuration for the metadata database.
	MetadataDBConfig *pebbledb.PebbleDBConfig

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
		CodeDBConfig:              pebbledb.DefaultConfig(),
		StorageDBConfig:           pebbledb.DefaultConfig(),
		LegacyDBConfig:            pebbledb.DefaultConfig(),
		MetadataDBConfig:          pebbledb.DefaultConfig(),
		ReaderThreadsPerCore:      2.0,
		ReaderConstantThreadCount: 0,
		ReaderPoolQueueSize:       1024,
		MiscPoolThreadsPerCore:    4.0,
		MiscConstantThreadCount:   0,
	}

	cfg.AccountDBConfig.CacheConfig.MaxSize = unit.GB
	cfg.StorageDBConfig.CacheConfig.MaxSize = unit.GB * 4

	cfg.AccountDBConfig.CacheConfig.MetricsName = "flatkv_account"
	cfg.CodeDBConfig.CacheConfig.MetricsName = "flatkv_code"
	cfg.StorageDBConfig.CacheConfig.MetricsName = "flatkv_storage"
	cfg.LegacyDBConfig.CacheConfig.MetricsName = "flatkv_legacy"
	cfg.MetadataDBConfig.CacheConfig.MetricsName = "flatkv_metadata"

	return cfg
}

// Copy returns a deep copy of the Config.
func (c *Config) Copy() *Config {
	//  The nested PebbleDB configs are value types, so a shallow struct copy is sufficient.
	cp := *c
	return &cp
}

// InitializeDataDirectories sets the DataDir for each nested PebbleDB config
// that does not already have one, using DataDir as the base path. The DBs live
// under the working directory: <DataDir>/working/<subdir>.
func (c *Config) InitializeDataDirectories() {
	workDir := filepath.Join(c.DataDir, workingDirName)
	if c.AccountDBConfig.DataDir == "" {
		c.AccountDBConfig.DataDir = filepath.Join(workDir, accountDBDir)
	}
	if c.CodeDBConfig.DataDir == "" {
		c.CodeDBConfig.DataDir = filepath.Join(workDir, codeDBDir)
	}
	if c.StorageDBConfig.DataDir == "" {
		c.StorageDBConfig.DataDir = filepath.Join(workDir, storageDBDir)
	}
	if c.LegacyDBConfig.DataDir == "" {
		c.LegacyDBConfig.DataDir = filepath.Join(workDir, legacyDBDir)
	}
	if c.MetadataDBConfig.DataDir == "" {
		c.MetadataDBConfig.DataDir = filepath.Join(workDir, metadataDir)
	}
}

// Validate checks that the configuration is sane and returns an error if it is not.
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if c.AccountDBConfig.Validate() != nil {
		return fmt.Errorf("account db config is invalid: %w", c.AccountDBConfig.Validate())
	}
	if c.CodeDBConfig.Validate() != nil {
		return fmt.Errorf("code db config is invalid: %w", c.CodeDBConfig.Validate())
	}
	if c.StorageDBConfig.Validate() != nil {
		return fmt.Errorf("storage db config is invalid: %w", c.StorageDBConfig.Validate())
	}
	if c.LegacyDBConfig.Validate() != nil {
		return fmt.Errorf("legacy db config is invalid: %w", c.LegacyDBConfig.Validate())
	}
	if c.MetadataDBConfig.Validate() != nil {
		return fmt.Errorf("metadata db config is invalid: %w", c.MetadataDBConfig.Validate())
	}

	if c.ReaderThreadsPerCore < 0 {
		return fmt.Errorf("reader threads per core must be greater than 0")
	}
	if c.ReaderConstantThreadCount < 0 {
		return fmt.Errorf("reader constant thread count must be greater than 0")
	}
	if c.ReaderPoolQueueSize < 0 {
		return fmt.Errorf("reader pool queue size must be greater than 0")
	}
	if c.MiscPoolThreadsPerCore < 0 {
		return fmt.Errorf("misc threads per core must be greater than 0")
	}
	if c.MiscConstantThreadCount < 0 {
		return fmt.Errorf("misc constant thread count must be greater than 0")
	}

	return nil
}
