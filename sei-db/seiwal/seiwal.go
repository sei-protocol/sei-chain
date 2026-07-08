package seiwal

// WAL is a generic, index-keyed, append-only write-ahead log over opaque byte payloads.
//
// Each record is tagged with a caller-provided monotonic index. The index is what makes garbage
// collection ("drop everything below N"), iteration ("start at N"), and rollback ("drop everything
// above N") expressible without the WAL ever interpreting a payload. The WAL never inspects the bytes
// it stores; callers own all serialization.
type WAL interface {

	// Append a record with the given index and payload.
	//
	// The index must be strictly greater than the index of the most recently appended record (indices
	// need not be contiguous, but they must strictly increase). data may be empty; it is copied into the
	// WAL's framing before this call returns, so the caller may reuse the buffer immediately.
	//
	// This method only schedules the append; it does not block until the record is durable. Durability is
	// achieved by a subsequent Flush.
	Append(index uint64, data []byte) error

	// Flush blocks until all previously scheduled appends are durable.
	Flush() error

	// Bounds reports the range of record indices currently stored in the WAL.
	Bounds() (
		// If true, there is at least one record in the WAL and first/last are valid. If false, the WAL is
		// empty and first/last are undefined.
		ok bool,
		// The lowest stored record index, inclusive. Only valid if ok is true.
		first uint64,
		// The highest stored record index, inclusive. Only valid if ok is true.
		last uint64,
		// Any error encountered while retrieving the range.
		err error,
	)

	// Prune removes all records with an index less than lowestIndexToKeep.
	//
	// This method merely schedules the prune; it does not block until the prune is complete. Pruning is
	// async and lazy, and implementations are free to delay it arbitrarily long. Pruning removes whole
	// sealed files only, so records may survive above the requested threshold until their containing file
	// is fully below it.
	Prune(lowestIndexToKeep uint64) error

	// Iterator returns an iterator over the WAL starting at the given index.
	//
	// The iterator reads a consistent, point-in-time snapshot of the WAL taken at some instant between the
	// start and the return of this call. Records appended before that instant are included; records
	// appended after it are not. For records appended concurrently with this call, whether they are
	// included is unspecified.
	Iterator(startIndex uint64) (Iterator, error)

	// Close flushes pending appends, seals the current file, and releases resources.
	Close() error
}

// Iterator iterates over the records of a WAL in ascending index order.
type Iterator interface {
	// Next advances the iterator to the next record. It returns false when iteration is complete (no more
	// records), and returns an error if advancing failed. After Next returns (false, nil), iteration is
	// complete; after it returns an error, the iterator must not be used further (other than Close).
	Next() (bool, error)

	// Entry returns the index and payload of the record at the iterator's current position. It is only
	// valid to call Entry after Next has returned (true, nil).
	//
	// The returned payload must be treated as read-only and must not be modified. Callers that need to
	// retain or mutate the data must copy it first.
	Entry() (index uint64, data []byte)

	// Close releases the resources held by the iterator.
	Close() error
}

// New opens (or creates) a WAL in the configured directory, recovering any files left behind by a
// previous session.
func New(config *Config) (WAL, error) {
	return newWAL(config, nil)
}

// NewWithRollback opens a WAL and deletes all records with an index greater than rollbackIndex before
// returning, so the WAL contains no record with an index greater than rollbackIndex.
func NewWithRollback(config *Config, rollbackIndex uint64) (WAL, error) {
	return newWAL(config, &rollbackIndex)
}
