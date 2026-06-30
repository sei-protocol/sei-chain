package mvcc

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/exp/slices"

	dbm "github.com/tendermint/tm-db"

	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
)

// This file contains the ascending-version MVCC iterator used for legacy DBs
// that were written by the pre-descending build. It is a verbatim port of the
// iterator implementation from main and is intentionally kept isolated from
// the descending fast-path iterator to avoid subtle interactions between the
// two encoding schemes.
//
// Archive nodes that cannot migrate will continue to use this path.

var _ dbm.Iterator = (*ascendingIterator)(nil)

// ascendingIterator is the legacy iterator. Versions of a logical key sort
// oldest-first on disk, so finding the visible version for a target height
// requires a SeekLT(version+1) dance rather than a cheap First().
type ascendingIterator struct {
	source             *pebble.Iterator
	prefix, start, end []byte
	version            int64
	valid              bool
	reverse            bool
	iterationCount     int64
	readCount          int64
	storeKey           string
	operationMetrics   *pebbledbmetrics.OperationMetrics

	closeSync sync.Once
}

func newAscendingIterator(src *pebble.Iterator, prefix, mvccStart, mvccEnd []byte, version int64, earliestVersion int64, reverse bool, storeKey string, operationMetrics *pebbledbmetrics.OperationMetrics) *ascendingIterator {
	// Return invalid iterator if requested iterator height is lower than earliest version after pruning
	if version < earliestVersion {
		return &ascendingIterator{
			source:           src,
			prefix:           prefix,
			start:            mvccStart,
			end:              mvccEnd,
			version:          version,
			valid:            false,
			reverse:          reverse,
			storeKey:         storeKey,
			operationMetrics: operationMetrics,
		}
	}

	// move the underlying PebbleDB iterator to the first key
	var valid bool
	if reverse {
		valid = src.Last()
	} else {
		valid = src.First()
	}

	itr := &ascendingIterator{
		source:           src,
		prefix:           prefix,
		start:            mvccStart,
		end:              mvccEnd,
		version:          version,
		valid:            valid,
		reverse:          reverse,
		storeKey:         storeKey,
		operationMetrics: operationMetrics,
	}

	if valid {
		currKey, currKeyVersion, ok := SplitMVCCKey(itr.source.Key())
		if !ok {
			// XXX: This should not happen as that would indicate we have a malformed MVCC key.
			panic(fmt.Sprintf("invalid PebbleDB MVCC key: %s", itr.source.Key()))
		}

		curKeyVersionDecoded, err := decodeUint64Ascending(currKeyVersion)
		if err != nil {
			itr.valid = false
			return itr
		}

		// We need to check whether initial key iterator visits has a version <= requested version
		// If larger version, call next to find another key which does
		if curKeyVersionDecoded > itr.version {
			itr.Next()
		} else {
			// If version is less, seek to the largest version of that key <= requested iterator version
			// It is guaranteed this won't move the iterator to a key that is invalid since
			// curKeyVersionDecoded <= requested iterator version, so there exists at least one version of currKey SeekLT may move to
			itr.valid = itr.source.SeekLT(MVCCEncodeAscending(currKey, itr.version+1))
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

// Domain returns the domain of the iterator. The caller must not modify the
// return values.
func (itr *ascendingIterator) Domain() ([]byte, []byte) {
	return itr.start, itr.end
}

func (itr *ascendingIterator) Key() []byte {
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

func (itr *ascendingIterator) Value() []byte {
	itr.assertIsValid()

	val, _, ok := SplitMVCCKey(itr.source.Value())
	if !ok {
		// XXX: This should not happen as that would indicate we have a malformed
		// MVCC value.
		panic(fmt.Sprintf("invalid PebbleDB MVCC value: %s", itr.source.Key()))
	}

	return slices.Clone(val)
}

func (itr *ascendingIterator) nextForward() {
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

	next := itr.source.NextPrefix()

	// First move the iterator to the next prefix, which may not correspond to the
	// desired version for that key, e.g. if the key was written at a later version,
	// so we seek back to the latest desired version, s.t. the version is <= itr.version.
	if next {
		nextKey, _, ok := SplitMVCCKey(itr.source.Key())
		if !ok {
			// XXX: This should not happen as that would indicate we have a malformed
			// MVCC key.
			itr.valid = false
			return
		}
		if !bytes.HasPrefix(nextKey, itr.prefix) {
			// the next key must have itr.prefix as the prefix
			itr.valid = false
			return
		}

		// Move the iterator to the closest version to the desired version, so we
		// append the current iterator key to the prefix and seek to that key.
		itr.valid = itr.source.SeekLT(MVCCEncodeAscending(nextKey, itr.version+1))

		tmpKey, tmpKeyVersion, ok := SplitMVCCKey(itr.source.Key())
		if !ok {
			// XXX: This should not happen as that would indicate we have a malformed
			// MVCC key.
			itr.valid = false
			return
		}

		// There exists cases where the SeekLT() call moved us back to the same key
		// we started at, so we must move to next key, i.e. two keys forward.
		if bytes.Equal(tmpKey, currKey) {
			if itr.source.NextPrefix() {
				itr.nextForward()

				_, tmpKeyVersion, ok = SplitMVCCKey(itr.source.Key())
				if !ok {
					// XXX: This should not happen as that would indicate we have a malformed
					// MVCC key.
					itr.valid = false
					return
				}

			} else {
				itr.valid = false
				return
			}
		}

		// We need to verify that every Next call either moves the iterator to a key whose version
		// is less than or equal to requested iterator version, or exhausts the iterator
		tmpKeyVersionDecoded, err := decodeUint64Ascending(tmpKeyVersion)
		if err != nil {
			itr.valid = false
			return
		}

		// If iterator is at a entry whose version is higher than requested version, call nextForward again
		if tmpKeyVersionDecoded > itr.version {
			itr.nextForward()
		}

		// The cursor might now be pointing at a key/value pair that is tombstoned.
		// If so, we must move the cursor.
		if itr.valid && itr.cursorTombstoned() {
			itr.nextForward()
		}

		return
	}

	itr.valid = false
}

func (itr *ascendingIterator) nextReverse() {
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

	next := itr.source.SeekLT(MVCCEncodeAscending(currKey, 0))

	// First move the iterator to the next prefix, which may not correspond to the
	// desired version for that key, e.g. if the key was written at a later version,
	// so we seek back to the latest desired version, s.t. the version is <= itr.version.
	if next {
		nextKey, _, ok := SplitMVCCKey(itr.source.Key())
		if !ok {
			// XXX: This should not happen as that would indicate we have a malformed
			// MVCC key.
			itr.valid = false
			return
		}
		if !bytes.HasPrefix(nextKey, itr.prefix) {
			// the next key must have itr.prefix as the prefix
			itr.valid = false
			return
		}

		// Move the iterator to the closest version to the desired version, so we
		// append the current iterator key to the prefix and seek to that key.
		itr.valid = itr.source.SeekLT(MVCCEncodeAscending(nextKey, itr.version+1))

		_, tmpKeyVersion, ok := SplitMVCCKey(itr.source.Key())
		if !ok {
			// XXX: This should not happen as that would indicate we have a malformed
			// MVCC key.
			itr.valid = false
			return
		}

		// We need to verify that every Next call either moves the iterator to a key whose version
		// is less than or equal to requested iterator version, or exhausts the iterator
		tmpKeyVersionDecoded, err := decodeUint64Ascending(tmpKeyVersion)
		if err != nil {
			itr.valid = false
			return
		}

		// If iterator is at a entry whose version is higher than requested version, call nextReverse again
		if tmpKeyVersionDecoded > itr.version {
			itr.nextReverse()
		}

		// The cursor might now be pointing at a key/value pair that is tombstoned.
		// If so, we must move the cursor.
		if itr.valid && itr.cursorTombstoned() {
			itr.nextReverse()
		}

		return
	}

	itr.valid = false
}

func (itr *ascendingIterator) Next() {
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

func (itr *ascendingIterator) Valid() bool {
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

func (itr *ascendingIterator) Error() error {
	return itr.source.Error()
}

func (itr *ascendingIterator) Close() error {
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

func (itr *ascendingIterator) assertIsValid() {
	if !itr.valid {
		panic("iterator is invalid")
	}
}

// cursorTombstoned checks if the current cursor is pointing at a key/value pair
// that is tombstoned. If the cursor is tombstoned, <true> is returned, otherwise
// <false> is returned. In the case where the iterator is valid but the key/value
// pair is tombstoned, the caller should call Next(). Note, this method assumes
// the caller assures the iterator is valid first!
func (itr *ascendingIterator) cursorTombstoned() bool {
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
