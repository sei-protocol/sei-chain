package seiwal

// WAL is a generic, index-keyed, append-only write-ahead log over payloads of type T.
//
// Each record is tagged with a caller-provided monotonic index. The index is what makes garbage
// collection ("drop everything below N"), iteration ("start at N"), and rollback ("drop everything
// above N") expressible without the WAL ever interpreting a payload.
//
// A WAL instance is not safe for concurrent use: its methods must not be called from multiple
// goroutines simultaneously. Callers that share a WAL across goroutines must serialize access
// themselves.
//
// Slices are not copied at the call boundary. Any slice passed into a WAL method — the payload and every
// slice reachable through it — must not be modified after the call: the WAL may retain it and read it
// asynchronously, so mutating it races the WAL and can corrupt what is persisted. Likewise every slice
// returned from a WAL or its iterator is owned by the WAL and must be treated as read-only. Callers that
// need to mutate such data must copy it first.
type WAL[T any] interface {

	// Append a record with the given index and payload.
	//
	// The required relationship between successive indices depends on Config.PermitGaps. When PermitGaps is
	// false (the default), each index must be exactly one greater than the previous (strictly contiguous).
	// When PermitGaps is true, each index need only be strictly greater than the previous, so gaps are
	// allowed. In either case the first append on a fresh WAL may use any index, which sets the baseline.
	//
	// This method only schedules the append; it does not block until the record is durable. Durability is
	// achieved by a subsequent Flush.
	//
	// data, and every slice reachable through it, must not be modified after this call: the payload may be
	// retained and serialized asynchronously, so mutating it races the WAL and can corrupt what is
	// persisted. Callers that need to reuse or mutate the buffer must copy it first.
	Append(index uint64, data T) error

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

	// PruneBefore removes all records with an index less than lowestIndexToKeep.
	//
	// This method merely schedules the prune; it does not block until the prune is complete. Pruning is
	// async and lazy, and implementations are free to delay it arbitrarily long. Pruning removes whole
	// sealed files only, so records may survive above the requested threshold until their containing file
	// is fully below it.
	PruneBefore(lowestIndexToKeep uint64) error

	// Iterator returns an iterator over the WAL starting at the given index.
	//
	// The iterator reads a consistent, point-in-time snapshot of the WAL taken at some instant between the
	// start and the return of this call. Records appended before that instant are included; records
	// appended after it are not. For records appended concurrently with this call, whether they are
	// included is unspecified.
	Iterator(startIndex uint64) (Iterator[T], error)

	// Close flushes pending appends, seals the current file, and releases resources.
	Close() error
}

// Iterator iterates over the records of a WAL in ascending index order.
//
// An Iterator is single-consumer and not safe for concurrent use: all of its methods, including Close, must
// be called from a single goroutine (or with external serialization). In particular, Close must not be
// called concurrently with Next from another goroutine.
//
// Every payload returned by an Iterator — and every slice reachable through it — is owned by the WAL and
// must be treated as read-only. Callers that need to retain or mutate the data must copy it first.
type Iterator[T any] interface {
	// Next advances the iterator to the next record. It returns false when iteration is complete (no more
	// records), and returns an error if advancing failed. After Next returns (false, nil), iteration is
	// complete; after it returns an error, the iterator must not be used further (other than Close).
	Next() (bool, error)

	// Entry returns the index and payload of the record at the iterator's current position. It is only
	// valid to call Entry after Next has returned (true, nil).
	//
	// The returned payload must be treated as read-only and must not be modified. Callers that need to
	// retain or mutate the data must copy it first.
	Entry() (index uint64, data T)

	// Close releases the resources held by the iterator.
	Close() error
}
