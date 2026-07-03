package disktable

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ litt.Iterator = (*reverseIterator)(nil)

// reverseIterator walks a snapshot of sealed segments in reverse insertion order (newest key first).
// Unlike the forward iterator it does not use the secondary-key value optimization: in reverse a
// secondary key is reached before its primary, so its value is always read directly. As a result
// reverse iteration may incur nontrivial random IO when reading values.
//
// The snapshot is captured when the iterator is created, and the iterator holds a reservation on each of its
// segments, so their files remain on disk until Close releases them — even if garbage collection collects those
// segments meanwhile. Close is therefore mandatory: a leaked iterator pins its segments' files indefinitely.
type reverseIterator struct {
	// table is the owning disk table, used to issue the close request.
	table *DiskTable

	// segs is the ordered (lowest-to-highest index) snapshot of sealed segments in scope.
	segs []*segment.Segment

	// segPos is the index into segs of the segment currently being walked.
	segPos int

	// keys holds the keys of the segment currently being walked (nil if not yet loaded).
	keys []*types.ScopedKey

	// keyPos is the index into keys of the next key to return.
	keyPos int

	// current is the key most recently returned by Next.
	current *types.ScopedKey

	// currentSeg is the segment that current was read from.
	currentSeg *segment.Segment

	// closed is true once Close has been called.
	closed bool
}

// newReverseIterator creates a reverse iterator over the given snapshot of sealed segments.
func newReverseIterator(table *DiskTable, segs []*segment.Segment) *reverseIterator {
	return &reverseIterator{
		table:  table,
		segs:   segs,
		segPos: len(segs) - 1,
	}
}

// Next advances the iterator to the next key in reverse insertion order.
func (it *reverseIterator) Next() (bool, error) {
	if it.closed {
		return false, fmt.Errorf("iterator is closed")
	}

	for {
		if it.segPos < 0 {
			return false, nil
		}

		if it.keys == nil {
			keys, err := it.segs[it.segPos].GetKeys()
			if err != nil {
				return false, fmt.Errorf("failed to read keys for segment %d: %w",
					it.segs[it.segPos].SegmentIndex(), err)
			}
			it.keys = keys
			it.keyPos = len(keys) - 1
		}

		if it.keyPos < 0 {
			// Done with this segment; advance to the previous one.
			it.keys = nil
			it.segPos--
			continue
		}

		it.current = it.keys[it.keyPos]
		it.keyPos--
		it.currentSeg = it.segs[it.segPos]

		return true, nil
	}
}

// GetKey returns the current key and whether it is a primary key.
func (it *reverseIterator) GetKey() (key []byte, isPrimary bool) {
	return it.current.Key, it.current.Kind.IsPrimary()
}

// GetValue reads and returns the value associated with the current key. Reverse iteration always reads
// directly from the value file (the forward secondary-key optimization does not apply because a
// secondary is reached before its primary).
func (it *reverseIterator) GetValue() (value []byte, err error) {
	value, err = it.currentSeg.Read(it.current.Key, it.current.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to read value: %w", err)
	}
	return value, nil
}

// Close releases the resources held by the iterator, including the reservations on its snapshot segments
// (allowing any segment GC collected while it was open to finally be deleted from disk).
func (it *reverseIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true

	// Release the reservation on each snapshot segment. This must happen even on the error paths below: a missed
	// release pins those segments' files on disk indefinitely.
	for _, seg := range it.segs {
		seg.Release()
	}
	it.segs = nil

	// Notify the control loop so the open-iterator metric is updated.
	request := &controlLoopCloseIteratorRequest{
		completionChan: make(chan struct{}, 1),
	}
	err := it.table.controlLoop.enqueue(request)
	if err != nil {
		return fmt.Errorf("failed to send close iterator request: %w", err)
	}
	_, err = util.Await(it.table.errorMonitor, request.completionChan)
	if err != nil {
		return fmt.Errorf("failed to await iterator close: %w", err)
	}
	return nil
}
