package datalake

// deleter decides when the objects of a store's deleted keys are deleted, and tracks those deletes until they finish.
// The control loop hands it every delete and prune it accepts, forwards the deletes it produces to the writer, and
// reports each metadata write that becomes crash durable. Not thread safe.
type deleter interface {
	// Start records the first key put into an empty store. No key below firstKey needs deleting.
	Start(firstKey uint64)

	// Prune schedules the deletion of every key below bound.
	Prune(
		// The lowest key to keep. Must be above the scheduled deletion watermark.
		bound uint64,
	)

	// Delete schedules the deletion of key, which must be in the store.
	Delete(key uint64)

	// ScheduledDeletionWatermark returns the lowest key not scheduled for deletion.
	ScheduledDeletionWatermark() uint64

	// DeletionWatermark returns the deletion watermark to record in the store's metadata.
	DeletionWatermark() uint64

	// Next returns the next delete that may start now and records it as in flight, or false if there is none.
	Next(
		// The lowest key whose put may not be complete.
		durableWatermark uint64,
	) (*deleteWrite, bool)

	// Collect records every in-flight delete that has finished, in the order they started, stopping at the first
	// one still running. It returns an error if a delete failed.
	Collect() error

	// MetadataDurable records that metadata is crash durable. Called after every metadata write completes.
	MetadataDurable(metadata storeMetadata)

	// Idle reports whether no deletion is waiting to start or in flight.
	Idle() bool
}
