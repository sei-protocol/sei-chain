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
)

// This file contains the ascending-version MVCC iterator used for legacy DBs
// that were written by the pre-descending build. It mirrors the traversal
// structure of the descending fast-path iterator but is kept isolated from it to
// avoid subtle interactions between the two encoding schemes.
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
	storeKey           string
<<<<<<< HEAD
=======
	operationMetrics   *pebbledbmetrics.OperationMetrics
	ctx                context.Context
	err                error
>>>>>>> 6debd90 (Add context cancellation to SS DB layer (#3940))

	closeSync sync.Once
}

<<<<<<< HEAD
func newAscendingIterator(src *pebble.Iterator, prefix, mvccStart, mvccEnd []byte, version int64, earliestVersion int64, reverse bool, storeKey string) *ascendingIterator {
	// Return invalid iterator if requested iterator height is lower than earliest version after pruning
	if version < earliestVersion {
		return &ascendingIterator{
			source:   src,
			prefix:   prefix,
			start:    mvccStart,
			end:      mvccEnd,
			version:  version,
			valid:    false,
			reverse:  reverse,
			storeKey: storeKey,
=======
func newAscendingIterator(ctx context.Context, src *pebble.Iterator, prefix, mvccStart, mvccEnd []byte, version int64, earliestVersion int64, reverse bool, storeKey string, operationMetrics *pebbledbmetrics.OperationMetrics) *ascendingIterator {
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
			ctx:              ctx,
>>>>>>> 6debd90 (Add context cancellation to SS DB layer (#3940))
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
<<<<<<< HEAD
		source:   src,
		prefix:   prefix,
		start:    mvccStart,
		end:      mvccEnd,
		version:  version,
		valid:    valid,
		reverse:  reverse,
		storeKey: storeKey,
=======
		source:           src,
		prefix:           prefix,
		start:            mvccStart,
		end:              mvccEnd,
		version:          version,
		valid:            valid,
		reverse:          reverse,
		storeKey:         storeKey,
		operationMetrics: operationMetrics,
		ctx:              ctx,
>>>>>>> 6debd90 (Add context cancellation to SS DB layer (#3940))
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

	return itr
}

// visibleVersionUpperBound returns the exclusive seek bound that isolates the
// versions of key which are visible at itr.version. Ascending encoding sorts a
// key's versions oldest-first, so this bound sits immediately past the newest
// visible one.
func (itr *ascendingIterator) visibleVersionUpperBound(key []byte) []byte {
	version := itr.version
	if version < math.MaxInt64 {
		version++
	}
	return MVCCEncodeAscending(key, version)
}

// seekVisibleVersionForKey positions the cursor on the newest version of
// targetKey that is visible at itr.version, reporting whether such a version
// exists. When it does not, the seek lands on an earlier logical key, so callers
// must not assume the cursor still refers to targetKey.
func (itr *ascendingIterator) seekVisibleVersionForKey(targetKey []byte) bool {
	if !itr.source.SeekLT(itr.visibleVersionUpperBound(targetKey)) {
		return false
	}
	foundKey, _, ok := SplitMVCCKey(itr.source.Key())
	if !ok {
		return false
	}
	return bytes.Equal(foundKey, targetKey)
}

// nextLogicalKey returns the first logical key ordered after every version of
// currKey. It seeks from an explicit bound rather than stepping the cursor, so
// it does not care where the cursor was left by a previous failed seek.
func (itr *ascendingIterator) nextLogicalKey(currKey []byte) ([]byte, bool) {
	seekKey := MVCCEncodeAscending(currKey, math.MaxInt64)
	for valid := itr.source.SeekGE(seekKey); valid; valid = itr.source.Next() {
		if err := abortIfCancelled(itr.ctx); err != nil {
			itr.err = err
			itr.valid = false
			return nil, false
		}
		nextKey, _, ok := SplitMVCCKey(itr.source.Key())
		if !ok || !bytes.HasPrefix(nextKey, itr.prefix) {
			return nil, false
		}
		// A key stored at exactly math.MaxInt64 lands on currKey itself; step over
		// any such residual versions.
		if !bytes.Equal(nextKey, currKey) {
			return nextKey, true
		}
	}
	return nil, false
}

// prevLogicalKey returns the logical key ordered immediately before every
// version of currKey.
func (itr *ascendingIterator) prevLogicalKey(currKey []byte) ([]byte, bool) {
	if !itr.source.SeekLT(MVCCEncodeAscending(currKey, 0)) {
		return nil, false
	}
	prevKey, _, ok := SplitMVCCKey(itr.source.Key())
	if !ok || !bytes.HasPrefix(prevKey, itr.prefix) {
		return nil, false
	}
	return prevKey, true
}

// positionAtOrAfterKey walks forward from startKey to the first logical key that
// is visible at itr.version and not tombstoned. The walk is iterative, so stack
// usage stays constant no matter how many keys must be skipped.
func (itr *ascendingIterator) positionAtOrAfterKey(startKey []byte) {
	currentKey := startKey
	for {
		if err := abortIfCancelled(itr.ctx); err != nil {
			itr.err = err
			itr.valid = false
			return
		}
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

// positionAtOrBeforeKey walks backward from startKey to the first logical key
// that is visible at itr.version and not tombstoned. A key whose newest version
// is above itr.version may still have an older visible version, so the whole key
// must not be skipped on that basis alone.
func (itr *ascendingIterator) positionAtOrBeforeKey(startKey []byte) {
	currentKey := startKey
	for {
		if err := abortIfCancelled(itr.ctx); err != nil {
			itr.err = err
			itr.valid = false
			return
		}
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

	nextKey, ok := itr.nextLogicalKey(currKey)
	if !ok {
		itr.valid = false
		return
	}
	itr.positionAtOrAfterKey(nextKey)
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

	prevKey, ok := itr.prevLogicalKey(currKey)
	if !ok {
		itr.valid = false
		return
	}
	itr.positionAtOrBeforeKey(prevKey)
}

func (itr *ascendingIterator) Next() {
	itr.iterationCount++

	if itr.reverse {
		itr.nextReverse()
	} else {
		itr.nextForward()
	}
<<<<<<< HEAD
=======
	if itr.err != nil {
		panic(itr.err)
	}
	if itr.Valid() {
		itr.readCount++
	}
>>>>>>> 6debd90 (Add context cancellation to SS DB layer (#3940))
}

func (itr *ascendingIterator) Valid() bool {
	if itr.err != nil {
		itr.valid = false
		return false
	}
	// once invalid, forever invalid
	if !itr.valid || itr.source == nil || !itr.source.Valid() {
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
	if itr.err != nil {
		return itr.err
	}
	if itr.source == nil {
		return nil
	}
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
