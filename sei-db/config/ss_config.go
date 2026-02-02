package config

const (
	DefaultSSKeepRecent    = 100000
	DefaultSSPruneInterval = 600
	DefaultSSImportWorkers = 1
	DefaultSSAsyncBuffer   = 100
)

// StateStoreConfig defines configuration for the state store (SS) layer.
type StateStoreConfig struct {
	// Enable defines if the state-store should be enabled for historical queries.
	Enable bool `mapstructure:"enable"`

	// DBDirectory overrides the directory to store the state store db files
	// If not explicitly set, default to application home directory
	// default to empty
	DBDirectory string `mapstructure:"db-directory"`

	// Backend defines the backend database used for state-store
	// Supported backends: pebbledb, rocksdb
	// defaults to pebbledb
	Backend string `mapstructure:"backend"`

	// AsyncWriteBuffer defines the async queue length for commits to be applied to State Store
	// Set <= 0 for synchronous writes, which means commits also need to wait for data to be persisted in State Store.
	// defaults to 100
	AsyncWriteBuffer int `mapstructure:"async-write-buffer"`

	// KeepRecent defines the number of versions to keep in state store
	// Setting it to 0 means keep everything.
	// Default to keep the last 100,000 blocks
	KeepRecent int `mapstructure:"keep-recent"`

	// PruneIntervalSeconds defines the interval in seconds to trigger pruning
	// default to every 600 seconds
	PruneIntervalSeconds int `mapstructure:"prune-interval-seconds"`

	// ImportNumWorkers defines the number of goroutines used during import
	// defaults to 1
	ImportNumWorkers int `mapstructure:"import-num-workers"`

	// KeepLastVersion defines whether to keep last version of a key during pruning or delete
	// defaults to true
	KeepLastVersion bool `mapstructure:"keep-last-version"`

	// UseDefaultComparer uses Pebble's default lexicographic byte comparer instead of
	// the custom MVCCComparer. This is NOT backwards compatible with existing databases
	// that were created with MVCCComparer - only use this for NEW databases.
	// The MVCC key encoding uses big-endian version bytes, so ordering is compatible,
	// but existing databases will fail to open due to comparer name mismatch.
	// defaults to false (use MVCCComparer for backwards compatibility)
	UseDefaultComparer bool `mapstructure:"use-default-comparer"`
}

// EVMStateStoreConfig defines configuration for the separate EVM state stores.
// EVM stores use default comparer and separate PebbleDBs for each key type
// (storage, nonce, code, codehash, codesize).
type EVMStateStoreConfig struct {
	// Enable defines if the EVM state stores should be enabled.
	// When enabled, EVM data is dual-written to separate optimized databases.
	// Reads check EVM_SS first, then fallback to Cosmos_SS.
	// defaults to false
	Enable bool `mapstructure:"enable"`

	// DBDirectory defines the directory to store the EVM state store db files
	// If not explicitly set, defaults to <home>/data/evm_ss
	DBDirectory string `mapstructure:"db-directory"`

	// KeepRecent defines the number of versions to keep in EVM state stores
	// Setting it to 0 means keep everything.
	// Default to keep the last 100,000 blocks
	KeepRecent int `mapstructure:"keep-recent"`
}

// DefaultStateStoreConfig returns the default StateStoreConfig
func DefaultStateStoreConfig() StateStoreConfig {
	return StateStoreConfig{
		Enable:               true,
		Backend:              "pebbledb",
		AsyncWriteBuffer:     DefaultSSAsyncBuffer,
		KeepRecent:           DefaultSSKeepRecent,
		PruneIntervalSeconds: DefaultSSPruneInterval,
		ImportNumWorkers:     DefaultSSImportWorkers,
		KeepLastVersion:      true,
		UseDefaultComparer:   false,
	}
}

func DefaultEVMStateStoreConfig() EVMStateStoreConfig {
	return EVMStateStoreConfig{
		Enable:     false, // Disabled by default, enable for optimized EVM storage
		KeepRecent: DefaultSSKeepRecent,
	}
}
