package flatkv

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

const (
	DefaultSnapshotInterval   uint32 = 10000
	DefaultSnapshotKeepRecent uint32 = 2
)

// Config defines configuration for the FlatKV (EVM) commit store.
type Config struct {
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
	AccountDBConfig pebbledb.PebbleDBConfig

	// CodeDBConfig defines the configuration for the code database.
	CodeDBConfig pebbledb.PebbleDBConfig

	// StorageDBConfig defines the configuration for the storage database.
	StorageDBConfig pebbledb.PebbleDBConfig

	// LegacyDBConfig defines the configuration for the legacy database.
	LegacyDBConfig pebbledb.PebbleDBConfig

	// MetadataDBConfig defines the configuration for the metadata database.
	MetadataDBConfig pebbledb.PebbleDBConfig
}

// DefaultConfig returns Config with safe default values.
func DefaultConfig() Config {
	cfg := Config{
		Fsync:               false,
		AsyncWriteBuffer:    0,
		SnapshotInterval:    DefaultSnapshotInterval,
		SnapshotKeepRecent:  DefaultSnapshotKeepRecent,
		EnablePebbleMetrics: true,
		AccountDBConfig:     pebbledb.DefaultConfig(),
		CodeDBConfig:        pebbledb.DefaultConfig(),
		StorageDBConfig:     pebbledb.DefaultConfig(),
		LegacyDBConfig:      pebbledb.DefaultConfig(),
		MetadataDBConfig:    pebbledb.DefaultConfig(),
	}

	cfg.AccountDBConfig.CacheSize = unit.GB
	cfg.StorageDBConfig.CacheSize = unit.GB * 4

	return cfg
}

/*

	accountDBDir = "account"
	codeDBDir    = "code"
	storageDBDir = "storage"
	legacyDBDir  = "legacy"
	metadataDir  = "metadata"
*/
