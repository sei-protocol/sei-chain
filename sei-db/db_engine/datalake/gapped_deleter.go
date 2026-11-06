package datalake

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/structures"
)

var _ deleter = (*gappedDeleter)(nil)

// gappedDeleter is the deleter of a store that allows gaps. Every deletion removes a single key, and a prune is one
// deletion per key below its bound. A key's object is deleted once the put of that key is complete.
type gappedDeleter struct {
	// The keys in the store, shared with the control loop. Prune() and Delete() remove the keys they schedule.
	keys *structures.SortedMap[uint64, struct{}]

	// The lowest key not scheduled for deletion.
	scheduledDeletionWatermark uint64

	// The deletion watermark of the metadata the store was recovered from.
	deletionWatermark uint64

	// Keys scheduled for deletion whose deletes have not started.
	waiting *structures.SortedMap[uint64, struct{}]

	// Deletes started and not yet collected, in the order they started.
	inFlight *structures.Deque[*writeResult]
}

// newGappedDeleter returns the deleter of a store with gaps.
func newGappedDeleter(
	// The keys in the store, shared with the control loop.
	keys *structures.SortedMap[uint64, struct{}],
	// The metadata the store was recovered from.
	metadata storeMetadata,
	// Keys below the metadata's scheduled deletion watermark whose objects still exist, in ascending order.
	danglingKeys []uint64,
) *gappedDeleter {
	d := &gappedDeleter{
		keys:                       keys,
		scheduledDeletionWatermark: metadata.ScheduledDeletionWatermark(),
		deletionWatermark:          metadata.DeletionWatermark(),
		waiting:                    structures.NewSortedMap[uint64, struct{}](),
		inFlight:                   structures.NewDeque[*writeResult](),
	}
	for _, key := range danglingKeys {
		d.waiting.Put(key, struct{}{})
	}
	return d
}

// Start records the first key put into an empty store. See deleter.
func (d *gappedDeleter) Start(firstKey uint64) {
	d.scheduledDeletionWatermark = firstKey
}

// Prune schedules the deletion of every key below bound, removing them from the store's keys. See deleter.
func (d *gappedDeleter) Prune(bound uint64) {
	for d.keys.Len() > 0 {
		key, _, _ := d.keys.PeekFront()
		if key >= bound {
			break
		}
		d.keys.PopFront()
		d.waiting.Put(key, struct{}{})
	}
	d.scheduledDeletionWatermark = bound
}

// Delete schedules the deletion of key, removing it from the store's keys. See deleter.
func (d *gappedDeleter) Delete(key uint64) {
	d.keys.Remove(key)
	d.waiting.Put(key, struct{}{})
}

// ScheduledDeletionWatermark returns the lowest key not scheduled for deletion. See deleter.
func (d *gappedDeleter) ScheduledDeletionWatermark() uint64 {
	return d.scheduledDeletionWatermark
}

// DeletionWatermark returns the deletion watermark of the metadata the store was recovered from. See deleter.
func (d *gappedDeleter) DeletionWatermark() uint64 {
	// Nothing reads the deletion watermark of a store with gaps: recovery lists the objects that still exist.
	return d.deletionWatermark
}

// Next returns the delete of the lowest waiting key, once the put of that key is complete. See deleter.
func (d *gappedDeleter) Next(durableWatermark uint64) (*deleteWrite, bool) {
	key, _, ok := d.waiting.PeekFront()
	if !ok || key >= durableWatermark {
		return nil, false
	}
	d.waiting.PopFront()
	write := &deleteWrite{key: key, result: newWriteResult(key)}
	d.inFlight.PushBack(write.result)
	return write, true
}

// Collect records every leading finished delete. See deleter.
func (d *gappedDeleter) Collect() error {
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

// MetadataDurable does nothing: no delete in a store with gaps waits for the metadata. See deleter.
func (d *gappedDeleter) MetadataDurable(storeMetadata) {}

// Idle reports whether no deletion is waiting to start or in flight. See deleter.
func (d *gappedDeleter) Idle() bool {
	return d.waiting.Len() == 0 && d.inFlight.IsEmpty()
}
