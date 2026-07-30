package types

import (
	"io"

	ics23 "github.com/confio/ics23/go"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Committer is the unified write-side interface for the state-commitment
// layer. Implementations persist multi-store state, expose commit metadata
// for app-hash computation, and provide hooks for rollback, state-sync, and
// version management.
//
// Lifecycle: a Committer is constructed but not opened. Initialize records
// the initial set of child stores, then LoadVersion opens the underlying
// databases. Close releases all backing resources.
type Committer interface {
	// Initialize records the set of child store (tree) names that should be
	// created when the database is first opened with no prior state. It has
	// no effect on a non-empty database. Must be called before LoadVersion.
	// Rejects store names not in keys.MemIAVLStoreKeys.
	Initialize(initialStores []string) error

	// Commit persists the current working state and returns the version
	// number assigned to the new commit. After a successful Commit the
	// working state advances to the next height.
	Commit() (int64, error)

	// Version returns the version of the currently loaded in-memory state,
	// i.e. the height of the most recent successful Commit (or LoadVersion).
	Version() int64

	// GetLatestVersion returns the highest version that has been committed
	// to disk. This may differ from Version when a read-only handle has
	// been opened at an older height.
	GetLatestVersion() (int64, error)

	// Get returns the value for a key in a given store. Returns an error
	// if store is not routable (i.e. not a member of
	// keys.MemIAVLStoreKeys).
	//
	// Get(store, key) is a replacement for GetChildStoreByName(store).Get(key). The
	// GetChildStoreByName(store).Get(key) pathway is deprecated.
	Get(store string, key []byte) (value []byte, ok bool, err error)

	// Has returns true if a key exists in a given store. Returns an error
	// if store is not routable (i.e. not a member of
	// keys.MemIAVLStoreKeys).
	//
	// Has(store, key) is a replacement for GetChildStoreByName(store).Has(key). The
	// GetChildStoreByName(store).Has(key) pathway is deprecated.
	Has(store string, key []byte) (bool, error)

	// ApplyChangeSets stages a batch of per-store key/value mutations on top
	// of the current working state. Changes are not durable until Commit is
	// called. Passing an empty slice is a no-op. Composite implementations
	// route change sets to the appropriate backend by store name.
	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	// Iterator returns an iterator over a range of keys in a given store.
	// Returns an error if store is not routable (i.e. not a member of
	// keys.MemIAVLStoreKeys), or if the routed backend does not support
	// iteration for that store (e.g. evm/ once migrated to flatKV).
	//
	// Iterator(store, start, end, ascending) is a replacement for
	// GetChildStoreByName(store).Iterator(start, end, ascending), which is a deprecated pathway.
	Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error)

	// GetProof returns a proof of the value for a key in a given store.
	// Returns an error if store is not routable (i.e. not a member of
	// keys.MemIAVLStoreKeys), or if the routed backend does not support
	// proofs for that store (e.g. evm/ once migrated to flatKV).
	//
	// GetProof(store, key) is a replacement for GetChildStoreByName(store).GetProof(key), which is a deprecated
	// pathway.
	GetProof(store string, key []byte) (*ics23.CommitmentProof, error)

	// ApplyUpgrades applies tree-level structural changes (adding, renaming
	// or deleting child stores) to the working state. The operation is
	// idempotent: upgrades referencing trees that already match the desired
	// state are skipped. Passing an empty slice is a no-op.
	ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error

	// WorkingCommitInfo returns the CommitInfo describing the uncommitted
	// working state, suitable for computing the app hash before Commit.
	WorkingCommitInfo() *proto.CommitInfo

	// LastCommitInfo returns the CommitInfo of the most recent persisted
	// commit. The returned value reflects on-disk state and is unaffected
	// by uncommitted ApplyChangeSets / ApplyUpgrades calls.
	LastCommitInfo() *proto.CommitInfo

	// LoadVersion opens the database at targetVersion (0 means the latest
	// persisted version).
	//
	// When readOnly is true, an isolated read-only Committer is returned
	// for use by concurrent queriers; the receiver is left untouched and
	// the caller is responsible for closing the returned handle.
	//
	// When readOnly is false, the receiver is closed and reopened at
	// targetVersion, and the receiver itself is returned. This is the
	// standard path used during node startup.
	LoadVersion(targetVersion int64, readOnly bool) (Committer, error)

	// Rollback truncates state back to targetVersion, discarding all newer
	// versions. The change is durable: the underlying WAL/changelog is
	// rewritten so the rollback persists across process restarts. Existing
	// handles are closed and reopened as part of the operation.
	Rollback(targetVersion int64) error

	// SetInitialVersion sets the version number that will be assigned to
	// the very first Commit. It must be called before any commit has been
	// made; calling it on a database with existing commits returns an
	// error.
	SetInitialVersion(initialVersion int64) error

	// GetChildStoreByName returns the CommitKVStore for the named child
	// store. Method calls on the returned store panic if name is not
	// routable. The returned store shares state with the Committer and
	// must not be used after Close.
	GetChildStoreByName(name string) CommitKVStore

	// Copy returns an in-memory snapshot of the current committer state.
	// O(1) for memiavl. Returns nil when the backend can't produce one
	// (e.g. flatkv) — callers should treat nil as "snapshot unavailable"
	// and fall back to the disk-backed path.
	Copy() Committer

	// Importer returns an Importer that ingests state at the given version,
	// typically used to restore from a state-sync snapshot. The caller owns
	// the returned Importer and must Close it when finished.
	Importer(version int64) (Importer, error)

	// Exporter returns an Exporter that streams the state at the given
	// version, typically used to produce a state-sync snapshot. The caller
	// owns the returned Exporter and must Close it when finished.
	Exporter(version int64) (Exporter, error)

	// SetWriteMode transitions the store's effective write mode at
	// runtime.
	//
	// Stores whose write mode is fixed — by configuration, or by
	// construction for single-backend stores — return an error and are
	// otherwise unaffected.
	//
	// Must be called between blocks: after Commit has completed (all
	// write buffers flushed) and before the next block's first write
	// batch. It is not synchronized against concurrent commits. See the
	// implementing store's documentation for the transition-legality and
	// trigger-determinism requirements.
	SetWriteMode(mode WriteMode) error

	// SetMigrationBatchSize sets the number of keys the in-flight
	// migration advances per block. Stores with no migration in progress
	// (single-backend committers) treat it as a no-op.
	//
	// A value of 0 pauses the migration: caller writes still route
	// normally, but no keys are pulled forward until the size is raised
	// again. This governance-supplied value is the sole source of the
	// per-block rate; there is no node-local config fallback.
	//
	// Must be called between blocks, before the next block's first write
	// batch, on the consensus goroutine.
	SetMigrationBatchSize(batchSize int) error

	// Closer releases all backing resources (open files, background
	// goroutines, locks). After Close the Committer must not be used.
	io.Closer
}

// Planed for future deprecation but not yet deprecated.
type CommitKVStore interface {
	// Planed for future deprecation but not yet deprecated.
	// This is the proper method to call to get a value from the store.
	Get(key []byte) []byte

	// Planed for future deprecation but not yet deprecated. This is the proper method to call to check
	// if a key exists in the store.
	Has(key []byte) bool

	// Deprected: do not call in production code, use CommitKVStore.ApplyChangeSets() instead.
	Set(key, value []byte)

	// Deprected: do not call in production code, use CommitKVStore.ApplyChangeSets() instead.
	Remove(key []byte)

	// Deprected but safe to call
	Version() int64

	// Deprected: do not call in production code, may panic on stores backed by flatKV
	RootHash() []byte

	// Partially deprecated: may panic if called on a store that does not support iteration (e.g. evm/ after migration)
	Iterator(start, end []byte, ascending bool) dbm.Iterator

	// Partially deprecated: may panic if called on a store that does not support proofs (e.g. evm/ after migration)
	GetProof(key []byte) *ics23.CommitmentProof

	// deprecated: some implementations always return errors, and ones that don't mean you are closing something
	// that shouldn't be closed directly
	io.Closer
}

type Importer interface {
	AddModule(name string) error

	AddNode(node *SnapshotNode)

	io.Closer
}

type Exporter interface {
	Next() (interface{}, error)

	io.Closer
}

// SnapshotNode contains import/export node data.
type SnapshotNode struct {
	Key     []byte
	Value   []byte
	Version int64
	Height  int8
}
