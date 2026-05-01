package types

import (
	"io"

	ics23 "github.com/confio/ics23/go"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

type Committer interface {
	Initialize(initialStores []string)

	Commit() (int64, error)

	Version() int64

	GetLatestVersion() (int64, error)

	GetEarliestVersion() (int64, error)

	ApplyChangeSets(cs []*proto.NamedChangeSet) error

	ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error

	WorkingCommitInfo() *proto.CommitInfo

	LastCommitInfo() *proto.CommitInfo

	LoadVersion(targetVersion int64, readOnly bool) (Committer, error)

	Rollback(targetVersion int64) error

	SetInitialVersion(initialVersion int64) error

	GetChildStoreByName(name string) CommitKVStore

	// Copy returns an in-memory snapshot of the current committer state.
	// O(1) for memiavl. Returns nil when the backend can't produce one
	// (e.g. flatkv) — callers should treat nil as "snapshot unavailable"
	// and fall back to the disk-backed path.
	Copy() Committer

	Importer(version int64) (Importer, error)

	Exporter(version int64) (Exporter, error)

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
