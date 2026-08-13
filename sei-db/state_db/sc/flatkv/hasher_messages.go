package flatkv

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
)

// This file contains the messages that can be sent to the block hasher's goroutine.

// hasherMessage is an interface for messages sent to the hasher via blockHasher.enqueue.
type hasherMessage interface {
	// If this is an empty interface, then the golang type system will not complain if non-implementing
	// types are passed to the hasher.
	unimplemented()
}

// hashRequest is one committed block for the hasher to hash.
type hashRequest struct {
	hasherMessage

	// version is the block height being hashed.
	version int64

	// current is the block's sealed snapshot for each database, keyed by database directory name. The
	// hasher reads this block's diff from these and finalizes them.
	current map[string]snapshot.Snapshot

	// previous is the preceding block's snapshot for each database. The lattice hash is a delta, so every
	// changed key's prior value is read here — and holding these reservations is what keeps Pebble at the
	// preceding version while that read happens. Releasing them early yields a wrong hash, silently.
	previous map[string]snapshot.Snapshot

	// alreadyHave is the replay skip list: the height each database had already reached when replay
	// started, or nil outside replay. It travels with the request because finalization consults it per
	// database, and by the time this is hashed the store has moved on.
	alreadyHave map[string]int64
}

// release hands back every reservation this request holds, for both blocks, so the databases can resume
// writing out later blocks.
//
// Every reservation is handed back even if one of them fails, because a reservation left held stalls its
// database's flushes indefinitely. The failures are joined and returned.
func (r *hashRequest) release() error {
	var errs []error
	for label, snapshots := range map[string]map[string]snapshot.Snapshot{
		"current": r.current, "previous": r.previous,
	} {
		for name, snap := range snapshots {
			if err := snap.Release(); err != nil {
				errs = append(errs, fmt.Errorf("release %s %s snapshot at version %d: %w",
					label, name, r.version, err))
			}
		}
	}
	return errors.Join(errs...)
}

// hashFlushRequest asks the hasher to report once it has dealt with everything enqueued ahead of it.
type hashFlushRequest struct {
	hasherMessage

	// responseChan produces a value once every message enqueued ahead of this one has been dealt with.
	// Buffered, so the hasher answering it cannot block on a caller that has already given up.
	responseChan chan struct{}
}

// newHashFlushRequest describes a wait for the hasher to catch up.
func newHashFlushRequest() *hashFlushRequest {
	return &hashFlushRequest{responseChan: make(chan struct{}, 1)}
}

// hasherSeedRequest asks the hasher for its accumulated state. Answered by the goroutine that owns that
// state, so the read cannot race it, and queued behind any block already offered so the answer describes
// every block accepted so far.
type hasherSeedRequest struct {
	hasherMessage

	// responseChan produces the accumulated state. Buffered, so the hasher answering it cannot block on a
	// caller that has already given up.
	responseChan chan hasherSeed
}

// newHasherSeedRequest describes a read of the hasher's accumulated state.
func newHasherSeedRequest() *hasherSeedRequest {
	return &hasherSeedRequest{responseChan: make(chan hasherSeed, 1)}
}

// hasherReseedRequest replaces the hasher's accumulated state, for a caller that has replaced the databases
// underneath it — an import, or seeding a store's first version. Sent as a message for the same reason as
// hasherSeedRequest: only the hasher's own goroutine touches that state.
type hasherReseedRequest struct {
	hasherMessage

	// seed is the state to adopt.
	seed hasherSeed

	// responseChan produces a value once the state has been adopted.
	responseChan chan struct{}
}

// newHasherReseedRequest describes a replacement of the hasher's accumulated state.
func newHasherReseedRequest(seed hasherSeed) *hasherReseedRequest {
	return &hasherReseedRequest{seed: seed, responseChan: make(chan struct{}, 1)}
}
