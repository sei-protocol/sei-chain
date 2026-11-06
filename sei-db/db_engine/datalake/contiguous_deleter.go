package datalake

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
)

var _ deleter = (*contiguousDeleter)(nil)

// contiguousDeleter is the deleter of a store that does not allow gaps. Every deletion is a prune. A key's object is
// deleted only once metadata recording its scheduled deletion is crash durable, so that a crash cannot leave a gap.
type contiguousDeleter struct {
	// The lowest key not scheduled for deletion.
	scheduledDeletionWatermark uint64

	// The lowest key whose scheduled deletion no durable metadata records. Only lower keys may be deleted.
	released uint64

	// Keys scheduled for deletion whose deletes have not started, in ascending, non-overlapping ranges.
	pending *structures.Deque[deletionRange]

	// Deletes started and not yet collected, in ascending key order.
	inFlight *structures.Deque[*writeResult]
}

// newContiguousDeleter returns the deleter of a store without gaps whose durable metadata is metadata.
func newContiguousDeleter(metadata storeMetadata) *contiguousDeleter {
	d := &contiguousDeleter{
		scheduledDeletionWatermark: metadata.ScheduledDeletionWatermark(),
		released:                   metadata.ScheduledDeletionWatermark(),
		pending:                    structures.NewDeque[deletionRange](),
		inFlight:                   structures.NewDeque[*writeResult](),
	}
	d.schedule(metadata.DeletionWatermark(), metadata.ScheduledDeletionWatermark())
	return d
}

// Start records the first key put into an empty store. See deleter.
func (d *contiguousDeleter) Start(firstKey uint64) {
	if d.released == d.scheduledDeletionWatermark {
		// No key from the old watermark up to firstKey was ever written, so none needs a durable record before
		// the deletion watermark passes it. Keys still waiting for durable metadata keep waiting.
		d.released = firstKey
	}
	d.scheduledDeletionWatermark = firstKey
}

// Prune schedules the deletion of every key below bound. See deleter.
func (d *contiguousDeleter) Prune(bound uint64) {
	d.schedule(d.scheduledDeletionWatermark, bound)
	d.scheduledDeletionWatermark = bound
}

// Delete schedules the deletion of key, which must be the lowest key in the store. See deleter.
func (d *contiguousDeleter) Delete(key uint64) {
	d.Prune(key + 1)
}

// ScheduledDeletionWatermark returns the lowest key not scheduled for deletion. See deleter.
func (d *contiguousDeleter) ScheduledDeletionWatermark() uint64 {
	return d.scheduledDeletionWatermark
}

// DeletionWatermark returns the lowest key whose object may still exist. See deleter.
func (d *contiguousDeleter) DeletionWatermark() uint64 {
	watermark := d.released
	if first, ok := d.pending.TryPeekFront(); ok {
		watermark = min(watermark, first.Start)
	}
	if result, ok := d.inFlight.TryPeekFront(); ok {
		watermark = min(watermark, result.Key())
	}
	return watermark
}

// Next returns the delete of the lowest pending key, once durable metadata records its scheduled deletion and its put
// is complete. See deleter.
func (d *contiguousDeleter) Next(durableWatermark uint64) (*deleteWrite, bool) {
	first, ok := d.pending.TryPeekFront()
	if !ok || first.Start >= d.released || first.Start >= durableWatermark {
		return nil, false
	}
	key := first.Start
	if first.Start+1 == first.End {
		d.pending.PopFront()
	} else {
		d.pending.Set(0, deletionRange{Start: first.Start + 1, End: first.End})
	}
	write := &deleteWrite{key: key, result: newWriteResult(key)}
	d.inFlight.PushBack(write.result)
	return write, true
}

// Collect records every leading finished delete. See deleter.
func (d *contiguousDeleter) Collect() error {
	for !d.inFlight.IsEmpty() {
		result := d.inFlight.PeekFront()
		err, done := result.Check()
		if !done {
			return nil
		}
		if err != nil {
			return fmt.Errorf("delete key %d: %w", result.Key(), err)
		}
		d.inFlight.PopFront()
	}
	return nil
}

// MetadataDurable releases the keys whose scheduled deletion metadata records. See deleter.
func (d *contiguousDeleter) MetadataDurable(metadata storeMetadata) {
	d.released = max(d.released, metadata.ScheduledDeletionWatermark())
}

// Idle reports whether no deletion is waiting to start or in flight. See deleter.
func (d *contiguousDeleter) Idle() bool {
	return d.pending.IsEmpty() && d.inFlight.IsEmpty()
}

// schedule queues the keys from start up to, but excluding, end. Ranges must be scheduled in ascending order.
func (d *contiguousDeleter) schedule(start uint64, end uint64) {
	if start >= end {
		return
	}
	if last, ok := d.pending.TryPeekBack(); ok && last.End == start {
		d.pending.Set(-1, deletionRange{Start: last.Start, End: end})
		return
	}
	d.pending.PushBack(deletionRange{Start: start, End: end})
}

// deletionRange is the keys from Start up to, but excluding, End.
type deletionRange struct {
	// The lowest key in the range.
	Start uint64

	// One above the highest key in the range.
	End uint64
}
