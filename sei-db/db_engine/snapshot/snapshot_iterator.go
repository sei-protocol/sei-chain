package snapshot

import (
	"bytes"
	"errors"
	"fmt"

	dbm "github.com/tendermint/tm-db"
)

var _ Iterator = (*snapshotIterator)(nil)

// errIteratorClosed is returned by Next when invoked on an already-closed iterator.
var errIteratorClosed = errors.New("iterator is closed")

// kvPair is a single in-memory override at a snapshot version. A nil value
// signals a tombstone: it suppresses any DB-side value at the same key.
type kvPair struct {
	key   []byte
	value []byte
}

// snapshotIterator is a forward-only iterator that merges a pre-sorted slice
// of in-memory overrides with an underlying DB iterator. Keys are returned in
// ascending lexicographic order; when both sides hold the same key, the override
// wins (and nil-valued overrides suppress the DB entry entirely).
//
// Ownership & lifetime:
//   - The iterator takes ownership of dbIter. dbIter is closed automatically
//     when iteration runs to exhaustion via Next, or explicitly by Close.
//     Either way, Close is idempotent and always safe (and recommended) to
//     call: callers that may not exhaust the iterator should still Close it to
//     avoid leaking dbIter resources.
//   - The iterator pins the overrides slice (and the byte slices it references)
//     for its full lifetime.
//   - This type knows nothing about snapshots, reservations, or any other
//     enclosing cache state. Callers that need additional cleanup (e.g.
//     releasing a snapshot reservation) should wrap this iterator.
//
// Returned slices:
//   - Key and value slices returned by Next are owned by the caller and remain
//     valid until Close. Override-sourced bytes alias the supplied overrides
//     slice; DB-sourced bytes are cloned out of the underlying iterator's
//     zero-copy buffers so they remain stable as the dbIter advances.
//
// Filtering:
//   - The engine's reserved metadata hash key (hashKey) is excluded from
//     iteration output: the DB-side value under that key is the most recently
//     flushed hash, generally stale relative to this snapshot. Consumers obtain
//     the snapshot's hash via Snapshot.AwaitHash, which is guaranteed to match
//     the iterated data.
//
// Concurrency:
//   - Not thread-safe. A single iterator must not be shared across goroutines;
//     create one iterator per consumer.
type snapshotIterator struct {
	// overrides are sorted ascending by key. The caller is responsible for sorting;
	// the iterator does not re-validate ordering.
	overrides   []kvPair
	overrideIdx int

	// dbIter is the underlying DB iterator. It is owned by this iterator and
	// arrives already positioned at its first key (tm-db iterators are created
	// pre-positioned).
	dbIter dbm.Iterator

	// hashKey is the engine's reserved metadata hash key (see
	// SnapshotEngineConfig.HashKey); entries with this key are excluded from
	// iteration output.
	hashKey []byte

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

// newSnapshotIterator constructs a snapshotIterator over pre-materialized inputs.
//
// Caller obligations:
//   - overrides must be sorted ascending by key. Override entries with value == nil
//     are tombstones that suppress same-key DB entries.
//   - dbIter must be a fresh DB iterator, already positioned at its first key
//     (tm-db iterators are created pre-positioned). The new iterator takes
//     ownership and will Close it.
//   - hashKey is the engine's reserved metadata hash key, which is excluded
//     from iteration output (see the Filtering section of the type doc).
func newSnapshotIterator(
	overrides []kvPair,
	dbIter dbm.Iterator,
	hashKey []byte,
) (*snapshotIterator, error) {
	it := &snapshotIterator{
		overrides: overrides,
		dbIter:    dbIter,
		hashKey:   hashKey,
	}
	if err := it.refreshDBPair(); err != nil {
		// We took ownership of dbIter; close it before returning the error
		// since the caller has no handle to clean it up.
		if closeErr := it.closeDBIter(); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return it, nil
}

func (it *snapshotIterator) Next() (bool, []byte, []byte, error) {
	if it.closed {
		return false, nil, nil, errIteratorClosed
	}
	if it.err != nil {
		return false, nil, nil, it.err
	}

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
				return false, nil, nil, err
			}
			return false, nil, nil, nil
		}

		// Pick the smaller tip; on ties, the override wins and we advance both
		// sides. Tombstones (nil-valued overrides) suppress emission and loop.
		var pick *kvPair
		switch cmp := compareTips(overrideTip, dbTip); {
		case cmp < 0:
			// A new value is present in the overrides but not present in the database.
			pick = overrideTip
			it.advanceOverrideIndex()
		case cmp > 0:
			// A value is present in the database but not present in the overrides.
			pick = dbTip
			if err := it.advanceDBIterator(); err != nil {
				it.err = err
				return false, nil, nil, err
			}
		default:
			// A value is present in both the overrides and the database.
			// Use the override value, skip the database value.
			pick = overrideTip
			it.advanceOverrideIndex()
			if err := it.advanceDBIterator(); err != nil {
				it.err = err
				return false, nil, nil, err
			}
		}

		if pick.value == nil {
			// We've encountered a tombstone for a deleted value. Skip it and continue the loop.
			continue
		}
		if bytes.Equal(pick.key, it.hashKey) {
			// The engine's reserved metadata hash key is not part of the snapshot's user data.
			continue
		}
		return true, pick.key, pick.value, nil
	}
}

// advanceDBIterator advances dbIter past its current position and refreshes
// nextDBPair. After this call, nextDBPair holds the new tip, or is nil if the
// iterator has been exhausted.
func (it *snapshotIterator) advanceDBIterator() error {
	it.dbIter.Next()
	return it.refreshDBPair()
}

// advanceOverrideIndex moves past the current override.
func (it *snapshotIterator) advanceOverrideIndex() {
	it.overrideIdx++
}

// refreshDBPair clones dbIter's current tip into nextDBPair. nextDBPair is set
// to nil if the iterator is exhausted or has errored.
func (it *snapshotIterator) refreshDBPair() error {
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

func (it *snapshotIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	return it.closeDBIter()
}

// closeDBIter closes dbIter exactly once across all callers (the constructor
// error path, Next-on-exhaustion, and Close). Subsequent calls return nil.
func (it *snapshotIterator) closeDBIter() error {
	if it.dbIterClosed {
		return nil
	}
	it.dbIterClosed = true
	if err := it.dbIter.Close(); err != nil {
		return fmt.Errorf("failed to close db iterator: %w", err)
	}
	return nil
}

// Return -1 if overrideTip sorts before dbTip, 0 if they share a key, 1 if overrideTip sorts after dbTip.
// A nil tip is treated as exhausted and sorts after every real key.
func compareTips(overrideTip *kvPair, dbTip *kvPair) int {
	switch {
	case dbTip == nil:
		return -1
	case overrideTip == nil:
		return 1
	default:
		return bytes.Compare(overrideTip.key, dbTip.key)
	}
}
