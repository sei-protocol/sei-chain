package gc

// PrunableStore is a store whose old data may be dropped by the StorageGarbageCollector.
type PrunableStore interface {
	// Name Returns the name of the store.
	Name() string

	// PruneBelow tells the store that it may drop snapshots/data for all blocks below a specified number.
	// Store can drop data asynchronously in the background.
	PruneBelow(blockNumber uint64) error

	// GetOldestBlockToRetain returns the oldest block this store must keep in order to remain able to serve cutLine,
	// or 0 when the store holds nothing. A chain's first block is block 1, so 0 is free to serve as that sentinel: no
	// store can legitimately need to retain from block 0.
	//
	// The collector skips the whole prune cycle when any store answers 0. An empty answer is treated as unknown rather
	// than empty — a snapshot may be mid-write and take hours to land — and pruning the other stores against it would
	// risk deleting contiguous blocks that snapshot will need once it appears.
	//
	// A store that retains a contiguous range of blocks (blockDB, receiptDB, the state WAL) can be restored to any
	// block it holds, so it needs nothing below the cut line and returns cutLine unchanged.
	// A store with checkpoints can only be restored to a height it has a snapshot for,
	// so it returns the newest snapshot at or below cutLine, or its oldest snapshot when every snapshot is above the cut line,
	// since in that case none of them can be dropped.
	//
	// The collector prunes every store below the lowest answer it receives, so an answer that is too low only costs temporary
	// disk, while one that is too high deletes blocks the system still needs.
	GetOldestBlockToRetain(cutLine uint64) uint64

	// GetLastCommittedBlock returns the highest block this store has ingested.
	GetLastCommittedBlock() (uint64, error)
}
