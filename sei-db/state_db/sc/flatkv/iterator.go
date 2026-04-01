package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// dbIterator is a generic iterator that wraps a PebbleDB iterator
// and converts keys between internal and external (memiavl) formats.
//
// EXPERIMENTAL: not used in production; only storage keys supported.
// Interface may change when Exporter/state-sync is implemented.
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
	if !it.iter.First() {
		return false
	}
	it.skipMetaForward()
	return it.iter.Valid()
}

func (it *dbIterator) Last() bool {
	if it.closed {
		return false
	}
	if !it.iter.Last() {
		return false
	}
	it.skipMetaBackward()
	return it.iter.Valid()
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

	if !it.iter.SeekGE(internalKey) {
		return false
	}
	it.skipMetaForward()
	return it.iter.Valid()
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

	if !it.iter.SeekLT(internalKey) {
		return false
	}
	it.skipMetaBackward()
	return it.iter.Valid()
}

func (it *dbIterator) Next() bool {
	if it.closed {
		return false
	}
	if !it.iter.Next() {
		return false
	}
	it.skipMetaForward()
	return it.iter.Valid()
}

func (it *dbIterator) Prev() bool {
	if it.closed {
		return false
	}
	if !it.iter.Prev() {
		return false
	}
	it.skipMetaBackward()
	return it.iter.Valid()
}

// skipMetaForward advances past any _meta/ keys.
// On I/O error Valid() becomes false and the loop exits;
// the caller surfaces the error via Error().
func (it *dbIterator) skipMetaForward() {
	for it.iter.Valid() && isMetaKey(it.iter.Key()) {
		it.iter.Next()
	}
}

// skipMetaBackward retreats past any _meta/ keys.
// Error handling mirrors skipMetaForward.
func (it *dbIterator) skipMetaBackward() {
	for it.iter.Valid() && isMetaKey(it.iter.Key()) {
		it.iter.Prev()
	}
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
	raw := it.iter.Value()
	switch it.kind {
	case evm.EVMKeyStorage:
		sd, err := vtype.DeserializeStorageData(raw)
		if err != nil {
			it.err = fmt.Errorf("deserialize storage value: %w", err)
			return nil
		}
		return sd.GetValue()[:]
	default:
		return raw
	}
}

// CommitStore factory methods for creating iterators

func (s *CommitStore) newStorageIterator(start, end []byte) Iterator {
	return newDBIterator(s.storageDB, evm.EVMKeyStorage, start, end)
}

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
