package cdkdatalake

import (
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
)

var _ deleter = (*gappedDeleter)(nil)

// gappedDeleter is the deleter of a store that allows gaps. A prune is one deletion per key below its bound.
type gappedDeleter struct {
	// Performs the deletes.
	writer objectDeleter

	// The keys in the store, shared with the control loop. Prune() and Delete() remove the keys they schedule.
	keys *structures.SortedMap[uint64, struct{}]

	// The deletion watermark of the metadata the store was recovered from.
	deletionWatermark uint64

	// Keys scheduled for deletion whose deletes have not started.
	pending *structures.SortedMap[uint64, struct{}]

	// Deletes submitted and not yet finalized, in the order they were submitted.
	inFlight *structures.Deque[*writeResult]
}

// newGappedDeleter returns the deleter of a store with gaps.
func newGappedDeleter(
	writer objectDeleter,
	// The keys in the store, shared with the control loop.
	keys *structures.SortedMap[uint64, struct{}],
	// The metadata the store was recovered from.
	metadata storeMetadata,
	// Keys below the metadata's scheduled deletion watermark whose objects still exist, in ascending order.
	danglingKeys []uint64,
) *gappedDeleter {
	d := &gappedDeleter{
		writer:            writer,
		keys:              keys,
		deletionWatermark: metadata.DeletionWatermark(),
		pending:           structures.NewSortedMap[uint64, struct{}](),
		inFlight:          structures.NewDeque[*writeResult](),
	}
	for _, key := range danglingKeys {
		d.pending.Put(key, struct{}{})
	}
	return d
}

// SetFirstKey does nothing: a store with gaps deletes only the keys it holds. See deleter.
func (d *gappedDeleter) SetFirstKey(uint64) {}

// Prune schedules the deletion of every key below bound, removing them from the store's keys. See deleter.
func (d *gappedDeleter) Prune(bound uint64) {
	for d.keys.Len() > 0 {
		key, _, _ := d.keys.PeekFront()
		if key >= bound {
			break
		}
		d.keys.PopFront()
		d.pending.Put(key, struct{}{})
	}
}

// Delete schedules the deletion of key, removing it from the store's keys. See deleter.
func (d *gappedDeleter) Delete(key uint64) error {
	d.keys.Remove(key)
	d.pending.Put(key, struct{}{})
	return nil
}

// DeletionWatermark returns the deletion watermark of the metadata the store was recovered from. See deleter.
func (d *gappedDeleter) DeletionWatermark() uint64 {
	// Nothing reads the deletion watermark of a store with gaps: recovery lists the objects that still exist.
	return d.deletionWatermark
}

// LowestRemainingKey returns the lowest key that is in the store, scheduled for deletion, or being deleted. See
// deleter.
func (d *gappedDeleter) LowestRemainingKey() (uint64, bool) {
	lowest := uint64(math.MaxUint64)
	found := false
	if key, _, ok := d.keys.PeekFront(); ok {
		lowest = key
		found = true
	}
	if key, _, ok := d.pending.PeekFront(); ok {
		lowest = min(lowest, key)
		found = true
	}
	// A delete of a key below those already in flight may start later, so in-flight keys are not in key order.
	for _, result := range d.inFlight.Forward() {
		lowest = min(lowest, result.Key())
		found = true
	}
	return lowest, found
}

// DoBackgroundWork starts the deletes of the lowest pending keys whose puts are complete. See deleter.
func (d *gappedDeleter) DoBackgroundWork(durableWatermark uint64, maxDeletes int) {
	for range maxDeletes {
		key, _, ok := d.pending.PeekFront()
		if !ok || key >= durableWatermark {
			return
		}
		d.pending.PopFront()
		d.inFlight.PushBack(d.writer.Delete(key))
	}
}

// FinalizeDeletes records every leading finished delete. See deleter.
func (d *gappedDeleter) FinalizeDeletes() error {
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
