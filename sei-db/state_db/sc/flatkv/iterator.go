package flatkv

import (
	"bytes"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// dbIterator is a generic iterator that wraps a PebbleDB iterator
// and converts keys between internal and external (memiavl) formats.
type dbIterator struct {
	iter   types.KeyValueDBIterator
	kind   evm.EVMKeyKind // key type for conversion
	start  []byte         // external format start key
	end    []byte         // external format end key
	err    error
	closed bool
}

// Compile-time interface checks
var (
	_ Iterator = (*dbIterator)(nil)
	_ Iterator = (*emptyIterator)(nil)
)

// newDBIterator creates a new dbIterator for the given key kind.
func newDBIterator(db types.KeyValueDB, kind evm.EVMKeyKind, start, end []byte) Iterator {
	// Convert external bounds to internal bounds
	var internalStart, internalEnd []byte
	startMatches := start == nil // nil start means unbounded
	endMatches := end == nil     // nil end means unbounded

	if start != nil {
		parsedKind, keyBytes := evm.ParseEVMKey(start)
		if parsedKind == kind {
			internalStart = keyBytes
			startMatches = true
		}
	}
	if end != nil {
		parsedKind, keyBytes := evm.ParseEVMKey(end)
		if parsedKind == kind {
			internalEnd = keyBytes
			endMatches = true
		}
	}

	if !startMatches || !endMatches {
		return &emptyIterator{}
	}

	// Exclude metadata key (0x00)
	if internalStart == nil {
		internalStart = metaKeyLowerBound()
	}

	iter, err := db.NewIter(&types.IterOptions{
		LowerBound: internalStart,
		UpperBound: internalEnd,
	})
	if err != nil {
		return &emptyIterator{err: err}
	}

	return &dbIterator{
		iter:  iter,
		kind:  kind,
		start: start,
		end:   end,
	}
}

// newDBPrefixIterator creates a new dbIterator for prefix scanning.
func newDBPrefixIterator(db types.KeyValueDB, kind evm.EVMKeyKind, internalPrefix []byte, externalPrefix []byte) Iterator {
	internalEnd := PrefixEnd(internalPrefix)

	// Exclude metadata key (0x00)
	if internalPrefix == nil || bytes.Compare(internalPrefix, metaKeyLowerBound()) < 0 {
		internalPrefix = metaKeyLowerBound()
	}

	iter, err := db.NewIter(&types.IterOptions{
		LowerBound: internalPrefix,
		UpperBound: internalEnd,
	})
	if err != nil {
		return &emptyIterator{err: err}
	}

	externalEnd := PrefixEnd(externalPrefix)

	return &dbIterator{
		iter:  iter,
		kind:  kind,
		start: externalPrefix,
		end:   externalEnd,
	}
}

func (it *dbIterator) Domain() ([]byte, []byte) {
	return it.start, it.end
}

func (it *dbIterator) Valid() bool {
	if it.closed || it.err != nil {
		return false
	}
	return it.iter.Valid()
}

func (it *dbIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.iter.Error()
}

func (it *dbIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	return it.iter.Close()
}

func (it *dbIterator) First() bool {
	if it.closed {
		return false
	}
	return it.iter.First()
}

func (it *dbIterator) Last() bool {
	if it.closed {
		return false
	}
	return it.iter.Last()
}

func (it *dbIterator) SeekGE(key []byte) bool {
	if it.closed {
		return false
	}

	kind, internalKey := evm.ParseEVMKey(key)
	if kind != it.kind {
		it.err = fmt.Errorf("key type mismatch: expected %d, got %d", it.kind, kind)
		return false
	}

	return it.iter.SeekGE(internalKey)
}

func (it *dbIterator) SeekLT(key []byte) bool {
	if it.closed {
		return false
	}

	kind, internalKey := evm.ParseEVMKey(key)
	if kind != it.kind {
		it.err = fmt.Errorf("key type mismatch: expected %d, got %d", it.kind, kind)
		return false
	}

	return it.iter.SeekLT(internalKey)
}

func (it *dbIterator) Next() bool {
	if it.closed {
		return false
	}
	return it.iter.Next()
}

func (it *dbIterator) Prev() bool {
	if it.closed {
		return false
	}
	return it.iter.Prev()
}

func (it *dbIterator) Kind() evm.EVMKeyKind {
	return it.kind
}

func (it *dbIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}
	// Return internal key format (without memiavl prefix)
	return it.iter.Key()
}

func (it *dbIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}
	return it.iter.Value()
}

// CommitStore factory methods for creating iterators

func (s *CommitStore) newStorageIterator(start, end []byte) Iterator {
	return newDBIterator(s.storageDB, evm.EVMKeyStorage, start, end)
}

func (s *CommitStore) newStoragePrefixIterator(internalPrefix, internalEnd []byte, memiavlPrefix []byte) Iterator {
	return newDBPrefixIterator(s.storageDB, evm.EVMKeyStorage, internalPrefix, memiavlPrefix)
}

func (s *CommitStore) newCodeIterator(start, end []byte) Iterator {
	return newDBIterator(s.codeDB, evm.EVMKeyCode, start, end)
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
func (it *emptyIterator) Kind() evm.EVMKeyKind     { return evm.EVMKeyUnknown }
func (it *emptyIterator) Key() []byte              { return nil }
func (it *emptyIterator) Value() []byte            { return nil }
