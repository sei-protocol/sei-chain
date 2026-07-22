package disktable

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ litt.Iterator = (*forwardIterator)(nil)

// forwardIterator walks a snapshot of sealed segments in insertion order (oldest key first), reading
// each segment's value files roughly sequentially. It is the optimized iteration path: when a full
// group (a primary key followed by its secondary keys) is scanned, each secondary key's value is served
// as a sub-slice of the primary's value with no additional IO.
//
// The snapshot is captured when the iterator is created, and the iterator holds a reservation on each of its
// segments, so their files remain on disk until Close releases them — even if garbage collection collects those
// segments meanwhile. Close is therefore mandatory: a leaked iterator pins its segments' files indefinitely.
type forwardIterator struct {
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

	// reader is a buffered reader over currentSeg's value files, used to read values roughly
	// sequentially. It is recreated whenever the iterator advances to a new segment (see segmentReader).
	reader *segment.SegmentReader

	// readerSeg is the segment that reader was created for, so we can detect when to recreate it.
	readerSeg *segment.Segment

	// closed is true once Close has been called.
	closed bool

	// groupValid is true once a primary key in the current group has been visited, meaning groupAddr and
	// groupValue describe that group's primary value.
	groupValid bool

	// groupAddr is the address of the current group's primary value.
	groupAddr types.Address

	// groupValue is the current group's primary value, loaded lazily. It lets secondary keys be served as
	// sub-slices of the primary value with no additional IO.
	groupValue []byte
}

// newForwardIterator creates a forward iterator over the given snapshot of sealed segments.
func newForwardIterator(table *DiskTable, segs []*segment.Segment) *forwardIterator {
	return &forwardIterator{
		table:  table,
		segs:   segs,
		segPos: 0,
	}
}

// Next advances the iterator to the next key in insertion order.
func (it *forwardIterator) Next() (bool, error) {
	if it.closed {
		return false, fmt.Errorf("iterator is closed")
	}

	for {
		if it.segPos >= len(it.segs) {
			return false, nil
		}

		if it.keys == nil {
			keys, err := it.segs[it.segPos].GetKeys()
			if err != nil {
				return false, fmt.Errorf("failed to read keys for segment %d: %w",
					it.segs[it.segPos].SegmentIndex(), err)
			}
			it.keys = keys
			it.keyPos = 0
		}

		if it.keyPos >= len(it.keys) {
			// Done with this segment; advance to the next one.
			it.keys = nil
			it.segPos++
			continue
		}

		it.current = it.keys[it.keyPos]
		it.keyPos++
		it.currentSeg = it.segs[it.segPos]

		// A primary key begins a new group. Record its address so that the secondary keys that follow it
		// can be served from the primary's value without extra IO.
		if it.current.Kind.IsPrimary() {
			it.groupValid = true
			it.groupAddr = it.current.Address
			it.groupValue = nil
		}

		return true, nil
	}
}

// GetKey returns the current key and whether it is a primary key.
func (it *forwardIterator) GetKey() (key []byte, isPrimary bool) {
	return it.current.Key, it.current.Kind.IsPrimary()
}

// GetValue reads and returns the value associated with the current key.
func (it *forwardIterator) GetValue() (value []byte, err error) {
	reader, err := it.segmentReader()
	if err != nil {
		return nil, err
	}
	addr := it.current.Address

	if it.current.Kind.IsPrimary() {
		if it.groupValue == nil {
			v, err := reader.Read(addr)
			if err != nil {
				return nil, fmt.Errorf("failed to read value: %w", err)
			}
			it.groupValue = v
		}
		return it.groupValue, nil
	}

	// The current key is a secondary key. Its primary was visited immediately before it, so we can serve
	// the value as a sub-slice of the (lazily loaded) primary value. A secondary always references a
	// sub-range of its primary's value on the same shard.
	//
	// This optimization is invalid for compressed segments: there addr.ValueSize() is the compressed blob
	// length while groupValue holds the decompressed value, so the sub-slice arithmetic would be wrong.
	// Fall through to a direct read (which decompresses) instead. That re-reads and re-decodes the blob
	// rather than reusing the primary's decoded value, but this path is intentionally not optimized: the
	// only legal secondary on a compressed segment is a full-value alias, so reading its value returns the
	// same bytes as the primary — a redundant workload not worth extra machinery.
	if !it.currentSeg.IsCompressed() && it.groupValid && it.secondaryWithinGroup(addr) {
		if it.groupValue == nil {
			v, err := reader.Read(it.groupAddr)
			if err != nil {
				return nil, fmt.Errorf("failed to read primary value for secondary key: %w", err)
			}
			it.groupValue = v
		}
		relativeOffset := addr.Offset() - it.groupAddr.Offset()
		return it.groupValue[relativeOffset : relativeOffset+addr.ValueSize()], nil
	}

	// Fallback: read the secondary's sub-range directly.
	value, err = reader.Read(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to read value: %w", err)
	}
	return value, nil
}

// segmentReader returns a buffered reader for the segment the iterator is currently positioned on,
// (re)creating it when the iterator has advanced to a new segment. Only one segment reader is open at a
// time; advancing closes the previous one.
func (it *forwardIterator) segmentReader() (*segment.SegmentReader, error) {
	if it.reader != nil && it.readerSeg == it.currentSeg {
		return it.reader, nil
	}
	if it.reader != nil {
		if err := it.reader.Close(); err != nil {
			return nil, fmt.Errorf("failed to close previous segment reader: %w", err)
		}
	}
	it.reader = it.currentSeg.NewReader()
	it.readerSeg = it.currentSeg
	return it.reader, nil
}

// secondaryWithinGroup returns true if the given secondary-key address references a sub-range of the
// current group's primary value (same shard, contained byte range).
func (it *forwardIterator) secondaryWithinGroup(addr types.Address) bool {
	if addr.ShardID() != it.groupAddr.ShardID() {
		return false
	}
	start := it.groupAddr.Offset()
	end := uint64(start) + uint64(it.groupAddr.ValueSize())
	return uint64(addr.Offset()) >= uint64(start) &&
		uint64(addr.Offset())+uint64(addr.ValueSize()) <= end
}

// Close releases the resources held by the iterator, including the reservations on its snapshot segments
// (allowing any segment GC collected while it was open to finally be deleted from disk).
func (it *forwardIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true

	// Close the buffered reader.
	var readerErr error
	if it.reader != nil {
		readerErr = it.reader.Close()
		it.reader = nil
	}

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
	if readerErr != nil {
		return fmt.Errorf("failed to close segment reader: %w", readerErr)
	}
	return nil
}
