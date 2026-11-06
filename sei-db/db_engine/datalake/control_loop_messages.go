package datalake

// putRequest asks the loop to accept a put.
type putRequest struct {
	// The key to write.
	key uint64

	// The value, for a put held in memory.
	value []byte

	// The value, for a stream put. The loop closes it if the put is rejected.
	stream DataLakeStream

	// Receives nil once the put is accepted, or the reason it was rejected.
	reply chan error
}

// deleteRequest asks the loop to delete a key.
type deleteRequest struct {
	// The key to delete.
	key uint64

	// Receives the outcome.
	reply chan error
}

// pruneRequest asks the loop to delete every key below a bound.
type pruneRequest struct {
	// The lowest key to keep.
	key uint64

	// Receives the outcome.
	reply chan error
}

// flushRequest asks the loop to make every accepted write durable.
type flushRequest struct {
	// Receives the outcome once the flush completes.
	reply chan error
}

// lookupRequest asks the loop whether a key is in the store.
type lookupRequest struct {
	// The key to look up.
	key uint64

	// Receives the answer.
	reply chan lookupResult
}

// lookupResult answers a lookupRequest.
type lookupResult struct {
	// True if the key has been accepted and not deleted.
	present bool

	// Set if the store has failed.
	err error
}

// boundsRequest asks the loop for the durable watermark and the first key.
type boundsRequest struct {
	// Receives the answer.
	reply chan storeBounds
}

// storeBounds answers a boundsRequest.
type storeBounds struct {
	// Every accepted write to a key strictly less than this watermark is durable.
	watermark uint64

	// The lowest key in the store.
	firstKey uint64

	// False if the store is empty.
	firstKeyOK bool
}

// iteratorRequest asks the loop for the store's key topology.
type iteratorRequest struct {
	// Receives the answer.
	reply chan keyTopologyResult
}

// keyTopologyResult answers an iteratorRequest.
type keyTopologyResult struct {
	// The store's key topology.
	topology keyTopology

	// Set if the store has failed.
	err error
}
