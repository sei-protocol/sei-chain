package flatkv

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
)

// TODO before merge: the iteration model is broken — dbcache.Cache does not
// expose a NewIter method, so we can't iterate over the backing store through
// the cache. Options:
//   1. Add an Iterator() method to dbcache.Cache that merges versioned data
//      with the underlying DB iterator.
//   2. Bypass the cache and iterate the raw DB directly (stale reads are
//      acceptable for export/state-sync).
//   3. Snapshot the cache, flush, then iterate the DB.
// For now, all iterator construction returns emptyIterator so the package
// compiles and benchmarks can run.

// dbIterator is a generic iterator that wraps a PebbleDB iterator
// and converts keys between internal and external (memiavl) formats.
//
// EXPERIMENTAL: not used in production; only storage keys supported.
// Interface may change when Exporter/state-sync is implemented.
// TODO before merge: restore dbIterator once the iteration model is resolved.
// type dbIterator struct {
// 	iter   types.KeyValueDBIterator
// 	kind   evm.EVMKeyKind
// 	start  []byte
// 	end    []byte
// 	err    error
// 	closed bool
// }

// Compile-time interface checks
var (
	_ Iterator = (*emptyIterator)(nil)
)

// TODO before merge: restore newDBIterator to create a real iterator once
// dbcache.Cache supports iteration or we decide on an alternative approach.
func newDBIterator(_ interface{}, _ evm.EVMKeyKind, _, _ []byte) Iterator {
	return &emptyIterator{}
}

// TODO before merge: restore newDBPrefixIterator (same as newDBIterator above).
func newDBPrefixIterator(_ interface{}, _ evm.EVMKeyKind, _ []byte, _ []byte) Iterator {
	return &emptyIterator{}
}

// CommitStore factory methods for creating iterators

// TODO before merge: these return empty iterators until the iteration model is resolved.
func (s *CommitStore) newStorageIterator(start, end []byte) Iterator {
	return newDBIterator(s.storageDB, evm.EVMKeyStorage, start, end)
}

// TODO before merge: returns empty iterator until the iteration model is resolved.
func (s *CommitStore) newStoragePrefixIterator(internalPrefix []byte, memiavlPrefix []byte) Iterator {
	return newDBPrefixIterator(s.storageDB, evm.EVMKeyStorage, internalPrefix, memiavlPrefix)
}

// emptyIterator is used when no data matches the query.
// If err is set, it indicates a creation failure (e.g. PebbleDB error).
type emptyIterator struct {
	err error
}

func (it *emptyIterator) Domain() ([]byte, []byte) { return nil, nil }
func (it *emptyIterator) Valid() bool              { return false }
func (it *emptyIterator) Error() error             { return it.err }
func (it *emptyIterator) Close() error             { return nil }
func (it *emptyIterator) First() bool              { return false }
func (it *emptyIterator) Last() bool               { return false }
func (it *emptyIterator) SeekGE(key []byte) bool   { return false }
func (it *emptyIterator) SeekLT(key []byte) bool   { return false }
func (it *emptyIterator) Next() bool               { return false }
func (it *emptyIterator) Prev() bool               { return false }
func (it *emptyIterator) Key() []byte              { return nil }
func (it *emptyIterator) Value() []byte            { return nil }
