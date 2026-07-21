package management

// A store that is persisted via snapshots (e.g. SC, SS).
type SnapshotStore interface {

	// Fetch a list of all block numbers that this store has snapshots for, in ascending sorted order.
	GetStoredBlocks() ([]uint64, error)

	// Instruct the store that it may drop snapshots for all blocks below a specified number.
	// Store may drop data asynchronously.
	PruneBelow(blockNumber uint64) error

	// Return the name of the snapshot store.
	Name() string
}

// A store that is made up of a stream of data (e.g. a WAL).
type StreamStore interface {

	// Fetch the range of blocks stored within this stream.
	GetStoredBlocks() (
		start uint64, // inclusive; meaningful only when hasData is true
		end uint64, // inclusive; meaningful only when hasData is true
		hasData bool, // true if the stream contains at least one block
		err error,
	)

	// Instruct the store that it may drop data for all blocks below a specified number.
	// Store may drop data asynchronously.
	PruneBelow(blockNumber uint64) error

	// Return the name of the stream store.
	Name() string
}
