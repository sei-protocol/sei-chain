package cdkdatalake

// deleter deletes the objects of keys the control loop has deleted, and tracks those deletes until they finish. The
// control loop hands it a key only once deleting the key's object is safe. Not thread safe.
type deleter interface {
	// SetFirstKey records the first key put into an empty store. No key below firstKey needs deleting.
	SetFirstKey(firstKey uint64)

	// Prune schedules the deletion of every key below bound not already scheduled.
	Prune(
		// The lowest key to keep.
		bound uint64,
	)

	// Delete schedules the deletion of key, which must be in the store. Only a store with gaps supports it.
	Delete(key uint64) error

	// DeletionWatermark returns the deletion watermark to record in the store's metadata.
	DeletionWatermark() uint64

	// LowestRemainingKey returns the lowest key whose object may still exist, and false if the deleter knows of no
	// such key.
	LowestRemainingKey() (uint64, bool)

	// DoBackgroundWork starts up to maxDeletes of the scheduled deletes whose puts are complete, waiting while the
	// writer's queue is full.
	DoBackgroundWork(
		// The lowest key whose put may not be complete.
		durableWatermark uint64,
		// The maximum number of deletes to start.
		maxDeletes int,
	)

	// FinalizeDeletes records every submitted delete that has finished, in the order they were submitted, stopping
	// at the first one still running.
	FinalizeDeletes() error
}

// objectDeleter deletes the objects of a store's keys.
type objectDeleter interface {
	// Delete deletes the object of key. Deleting an absent object succeeds.
	Delete(key uint64) *writeResult
}
