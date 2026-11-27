package types

import (
	"io"

	ics23 "github.com/confio/ics23/go"
	dbm "github.com/tendermint/tm-db"
)

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

	// AddNode queues node for import. A non-nil error means node was not accepted and the import cannot
	// complete.
	AddNode(node *SnapshotNode) error

	// Abort discards the import in place of Close, publishing none of it. The returned error may be
	// reason itself, or another error that already ended the import.
	Abort(reason error) error

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
