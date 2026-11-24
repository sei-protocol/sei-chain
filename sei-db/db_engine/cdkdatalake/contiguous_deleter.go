package cdkdatalake

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
)

var _ deleter = (*contiguousDeleter)(nil)

// contiguousDeleter is the deleter of a store that does not allow gaps, which deletes keys only by pruning.
type contiguousDeleter struct {
	// Performs the deletes.
	writer objectDeleter

	// The lowest key not scheduled for deletion.
	prunedBound uint64

	// Keys scheduled for deletion whose deletes have not started, in ascending, non-overlapping ranges.
	pending *structures.Deque[deletionRange]

	// Deletes submitted and not yet finalized, in ascending key order.
	inFlight *structures.Deque[*writeResult]
}

// newContiguousDeleter returns the deleter of a store without gaps whose durable metadata is metadata.
func newContiguousDeleter(
	writer objectDeleter,
	metadata storeMetadata,
) *contiguousDeleter {
	d := &contiguousDeleter{
		writer:      writer,
		prunedBound: metadata.ScheduledDeletionWatermark(),
		pending:     structures.NewDeque[deletionRange](),
		inFlight:    structures.NewDeque[*writeResult](),
	}
	d.schedule(metadata.DeletionWatermark(), metadata.ScheduledDeletionWatermark())
	return d
}

// SetFirstKey records the first key put into an empty store. See deleter.
func (d *contiguousDeleter) SetFirstKey(firstKey uint64) {
	// No key from the old bound up to firstKey was ever written.
	d.prunedBound = firstKey
}

// Prune schedules the deletion of every key below bound not already scheduled. See deleter.
func (d *contiguousDeleter) Prune(bound uint64) {
	if bound <= d.prunedBound {
		return
	}
	d.schedule(d.prunedBound, bound)
	d.prunedBound = bound
}

// Delete fails: a store without gaps deletes keys only by pruning. See deleter.
func (d *contiguousDeleter) Delete(key uint64) error {
	return fmt.Errorf("a store without gaps cannot delete key %d alone", key)
}

// DeletionWatermark returns the lowest key whose object may still exist. See deleter.
func (d *contiguousDeleter) DeletionWatermark() uint64 {
	watermark := d.prunedBound
	if first, ok := d.pending.TryPeekFront(); ok {
		watermark = min(watermark, first.Start)
	}
	if result, ok := d.inFlight.TryPeekFront(); ok {
		watermark = min(watermark, result.Key())
	}
	return watermark
}

// LowestRemainingKey returns the deletion watermark: no key below it has an object. See deleter.
func (d *contiguousDeleter) LowestRemainingKey() (uint64, bool) {
	return d.DeletionWatermark(), true
}

// DoBackgroundWork starts the deletes of the lowest pending keys whose puts are complete. See deleter.
func (d *contiguousDeleter) DoBackgroundWork(durableWatermark uint64, maxDeletes int) {
	for range maxDeletes {
		first, ok := d.pending.TryPeekFront()
		if !ok || first.Start >= durableWatermark {
			return
		}
		if first.Start+1 == first.End {
			d.pending.PopFront()
		} else {
			d.pending.Set(0, deletionRange{Start: first.Start + 1, End: first.End})
		}
		d.inFlight.PushBack(d.writer.Delete(first.Start))
	}
}

// FinalizeDeletes records every leading finished delete. See deleter.
func (d *contiguousDeleter) FinalizeDeletes() error {
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
