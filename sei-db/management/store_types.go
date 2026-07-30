package management

import "fmt"

// StoreType describes how a store retains its data, which determines how the StorageGarbageCollector
// interprets what the store currently holds.
type StoreType int

const (
	// SnapshotStore is a store that is persisted via snapshots (e.g. SC, SS). It retains a discrete set of
	// snapshotted heights, reported by GetStoredSnapshots.
	SnapshotStore StoreType = iota + 1

	// StreamStore is a store that is made up of a stream of data (e.g. a WAL). It retains a contiguous range
	// of blocks, reported by GetBlockRange.
	StreamStore
)

func (t StoreType) String() string {
	switch t {
	case SnapshotStore:
		return "SnapshotStore"
	case StreamStore:
		return "StreamStore"
	default:
		return fmt.Sprintf("UnknownStoreType(%d)", int(t))
	}
}

// PrunableStore is a store whose old data may be dropped by the StorageGarbageCollector.
//
// Both GetStoredSnapshots and GetBlockRange must be implemented, but only the accessor matching the store's
// StoreType is required to carry information: a StreamStore has no discrete snapshots to report, and a
// SnapshotStore reports the range spanned by the snapshots it holds.
type PrunableStore interface {
	// Name Return the name of the store.
	Name() string

	// GetStoreType returns the type of the store.
	GetStoreType() StoreType

	// PruneBelow tells the store that it may drop snapshots/data for all blocks below a specified number.
	// Store can drop data asynchronously in the background.
	PruneBelow(blockNumber uint64) error

	// GetStoredSnapshots return a list of all block heights that this store has snapshots for, in ascending
	// sorted order. A StreamStore has no snapshots and returns nil.
	GetStoredSnapshots() ([]uint64, error)

	// GetBlockRange fetch the range of blocks stored in this store.
	GetBlockRange() (
		start uint64, // inclusive; meaningful only when hasData is true
		end uint64, // inclusive; meaningful only when hasData is true
		hasData bool, // true if the store contains at least one block
		err error,
	)

	// GetLastCommittedBlock returns the highest block this store has ingested. Meaningful only when the store has
	// data.
	//
	// This is the store's committed height, not the newest data it has durably retained. For a SnapshotStore the
	// two differ: snapshots are written periodically while the store keeps ingesting blocks, so the committed
	// height normally sits above the newest snapshot. Reporting the newest snapshot height instead is safe but
	// makes the garbage collector systematically under-prune.
	GetLastCommittedBlock() (uint64, error)
}
