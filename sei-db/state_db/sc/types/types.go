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
	Initialize(initialStores []string)

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

	// Get returns the value for a key in a given store.
	//
	// Get(store, key) is a replacement for GetChildStoreByName(store).Get(key). The
	// GetChildStoreByName(store).Get(key) pathway is deprecated.
	Get(store string, key []byte) (value []byte, ok bool, err error)

	// Has returns true if a key exists in a given store.
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
	//
	// Note that this method may not be supported for some stores. For example, after evm/ data is migrated to flatKV,
	// iteration is not supported for the evm/ store. If this method is called for an unsupported store, it will return
	// an error.
	//
	// Iterator(store, start, end, ascending) is a replacement for
	// GetChildStoreByName(store).Iterator(start, end, ascending), which is a deprecated pathway.
	Iterator(store string, start []byte, end []byte, ascending bool) (dbm.Iterator, error)

	// GetProof returns a proof of the value for a key in a given store.
	//
	// Note that this method may not be supported for some stores. For example, after evm/ data is migrated to flatKV,
	// proofs are not supported for the evm/ store. If this method is called for an unsupported store, it will return
	// an error.
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
	// store, or nil if no such store exists. The returned store shares
	// state with the Committer and must not be used after Close.
	GetChildStoreByName(name string) CommitKVStore

	// Importer returns an Importer that ingests state at the given version,
	// typically used to restore from a state-sync snapshot. The caller owns
	// the returned Importer and must Close it when finished.
	Importer(version int64) (Importer, error)

	// Exporter returns an Exporter that streams the state at the given
	// version, typically used to produce a state-sync snapshot. The caller
	// owns the returned Exporter and must Close it when finished.
	Exporter(version int64) (Exporter, error)

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
