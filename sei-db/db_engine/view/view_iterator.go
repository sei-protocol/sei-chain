package view

import (
	"bytes"
	"errors"
	"fmt"

	dbm "github.com/tendermint/tm-db"
)

var _ dbm.Iterator = (*viewIterator)(nil)

// kvPair is a single in-memory override at a view version. A nil value
// signals a tombstone: it suppresses any DB-side value at the same key.
type kvPair struct {
	key   []byte
	value []byte
}

// viewIterator is a forward-only cursor that merges a pre-sorted slice
// of in-memory overrides with an underlying DB iterator. Keys are returned in
// ascending lexicographic order; when both sides hold the same key, the override
// wins (and nil-valued overrides suppress the DB entry entirely).
//
// It is positioned on its first pair at construction, so Valid may be consulted
// immediately; Next moves to the following pair and does nothing once the cursor
// has gone invalid.
//
// Ownership & lifetime:
//   - The iterator takes ownership of dbIter. dbIter is closed automatically
//     when iteration runs to exhaustion via Next, or explicitly by Close.
//     Either way, Close is idempotent and always safe (and recommended) to
//     call: callers that may not exhaust the iterator should still Close it to
//     avoid leaking dbIter resources.
//   - The iterator pins the overrides slice (and the byte slices it references)
//     for its full lifetime.
//   - This type knows nothing about views, reservations, or any other
//     enclosing cache state. Callers that need additional cleanup (e.g.
//     releasing a view reservation) should wrap this iterator.
//
// Returned slices:
//   - Slices returned by Key and Value remain valid until Close and must not be
//     mutated. Override-sourced bytes alias the supplied overrides slice;
//     DB-sourced bytes are cloned out of the underlying iterator's zero-copy
//     buffers so they remain stable as the dbIter advances.
//
// Filtering:
//   - Keys under the manager's reserved metadata prefix (reservedPrefix) are
//     excluded from iteration output: the DB-side values there belong to the
//     most recently flushed version and are generally stale relative to this
//     view. They are manager bookkeeping, not user data.
//
// Concurrency:
//   - Not thread-safe. A single iterator must not be shared across goroutines;
//     create one iterator per consumer.
type viewIterator struct {
	// overrides are sorted in iteration order by key (see reverse). The caller is
	// responsible for sorting; the iterator does not re-validate ordering.
	overrides   []kvPair
	overrideIdx int

	// dbIter is the underlying DB iterator. It is owned by this iterator and
	// arrives already positioned at its first key (tm-db iterators are created
	// pre-positioned).
	dbIter dbm.Iterator

	// reservedPrefix is the manager's reserved metadata key prefix (see
	// ViewManagerConfig.ReservedPrefix); entries under it are excluded from
	// iteration output.
	reservedPrefix []byte

	// reverse reports whether iteration walks keys in descending order. It must match the direction
	// the overrides were sorted in and the direction dbIter was opened with; the merge picks whichever
	// tip comes first in that order.
	reverse bool

	// start is the inclusive lower bound this iterator was opened with, reported verbatim by Domain.
	start []byte

	// end is the exclusive upper bound this iterator was opened with, reported verbatim by Domain.
	end []byte

	// key is the key the cursor currently sits on, nil once the merge has run out or errored.
	key []byte

	// value is the value the cursor currently sits on, nil once the merge has run out or errored.
	value []byte

	// nextDBPair caches the dbIter's current tip, cloned out of dbIter's
	// zero-copy buffers. Populated by the constructor and refreshed by
	// advanceDBIterator; nil only when the DB iterator is exhausted (or has
	// errored, in which case the error is sticky on it.err).
	nextDBPair *kvPair

	// err is sticky once set: every subsequent Next returns it without further work.
	err error

	// closed is set by Close to make further Next/Close calls inert. Distinct
	// from dbIterClosed: an iterator can have dbIter closed (via exhaustion
	// in Next) without the user having called Close.
	closed bool

	// dbIterClosed tracks whether dbIter.Close has been called, so the
	// preemptive close from Next and the user-driven close from Close don't
	// double-close the underlying iterator.
	dbIterClosed bool
}

// newViewIterator constructs a viewIterator over pre-materialized inputs.
//
// Caller obligations:
//   - overrides must be sorted in iteration order by key — ascending, or descending
//     when reverse is set. Override entries with value == nil are tombstones that
//     suppress same-key DB entries.
//   - dbIter must be a fresh DB iterator, already positioned at its first key
//     (tm-db iterators are created pre-positioned). The new iterator takes
//     ownership and will Close it.
//   - reservedPrefix is the manager's reserved metadata key prefix, whose keys are
//     excluded from iteration output (see the Filtering section of the type doc).
//   - reverse must match both the order overrides are sorted in and the direction
//     dbIter was opened with. Mismatching them yields a silently wrong merge.
//   - start and end are reported by Domain. They must match the bounds dbIter was
//     opened with and the range the overrides were filtered to; this iterator does
//     no bounds filtering of its own.
//
// The returned iterator is already positioned on its first pair. A failure while
// positioning it is returned here rather than surfaced through Error, and closes
// dbIter on the way out.
func newViewIterator(
	overrides []kvPair,
	dbIter dbm.Iterator,
	reservedPrefix []byte,
	reverse bool,
	start []byte,
	end []byte,
) (*viewIterator, error) {
	it := &viewIterator{
		overrides:      overrides,
		dbIter:         dbIter,
		reservedPrefix: reservedPrefix,
		reverse:        reverse,
		start:          start,
		end:            end,
	}
	if err := it.refreshDBPair(); err != nil {
		it.err = err
	} else {
		it.advance()
	}
	if it.err != nil {
		// We took ownership of dbIter; close it before returning the error
		// since the caller has no handle to clean it up.
		if closeErr := it.closeDBIter(); closeErr != nil {
			return nil, errors.Join(it.err, closeErr)
		}
		return nil, it.err
	}
	return it, nil
}

func (it *viewIterator) Domain() ([]byte, []byte) {
	return it.start, it.end
}

func (it *viewIterator) Valid() bool {
	return !it.closed && it.err == nil && it.key != nil
}

func (it *viewIterator) Next() {
	if !it.Valid() {
		return
	}
	it.advance()
}

func (it *viewIterator) Key() []byte {
	return it.key
}

func (it *viewIterator) Value() []byte {
	return it.value
}

func (it *viewIterator) Error() error {
	return it.err
}

// advance moves the cursor onto the next merged pair, or off the end. On the way off the end, and on
// any failure, key and value go nil so Valid reports false.
func (it *viewIterator) advance() {
	it.key, it.value = nil, nil

	for {
		var overrideTip *kvPair
		if it.overrideIdx < len(it.overrides) {
			overrideTip = &it.overrides[it.overrideIdx]
		}
		dbTip := it.nextDBPair

		if overrideTip == nil && dbTip == nil {
			// Both sides exhausted. Preemptively close dbIter so callers who
			// iterate to completion and don't close don't leak. Not closing
			// is irresponsible caller behavior, but we can provide a safety
			// net for some modes of failure.
			if err := it.closeDBIter(); err != nil {
				it.err = err
			}
			return
		}

		// Pick the smaller tip; on ties, the override wins and we advance both
		// sides. Tombstones (nil-valued overrides) suppress emission and loop.
		var pick *kvPair
		switch cmp := compareTips(overrideTip, dbTip, it.reverse); {
		case cmp < 0:
			// A new value is present in the overrides but not present in the database.
			pick = overrideTip
			it.advanceOverrideIndex()
		case cmp > 0:
			// A value is present in the database but not present in the overrides.
			pick = dbTip
			if err := it.advanceDBIterator(); err != nil {
				it.err = err
				return
			}
		default:
			// A value is present in both the overrides and the database.
			// Use the override value, skip the database value.
			pick = overrideTip
			it.advanceOverrideIndex()
			if err := it.advanceDBIterator(); err != nil {
				it.err = err
				return
			}
		}

		if pick.value == nil {
			// We've encountered a tombstone for a deleted value. Skip it and continue the loop.
			continue
		}
		if bytes.HasPrefix(pick.key, it.reservedPrefix) {
			// The manager's reserved metadata keyspace is not part of the view's user data.
			continue
		}
		it.key, it.value = pick.key, pick.value
		return
	}
}

// advanceDBIterator advances dbIter past its current position and refreshes
// nextDBPair. After this call, nextDBPair holds the new tip, or is nil if the
// iterator has been exhausted.
func (it *viewIterator) advanceDBIterator() error {
	it.dbIter.Next()
	return it.refreshDBPair()
}

// advanceOverrideIndex moves past the current override.
func (it *viewIterator) advanceOverrideIndex() {
	it.overrideIdx++
}

// refreshDBPair clones dbIter's current tip into nextDBPair. nextDBPair is set
// to nil if the iterator is exhausted or has errored.
func (it *viewIterator) refreshDBPair() error {
	if err := it.dbIter.Error(); err != nil {
		it.nextDBPair = nil
		return fmt.Errorf("db iterator error: %w", err)
	}
	if !it.dbIter.Valid() {
		it.nextDBPair = nil
		return nil
	}
	value := bytes.Clone(it.dbIter.Value())
	if value == nil {
		// A found value is never nil: normalize a backend that returns a nil slice for a stored
		// zero-length value to a non-nil empty slice. In a kvPair a nil value means a tombstone,
		// so without this the merge loop would silently drop the key.
		value = []byte{}
	}
	it.nextDBPair = &kvPair{
		key:   bytes.Clone(it.dbIter.Key()),
		value: value,
	}
	return nil
}

func (it *viewIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	return it.closeDBIter()
}

// closeDBIter closes dbIter exactly once across all callers (the constructor
// error path, the merge running off the end, and Close). Subsequent calls return nil.
func (it *viewIterator) closeDBIter() error {
	if it.dbIterClosed {
		return nil
	}
	it.dbIterClosed = true
	if err := it.dbIter.Close(); err != nil {
		return fmt.Errorf("failed to close db iterator: %w", err)
	}
	return nil
}

// Return -1 if overrideTip comes first in iteration order, 0 if the two tips share a key, 1 if dbTip
// comes first. "First" follows the iteration direction: the smaller key ascending, the larger key
// descending.
//
// A nil tip is exhausted and always sorts last, in both directions. That is why the exhaustion cases
// are decided before the key comparison and are not affected by reverse — inverting them would let an
// exhausted side win the merge and end iteration while the other side still has data.
func compareTips(overrideTip *kvPair, dbTip *kvPair, reverse bool) int {
	switch {
	case dbTip == nil:
		return -1
	case overrideTip == nil:
		return 1
	default:
		cmp := bytes.Compare(overrideTip.key, dbTip.key)
		if reverse {
			return -cmp
		}
		return cmp
	}
}
