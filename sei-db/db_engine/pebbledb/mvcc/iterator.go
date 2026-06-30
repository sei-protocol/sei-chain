package mvcc

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/cockroachdb/pebble/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/exp/slices"

	dbm "github.com/tendermint/tm-db"

	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

var _ dbm.Iterator = (*iterator)(nil)

// iterator implements the Iterator interface. It wraps a PebbleDB iterator
// with added MVCC key handling logic. The iterator will iterate over the key space
// in the provided domain for a given version. If a key has been written at the
// provided version, that key/value pair will be iterated over. Otherwise, the
// latest version for that key/value pair will be iterated over s.t. it's less
// than the provided version. The start key must not be empty.
type iterator struct {
	source             *pebble.Iterator
	prefix, start, end []byte
	version            int64
	valid              bool
	reverse            bool
	useDefaultComparer bool
	iterationCount     int64
	readCount          int64
	storeKey           string
	operationMetrics   *pebbledbmetrics.OperationMetrics

	closeSync sync.Once
}

func newPebbleDBIterator(src *pebble.Iterator, prefix, mvccStart, mvccEnd []byte, version int64, earliestVersion int64, reverse bool, useDefaultComparer bool, storeKey string, operationMetrics *pebbledbmetrics.OperationMetrics) *iterator {
	// Return invalid iterator if requested iterator height is lower than earliest version after pruning
	if version < earliestVersion {
		return &iterator{
			source:             src,
			prefix:             prefix,
			start:              mvccStart,
			end:                mvccEnd,
			version:            version,
			valid:              false,
			reverse:            reverse,
			useDefaultComparer: useDefaultComparer,
			storeKey:           storeKey,
			operationMetrics:   operationMetrics,
		}
	}

	// move the underlying PebbleDB iterator to the first key
	var valid bool
	if reverse {
		valid = src.Last()
	} else {
		valid = src.First()
	}

	itr := &iterator{
		source:             src,
		prefix:             prefix,
		start:              mvccStart,
		end:                mvccEnd,
		version:            version,
		valid:              valid,
		reverse:            reverse,
		useDefaultComparer: useDefaultComparer,
		storeKey:           storeKey,
		operationMetrics:   operationMetrics,
	}

	if valid {
		currKey, _, ok := SplitMVCCKey(itr.source.Key())
		if !ok {
			// XXX: This should not happen as that would indicate we have a malformed MVCC key.
			panic(fmt.Sprintf("invalid PebbleDB MVCC key: %s", itr.source.Key()))
		}
		if reverse {
			itr.positionAtOrBeforeKey(currKey)
		} else {
			itr.positionAtOrAfterKey(currKey)
		}
	}

	// Make sure we skip to the next key if the current one is tombstone
	// Only check if iterator is still valid after the seek/next operations above
	if itr.valid && valTombstoned(itr.source.Value()) {
		if reverse {
			itr.nextReverse()
		} else {
			itr.nextForward()
		}
	}
	if itr.Valid() {
		itr.readCount = 1
	}

	return itr
}

func (itr *iterator) seekVisibleVersionForKey(targetKey []byte) bool {
	seekKey := MVCCEncodeDescending(targetKey, itr.version)
	valid := itr.source.SeekGE(seekKey)
	if !valid {
		return false
	}

	foundKey, foundVersion, ok := SplitMVCCKey(itr.source.Key())
	if !ok {
		return false
	}
	if !bytes.Equal(foundKey, targetKey) {
		return false
	}
	foundVersionDecoded, err := decodeUint64Descending(foundVersion)
	if err != nil {
		return false
	}
	return foundVersionDecoded <= itr.version
}

func (itr *iterator) nextLogicalKey(currKey []byte) ([]byte, bool) {
	if itr.useDefaultComparer {
		return itr.nextLogicalKeyByScan(currKey)
	}
	seekKey := MVCCComparer.ImmediateSuccessor(nil, MVCCEncodeDescending(currKey, 0))
	valid := itr.source.SeekGE(seekKey)
	if !valid {
		return nil, false
	}
	nextKey, _, ok := SplitMVCCKey(itr.source.Key())
	if !ok || !bytes.HasPrefix(nextKey, itr.prefix) {
		return nil, false
	}
	return nextKey, true
}

func (itr *iterator) nextLogicalKeyByScan(currKey []byte) ([]byte, bool) {
	for valid := itr.source.Next(); valid; valid = itr.source.Next() {
		nextKey, _, ok := SplitMVCCKey(itr.source.Key())
		if !ok || !bytes.HasPrefix(nextKey, itr.prefix) {
			return nil, false
		}
		if !bytes.Equal(nextKey, currKey) {
			return nextKey, true
		}
	}
	return nil, false
}

func (itr *iterator) prevLogicalKey(currKey []byte) ([]byte, bool) {
	seekKey := MVCCEncodeDescending(currKey, math.MaxInt64)
	valid := itr.source.SeekLT(seekKey)
	if !valid {
		return nil, false
	}
	prevKey, _, ok := SplitMVCCKey(itr.source.Key())
	if !ok || !bytes.HasPrefix(prevKey, itr.prefix) {
		return nil, false
	}
	return prevKey, true
}

func (itr *iterator) positionAtOrAfterKey(startKey []byte) {
	currentKey := startKey
	for {
		itr.valid = itr.seekVisibleVersionForKey(currentKey)
		if itr.valid && !itr.cursorTombstoned() {
			return
		}
		nextKey, ok := itr.nextLogicalKey(currentKey)
		if !ok {
			itr.valid = false
			return
		}
		currentKey = nextKey
	}
}

func (itr *iterator) positionAtOrBeforeKey(startKey []byte) {
	currentKey := startKey
	for {
		itr.valid = itr.seekVisibleVersionForKey(currentKey)
		if itr.valid && !itr.cursorTombstoned() {
			return
		}
		prevKey, ok := itr.prevLogicalKey(currentKey)
		if !ok {
			itr.valid = false
			return
		}
		currentKey = prevKey
	}
}

// Domain returns the domain of the iterator. The caller must not modify the
// return values.
func (itr *iterator) Domain() ([]byte, []byte) {
	return itr.start, itr.end
}

func (itr *iterator) Key() []byte {
	itr.assertIsValid()

	key, _, ok := SplitMVCCKey(itr.source.Key())
	if !ok {
		// XXX: This should not happen as that would indicate we have a malformed
		// MVCC key.
		panic(fmt.Sprintf("invalid PebbleDB MVCC key: %s", itr.source.Key()))
	}

	keyCopy := slices.Clone(key)
	return keyCopy[len(itr.prefix):]
}

func (itr *iterator) Value() []byte {
	itr.assertIsValid()

	val, _, ok := SplitMVCCKey(itr.source.Value())
	if !ok {
		// XXX: This should not happen as that would indicate we have a malformed
		// MVCC value.
		panic(fmt.Sprintf("invalid PebbleDB MVCC value: %s", itr.source.Key()))
	}

	return slices.Clone(val)
}

func (itr *iterator) nextForward() {
	if !itr.source.Valid() {
		itr.valid = false
		return
	}

	currKey, _, ok := SplitMVCCKey(itr.source.Key())
	if !ok {
		// XXX: This should not happen as that would indicate we have a malformed
		// MVCC key.
		panic(fmt.Sprintf("invalid PebbleDB MVCC key: %s", itr.source.Key()))
	}
	nextKey, ok := itr.nextLogicalKey(currKey)
	if !ok {
		itr.valid = false
		return
	}
	itr.positionAtOrAfterKey(nextKey)
}

func (itr *iterator) nextReverse() {
	if !itr.source.Valid() {
		itr.valid = false
		return
	}

	currKey, _, ok := SplitMVCCKey(itr.source.Key())
	if !ok {
		// XXX: This should not happen as that would indicate we have a malformed
		// MVCC key.
		panic(fmt.Sprintf("invalid PebbleDB MVCC key: %s", itr.source.Key()))
	}

	prevKey, ok := itr.prevLogicalKey(currKey)
	if !ok {
		itr.valid = false
		return
	}
	itr.positionAtOrBeforeKey(prevKey)
}

func (itr *iterator) Next() {
	itr.iterationCount++

	if itr.reverse {
		itr.nextReverse()
	} else {
		itr.nextForward()
	}
	if itr.Valid() {
		itr.readCount++
	}
}

func (itr *iterator) Valid() bool {
	// once invalid, forever invalid
	if !itr.valid || !itr.source.Valid() {
		itr.valid = false
		return itr.valid
	}

	// if source has error, consider it invalid
	if err := itr.source.Error(); err != nil {
		itr.valid = false
		return itr.valid
	}

	// if key is at the end or past it, consider it invalid
	if end := itr.end; end != nil {
		if bytes.Compare(end, itr.Key()) <= 0 {
			itr.valid = false
			return itr.valid
		}
	}

	return true
}

func (itr *iterator) Error() error {
	return itr.source.Error()
}

func (itr *iterator) Close() error {
	itr.closeSync.Do(func() {
		_ = itr.source.Close()
		itr.source = nil
		itr.valid = false

		// Record the number of iterations performed by this iterator
		otelMetrics.iteratorIterations.Record(
			context.Background(),
			float64(itr.iterationCount),
			metric.WithAttributes(
				attribute.Bool("reverse", itr.reverse),
				attribute.String("store", itr.storeKey),
			),
		)
		if itr.operationMetrics != nil {
			itr.operationMetrics.AddRead(itr.readCount)
		}
	})
	return nil
}

func (itr *iterator) assertIsValid() {
	if !itr.valid {
		panic("iterator is invalid")
	}
}

// cursorTombstoned checks if the current cursor is pointing at a key/value pair
// that is tombstoned. If the cursor is tombstoned, <true> is returned, otherwise
// <false> is returned. In the case where the iterator is valid but the key/value
// pair is tombstoned, the caller should call Next(). Note, this method assumes
// the caller assures the iterator is valid first!
func (itr *iterator) cursorTombstoned() bool {
	_, tombBz, ok := SplitMVCCKey(itr.source.Value())
	if !ok {
		// XXX: This should not happen as that would indicate we have a malformed
		// MVCC value.
		panic(fmt.Sprintf("invalid PebbleDB MVCC value: %s", itr.source.Key()))
	}

	// If the tombstone suffix is empty, we consider this a zero value and thus it
	// is not tombstoned.
	if len(tombBz) == 0 {
		return false
	}

	// If the tombstone suffix is non-empty and greater than the target version,
	// the value is not tombstoned.
	tombstone, err := decodeUint64Descending(tombBz)
	if err != nil {
		panic(fmt.Errorf("failed to decode value tombstone: %w", err))
	}
	if tombstone > itr.version {
		return false
	}

	return true
}

func (itr *iterator) DebugRawIterate() {
	valid := itr.source.Valid()
	if valid {
		// The first key may not represent the desired target version, so move the
		// cursor to the correct location.
		firstKey, _, _ := SplitMVCCKey(itr.source.Key())
		itr.positionAtOrAfterKey(firstKey)
		valid = itr.valid
	}

	for valid {
		key, vBz, ok := SplitMVCCKey(itr.source.Key())
		if !ok {
			panic(fmt.Sprintf("invalid PebbleDB MVCC key: %s", itr.source.Key()))
		}

		version, err := decodeUint64Descending(vBz)
		if err != nil {
			panic(fmt.Errorf("failed to decode key version: %w", err))
		}

		val, tombBz, ok := SplitMVCCKey(itr.source.Value())
		if !ok {
			panic(fmt.Sprintf("invalid PebbleDB MVCC value: %s", itr.source.Value()))
		}

		var tombstone int64
		if len(tombBz) > 0 {
			tombstone, err = decodeUint64Descending(vBz)
			if err != nil {
				panic(fmt.Errorf("failed to decode value tombstone: %w", err))
			}
		}

		fmt.Printf("KEY: %s, VALUE: %s, VERSION: %d, TOMBSTONE: %d\n", key, val, version, tombstone)

		if itr.reverse {
			prevKey, ok := itr.prevLogicalKey(key)
			if !ok {
				valid = false
				continue
			}
			itr.positionAtOrBeforeKey(prevKey)
			valid = itr.valid
			continue
		} else {
			nextKey, ok := itr.nextLogicalKey(key)
			if !ok {
				valid = false
				continue
			}
			itr.positionAtOrAfterKey(nextKey)
			valid = itr.valid
			continue
		}
	}
}
