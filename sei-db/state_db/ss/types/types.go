// Package types declares the contracts of the Cosmos state store (SS): the versioned key-value store
// that serves historical reads, and the capabilities its implementations may offer.
package types

import (
	"context"
	"io"

	dbm "github.com/tendermint/tm-db"

	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// StateStore is the unified interface for versioned key-value storage.
// Implemented by pebbledb/mvcc.Database, rocksdb/mvcc.Database,
// CosmosStateStore, EVMStateStore, and CompositeStateStore.
type StateStore interface {
	Get(storeKey string, version int64, key []byte) ([]byte, error)
	Has(storeKey string, version int64, key []byte) (bool, error)
	Iterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error)
	ReverseIterator(storeKey string, version int64, start, end []byte) (dbm.Iterator, error)
	RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error)
	GetLatestVersion() int64
	SetLatestVersion(version int64) error
	GetEarliestVersion() int64
	SetEarliestVersion(version int64, ignoreVersion bool) error
	ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error
	ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error
	Prune(version int64) error
	Import(version int64, ch <-chan dbtypes.SnapshotNode) error
	io.Closer
}

// ContextIteratorStore is implemented by StateStores whose iterators can observe
// a deadline while skipping MVCC versions. Historical traces attach the RPC
// timeout here so a skip loop does not run for minutes after the caller gave up.
type ContextIteratorStore interface {
	IteratorWithContext(ctx context.Context, storeKey string, version int64, start, end []byte) (dbm.Iterator, error)
	ReverseIteratorWithContext(ctx context.Context, storeKey string, version int64, start, end []byte) (dbm.Iterator, error)
}

// IterateWithContext prefers ContextIteratorStore when the store implements it.
func IterateWithContext(store StateStore, ctx context.Context, storeKey string, version int64, start, end []byte, reverse bool) (dbm.Iterator, error) {
	if c, ok := store.(ContextIteratorStore); ok {
		if reverse {
			return c.ReverseIteratorWithContext(ctx, storeKey, version, start, end)
		}
		return c.IteratorWithContext(ctx, storeKey, version, start, end)
	}
	if reverse {
		return store.ReverseIterator(storeKey, version, start, end)
	}
	return store.Iterator(storeKey, version, start, end)
}
