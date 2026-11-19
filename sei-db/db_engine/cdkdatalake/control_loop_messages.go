package cdkdatalake

import "github.com/sei-protocol/sei-chain/sei-db/datalake"

// putRequest asks the loop to accept a put.
type putRequest struct {
	// The key to write.
	key uint64

	// The value, for a put held in memory.
	value []byte

	// The value, for a stream put. The loop closes it if the put is rejected.
	stream datalake.DataLakeStream

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
	// Receives a value once the flush completes.
	reply chan struct{}
}

// closeRequest asks the loop to make every accepted write durable and exit.
type closeRequest struct {
	// Receives the flush's failure, or nil.
	reply chan error
}

// lookupRequest asks the loop whether a key is in the store.
type lookupRequest struct {
	// The key to look up.
	key uint64

	// Receives true if the key has been accepted and not deleted.
	reply chan bool
}

// boundsRequest asks the loop for the durable watermark, the deletion watermark and the first key.
type boundsRequest struct {
	// Receives the answer.
	reply chan storeBounds
}

// storeBounds answers a boundsRequest.
type storeBounds struct {
	// Every accepted write to a key strictly less than this watermark is durable.
	watermark uint64

	// The lowest key whose object may still exist in the backing store.
	deletionWatermark uint64

	// The lowest key in the store.
	firstKey uint64

	// False if the store is empty.
	firstKeyOK bool
}

// iteratorRequest asks the loop for the store's key topology.
type iteratorRequest struct {
	// Receives the store's key topology.
	reply chan keyTopology
}
