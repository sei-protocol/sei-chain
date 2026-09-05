package walrus

// Snapshot is the handle for one retained state snapshot: a flat image of the entire state as of the end of
// one block.
//
// Like a pod's handles, it holds metadata rather than the data itself. When the underlying database is opened
// is an implementation matter the contract does not expose.
//
// One snapshot is not backed by anything on disk. A fresh instance starts with a pseudo-snapshot at block 0
// that reports every key absent, so a backwards walk always has somewhere to terminate and no caller has to
// handle the case of there being no floor at all. It is correct only because a fresh instance has pods
// covering every block above it, which is the invariant the catalog maintains.
//
// A Snapshot is safe for concurrent use.
type Snapshot interface {

	// BlockNumber returns the block whose state this snapshot holds.
	BlockNumber() uint64

	// Get returns the value key held at the end of the snapshot's block.
	//
	// A zero-length value that was actually written is returned as a non-nil empty slice, so that an empty
	// value is distinguishable from an absent key.
	Get(key []byte) (value []byte, found bool, err error)

	// Path returns the directory the snapshot lives in, or the empty string for the block 0 pseudo-snapshot.
	Path() string

	// Size returns the bytes the snapshot occupies.
	Size() int64

	// Delete removes the snapshot.
	Delete() error
}
