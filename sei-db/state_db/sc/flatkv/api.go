package flatkv

import (
	"io"

	dbm "github.com/tendermint/tm-db"

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

	// SetInitialVersion seeds the store so that the next Commit produces
	// initialVersion. Must be called after LoadVersion, on a truly fresh
	// store (no prior commits) and before any writes. Returns an error on
	// a read-only store, on a non-fresh store, or for initialVersion <= 0.
	SetInitialVersion(initialVersion int64) error

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

	// RawGlobalIterator returns a positioned forward iterator over all committed
	// keys across underlying data DBs, merged in global lexicographic order.
	// Keys are physical format: "evm/" + type_prefix_byte + stripped_key.
	// Pending writes are not visible. Keys and values are read-only; copy
	// before modifying. Caller must Close when done.
	RawGlobalIterator() (dbm.Iterator, error)

	// RootHash returns the 32-byte checksum of the working LtHash.
	// Note: This is the Blake3-256 digest of the underlying 2048-byte
	// raw LtHash vector.
	RootHash() []byte

	// CommittedRootHash returns the 32-byte checksum of the last committed LtHash.
	CommittedRootHash() []byte

	// Version returns the latest committed version.
	Version() int64

	// GetLatestVersion returns the latest committed version persisted to
	// disk. Equivalent to Version() once LoadVersion has run; before
	// LoadVersion it answers from on-disk metadata so callers can
	// inspect the store's height without taking ownership of it.
	GetLatestVersion() (int64, error)

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

	// CleanupOrphanedReadOnlyDirs removes readonly-* working directories
	// left behind by a previous process crash. Must be called once at
	// process startup, before any read-only instances are created.
	CleanupOrphanedReadOnlyDirs() error

	io.Closer
}
