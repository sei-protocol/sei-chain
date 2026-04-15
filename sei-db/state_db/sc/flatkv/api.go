package flatkv

import (
	"io"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// Options configures a FlatKV store.
type Options struct {
	// Dir is the base directory containing snapshot dirs, working/, and changelog/.
	Dir string
}

// Store provides EVM state storage with LtHash integrity.
//
// Lifecycle: NewCommitStore (create) → LoadVersion (open) → ApplyChangeSets/Commit → Close.
// Write path: ApplyChangeSets (buffer) → Commit (persist).
// Read path: Get/Has/Iterator read committed state only.
// Key format: x/evm memiavl keys (mapped internally to account/code/storage DBs).
type Store interface {
	// LoadVersion opens the database at the given version (0 = latest).
	// When readOnly is true an isolated, read-only store is returned;
	// the caller must Close it when done.
	LoadVersion(targetVersion int64, readOnly bool) (Store, error)

	// ApplyChangeSets buffers EVM changesets (x/evm memiavl keys) and updates LtHash.
	// Non-EVM modules are routed into legacy storage under their module prefix.
	// Call Commit to persist.
	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	// Commit persists buffered writes and advances the version.
	Commit() (int64, error)

	// Get returns the value for a key within the given module.
	// For EVM keys (moduleName == "evm"), the key is a memiavl EVM key
	// routed to account/storage/code/legacy DBs internally.
	// For non-EVM modules, the key is read from legacy storage with the module prefix.
	// If not found, returns (nil, false).
	Get(moduleName string, key []byte) (value []byte, found bool)

	// GetBlockHeightModified returns the block height at which the key was last modified.
	// Only supported for EVM keys; non-EVM legacy data does not track block height.
	// If not found, returns (-1, false, nil).
	GetBlockHeightModified(moduleName string, key []byte) (int64, bool, error)

	// Has reports whether the key exists within the given module.
	Has(moduleName string, key []byte) bool

	// Iterator returns an iterator over [start, end) in memiavl key order.
	// Pass nil for unbounded.
	//
	// EXPERIMENTAL: not used in production; only storage keys supported.
	// Interface may change when Exporter/state-sync is implemented.
	Iterator(start, end []byte) Iterator

	// IteratorByPrefix iterates all keys with the given prefix (more efficient than Iterator).
	// Currently only supports: StateKeyPrefix||addr (storage iteration).
	//
	// EXPERIMENTAL: not used in production; only storage keys supported.
	// Interface may change when Exporter/state-sync is implemented.
	IteratorByPrefix(prefix []byte) Iterator

	// RootHash returns the 32-byte checksum of the working LtHash.
	// Note: This is the Blake3-256 digest of the underlying 2048-byte
	// raw LtHash vector.
	RootHash() []byte

	// CommittedRootHash returns the 32-byte checksum of the last committed LtHash.
	CommittedRootHash() []byte

	// Version returns the latest committed version.
	Version() int64

	// WriteSnapshot writes a complete snapshot to dir.
	WriteSnapshot(dir string) error

	// Rollback restores state to targetVersion by rewinding to the best
	// snapshot, replaying WAL, and pruning snapshots/WAL beyond target.
	Rollback(targetVersion int64) error

	// Exporter creates an exporter for the given version (0 = current).
	Exporter(version int64) (types.Exporter, error)

	// Importer load data from snapshot to the database
	Importer(version int64) (types.Importer, error)

	// Get the phase timer used to measure time spent in various phases of execution. Useful for metrics
	// integration with external phases of execution.
	GetPhaseTimer() *metrics.PhaseTimer

	io.Closer
}

// Iterator provides ordered iteration over EVM keys.
// Follows PebbleDB semantics: not positioned on creation.
//
// Keys are returned in physical format ("evm/" + type_prefix_byte + stripped_key).
// SeekGE/SeekLT accept both physical keys and memiavl keys (prefix_byte + stripped_key).
//
// EXPERIMENTAL: not used in production. Interface may change when
// Exporter/state-sync is implemented.
type Iterator interface {
	Domain() (start, end []byte)
	Valid() bool
	Error() error
	Close() error

	First() bool
	Last() bool
	SeekGE(key []byte) bool
	SeekLT(key []byte) bool
	Next() bool
	Prev() bool

	// Key returns the current key in physical format (valid until next move).
	// Physical format: "evm/" + type_prefix_byte + stripped_key.
	Key() []byte

	// Value returns the current value (valid until next move).
	Value() []byte
}
