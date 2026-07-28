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

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

var _ types.DBIterator = (*iterator)(nil)

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
	iterationCount     int64
	storeKey           string

	closeSync sync.Once
}

func newPebbleDBIterator(src *pebble.Iterator, prefix, mvccStart, mvccEnd []byte, version int64, earliestVersion int64, reverse bool, storeKey string) *iterator {
	// Return invalid iterator if requested iterator height is lower than earliest version after pruning
	if version < earliestVersion {
		return &iterator{
			source:   src,
			prefix:   prefix,
			start:    mvccStart,
			end:      mvccEnd,
			version:  version,
			valid:    false,
			reverse:  reverse,
			storeKey: storeKey,
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
		source:   src,
		prefix:   prefix,
		start:    mvccStart,
		end:      mvccEnd,
		version:  version,
		valid:    valid,
		reverse:  reverse,
		storeKey: storeKey,
	}

	if valid {
		currKey, _, ok := SplitMVCCKey(itr.source.Key())
		if !ok {
			// XXX: This should not happen as that would indicate we have a malformed MVCC key.
			panic(fmt.Sprintf("invalid PebbleDB MVCC key: %s", itr.source.Key()))
		}

		// Last()/First() land on an arbitrary version of currKey, so walk to the
		// first logical key at or beyond it that is visible at itr.version. Note
		// that currKey itself may still be visible even when the version we
		// landed on is newer than itr.version, so the whole key must not be
		// skipped on that basis alone.
		if reverse {
			itr.positionAtOrBeforeKey(currKey)
		} else {
			itr.positionAtOrAfterKey(currKey)
		}
	}

	return itr
}

// visibleVersionUpperBound returns the exclusive seek bound that isolates the
// versions of key which are visible at itr.version.
func (itr *iterator) visibleVersionUpperBound(key []byte) []byte {
	version := itr.version
	if version < math.MaxInt64 {
		version++
	}
	return MVCCEncode(key, version)
}

// seekVisibleVersionForKey positions the cursor on the newest version of
// targetKey that is visible at itr.version, reporting whether such a version
// exists. When it does not, the cursor is left on an earlier logical key and
// the caller must not treat the current position as a result.
func (itr *iterator) seekVisibleVersionForKey(targetKey []byte) bool {
	if !itr.source.SeekLT(itr.visibleVersionUpperBound(targetKey)) {
		return false
	}

	foundKey, foundVersion, ok := SplitMVCCKey(itr.source.Key())
	if !ok {
		return false
	}
	// Every version of targetKey sorts at or above the seek bound when none of
	// them are visible, which lands the cursor on a preceding logical key.
	if !bytes.Equal(foundKey, targetKey) {
		return false
	}

	foundVersionDecoded, err := decodeUint64Ascending(foundVersion)
	if err != nil {
		return false
	}
	return foundVersionDecoded <= itr.version
}

// nextLogicalKey returns the smallest logical key greater than currKey that is
// still within the iterator's prefix, independent of which version the cursor
// currently sits on.
func (itr *iterator) nextLogicalKey(currKey []byte) ([]byte, bool) {
	// MVCCEncode(currKey, 0) carries an empty version, which sorts below every
	// version of currKey, so this lands on currKey's oldest version if it has
	// any and on the following logical key otherwise.
	if !itr.source.SeekGE(MVCCEncode(currKey, 0)) {
		return nil, false
	}

	nextKey, _, ok := SplitMVCCKey(itr.source.Key())
	if !ok {
		return nil, false
	}
	if bytes.Equal(nextKey, currKey) {
		if !itr.source.NextPrefix() {
			return nil, false
		}
		nextKey, _, ok = SplitMVCCKey(itr.source.Key())
		if !ok {
			return nil, false
		}
	}
	if !bytes.HasPrefix(nextKey, itr.prefix) {
		return nil, false
	}
	return nextKey, true
}

// prevLogicalKey returns the largest logical key smaller than currKey that is
// still within the iterator's prefix, independent of which version the cursor
// currently sits on.
func (itr *iterator) prevLogicalKey(currKey []byte) ([]byte, bool) {
	if !itr.source.SeekLT(MVCCEncode(currKey, 0)) {
		return nil, false
	}

	prevKey, _, ok := SplitMVCCKey(itr.source.Key())
	if !ok || !bytes.HasPrefix(prevKey, itr.prefix) {
		return nil, false
	}
	return prevKey, true
}

// positionAtOrAfterKey advances to the first logical key at or after startKey
// that has a non-tombstoned version visible at itr.version.
//
// The walk is iterative on purpose: a historical iteration has to step over
// every logical key written after itr.version, and on an archive node that can
// be millions of keys. Recursing once per skipped key overflows the goroutine
// stack and takes the whole process down with it.
func (itr *iterator) positionAtOrAfterKey(startKey []byte) {
	currentKey := startKey
	for {
		if itr.seekVisibleVersionForKey(currentKey) && !itr.cursorTombstoned() {
			itr.valid = true
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

// positionAtOrBeforeKey is the reverse-iteration counterpart of
// positionAtOrAfterKey.
func (itr *iterator) positionAtOrBeforeKey(startKey []byte) {
	currentKey := startKey
	for {
		if itr.seekVisibleVersionForKey(currentKey) && !itr.cursorTombstoned() {
			itr.valid = true
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
	tombstone, err := decodeUint64Ascending(tombBz)
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
		valid = itr.source.SeekLT(MVCCEncode(firstKey, itr.version+1))
	}

	for valid {
		key, vBz, ok := SplitMVCCKey(itr.source.Key())
		if !ok {
			panic(fmt.Sprintf("invalid PebbleDB MVCC key: %s", itr.source.Key()))
		}

		version, err := decodeUint64Ascending(vBz)
		if err != nil {
			panic(fmt.Errorf("failed to decode key version: %w", err))
		}

		val, tombBz, ok := SplitMVCCKey(itr.source.Value())
		if !ok {
			panic(fmt.Sprintf("invalid PebbleDB MVCC value: %s", itr.source.Value()))
		}

		var tombstone int64
		if len(tombBz) > 0 {
			tombstone, err = decodeUint64Ascending(vBz)
			if err != nil {
				panic(fmt.Errorf("failed to decode value tombstone: %w", err))
			}
		}

		fmt.Printf("KEY: %s, VALUE: %s, VERSION: %d, TOMBSTONE: %d\n", key, val, version, tombstone)

		var next bool
		if itr.reverse {
			next = itr.source.SeekLT(MVCCEncode(key, 0))
		} else {
			next = itr.source.NextPrefix()
		}

		if next {
			nextKey, _, ok := SplitMVCCKey(itr.source.Key())
			if !ok {
				panic(fmt.Sprintf("invalid PebbleDB MVCC key: %s", itr.source.Key()))
			}

			// the next key must have itr.prefix as the prefix
			if !bytes.HasPrefix(nextKey, itr.prefix) {
				valid = false
			} else {
				valid = itr.source.SeekLT(MVCCEncode(nextKey, itr.version+1))
			}
		} else {
			valid = false
		}
	}
}
