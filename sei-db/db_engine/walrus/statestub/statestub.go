// Package statestub provides the live state store that produces the snapshots a Walrus instance retains.
//
// It stands in for FlatKV in the proof of concept. Nothing in walrus depends on which store produced a
// snapshot, only on the snapshot being a flat image of state at a block, so swapping this for the real live
// state DB later changes no walrus code.
package statestub

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// StateStub is a flat, latest-value-only state store that can checkpoint itself.
//
// It keeps one value per key rather than a version history: reconstructing history is what walrus is for.
//
// A StateStub is safe for concurrent Get calls. CommitBlock, Checkpoint, and Close must be serialized by the
// caller.
type StateStub interface {

	// CommitBlock applies a block's changes and makes them durable.
	//
	// Blocks must arrive in contiguous ascending order. Deletions in a changeset remove the key rather than
	// storing a tombstone, because a flat image has nowhere for a tombstone to be observed from.
	CommitBlock(blockNumber uint64, changeSets []*proto.NamedChangeSet) error

	// Get returns the value key currently holds.
	//
	// A zero-length value that was actually written is returned as a non-nil empty slice, so that an empty
	// value is distinguishable from an absent key.
	Get(key []byte) (value []byte, found bool, err error)

	// BlockNumber returns the last block committed.
	BlockNumber() (ok bool, blockNumber uint64)

	// Checkpoint writes an immutable image of state as of the last committed block into a fresh directory
	// under the store's staging path and returns that directory and the block it covers.
	//
	// The directory that comes back belongs to the caller for its whole life. Handing it to
	// Walrus.RetainSnapshot gives that instance its own hard-linked reference, after which the caller is free
	// to delete this directory: the snapshot data survives until every reference to it is gone. A directory
	// the caller neither retains elsewhere nor deletes is leaked.
	Checkpoint() (directory string, blockNumber uint64, err error)

	// Close releases the store's resources.
	Close() error
}
