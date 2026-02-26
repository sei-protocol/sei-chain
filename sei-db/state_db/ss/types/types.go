package types

import (
	"io"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// MvccDB is the DB engine layer contract for versioned key-value storage.
type MvccDB interface {
	Get(storeKey string, version int64, key []byte) ([]byte, error)
	Has(storeKey string, version int64, key []byte) (bool, error)
	Iterator(storeKey string, version int64, start, end []byte) (DBIterator, error)
	ReverseIterator(storeKey string, version int64, start, end []byte) (DBIterator, error)
	RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error)
	GetLatestVersion() int64
	SetLatestVersion(version int64) error
	GetEarliestVersion() int64
	SetEarliestVersion(version int64, ignoreVersion bool) error
	ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error
	ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error
	Prune(version int64) error
	Import(version int64, ch <-chan SnapshotNode) error
	io.Closer
}

// StateStore is the SS layer contract.
// Extends MvccDB; implemented by CosmosStateStore, EVMStateStore, and CompositeStateStore.
type StateStore interface {
	MvccDB
}

type DBIterator interface {
	Domain() (start []byte, end []byte)
	Valid() bool
	Next()
	Key() (key []byte)
	Value() (value []byte)
	Error() error
	Close() error
}

type SnapshotNode struct {
	StoreKey string
	Key      []byte
	Value    []byte
}

type RawSnapshotNode struct {
	StoreKey string
	Key      []byte
	Value    []byte
	Version  int64
}
