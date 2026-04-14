package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
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
// start/end are external memiavl keys; they are converted to physical keys
// for the underlying DB iterator.
func newDBIterator(db types.KeyValueDB, kind evm.EVMKeyKind, start, end []byte) Iterator {
	var physStart, physEnd []byte
	startMatches := start == nil
	endMatches := end == nil

	if start != nil {
		parsedKind, keyBytes := evm.ParseEVMKey(start)
		if parsedKind == kind {
			physStart = ktype.EVMPhysicalKey(kind, keyBytes)
			startMatches = true
		}
	}
	if end != nil {
		parsedKind, keyBytes := evm.ParseEVMKey(end)
		if parsedKind == kind {
			physEnd = ktype.EVMPhysicalKey(kind, keyBytes)
			endMatches = true
		}
	}

	if !startMatches || !endMatches {
		return &emptyIterator{}
	}

	iter, err := db.NewIter(&types.IterOptions{
		LowerBound: physStart,
		UpperBound: physEnd,
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
// strippedPrefix is the stripped key prefix (e.g. addr for storage);
// it is converted to a physical key prefix for the DB.
func newDBPrefixIterator(db types.KeyValueDB, kind evm.EVMKeyKind, strippedPrefix []byte, externalPrefix []byte) Iterator {
	physPrefix := ktype.EVMPhysicalKey(kind, strippedPrefix)
	physEnd := ktype.PrefixEnd(physPrefix)

	iter, err := db.NewIter(&types.IterOptions{
		LowerBound: physPrefix,
		UpperBound: physEnd,
	})
	if err != nil {
		return &emptyIterator{err: err}
	}

	externalEnd := ktype.PrefixEnd(externalPrefix)

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

	physKey, err := it.resolvePhysicalKey(key)
	if err != nil {
		it.err = err
		return false
	}

	if !it.iter.SeekGE(physKey) {
		return false
	}
	it.skipMetaForward()
	return it.iter.Valid()
}

func (it *dbIterator) SeekLT(key []byte) bool {
	if it.closed {
		return false
	}

	physKey, err := it.resolvePhysicalKey(key)
	if err != nil {
		it.err = err
		return false
	}

	if !it.iter.SeekLT(physKey) {
		return false
	}
	it.skipMetaBackward()
	return it.iter.Valid()
}

// resolvePhysicalKey converts a seek key to physical format for the underlying
// DB iterator. Accepts both formats so that keys returned by Key() can be
// passed directly back to SeekGE/SeekLT:
//   - Physical keys ("evm/" + prefix_byte + stripped_key) are validated and
//     passed through.
//   - Memiavl keys (prefix_byte + stripped_key) are converted via EVMPhysicalKey.
//
// Memiavl EVM prefix bytes (0x03..0x0a) are all below 0x20, while physical
// keys start with an ASCII module name (>= 0x20), so the formats are
// unambiguous.
func (it *dbIterator) resolvePhysicalKey(key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("empty seek key")
	}
	if key[0] >= 0x20 { // physical key: starts with ASCII module name; memiavl keys start with 0x03..0x0a
		kind, _, err := ktype.StripEVMPhysicalKey(key)
		if err != nil {
			return nil, fmt.Errorf("invalid physical seek key: %w", err)
		}
		if kind != it.kind {
			return nil, fmt.Errorf("physical key type mismatch: expected %d, got %d", it.kind, kind)
		}
		return key, nil
	}
	kind, strippedKey := evm.ParseEVMKey(key)
	if kind != it.kind {
		return nil, fmt.Errorf("key type mismatch: expected %d, got %d", it.kind, kind)
	}
	return ktype.EVMPhysicalKey(kind, strippedKey), nil
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
	// Returns raw physical key ("evm/" + type_prefix + stripped_key).
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
