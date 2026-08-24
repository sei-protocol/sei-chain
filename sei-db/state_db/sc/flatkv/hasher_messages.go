package flatkv

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
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

	// oldValues is the read of the values this block replaced, started when the hasher pulled the block
	// into its look-ahead window. Nil for a block the hasher reached without reading ahead, which reads
	// inline instead.
	oldValues *pendingOldValues
}

// pendingOldValues is an in-progress or completed read of the values one block replaced.
type pendingOldValues struct {
	// done is closed once changed and err are set.
	done chan struct{}

	// changed is every key the block changed with the value it replaced, one entry per data store. Valid
	// only once done is closed.
	changed []lthash.DBPairs

	// err is the read's failure. Valid only once done is closed.
	err error
}

// await blocks until the read completes and reports its result.
func (p *pendingOldValues) await() ([]lthash.DBPairs, error) {
	<-p.done
	return p.changed, p.err
}

// awaitOldValues reports the values this block replaced, reading them now if none was started ahead of time.
func (r *hashRequest) awaitOldValues(pool threading.Pool) ([]lthash.DBPairs, error) {
	if r.oldValues == nil {
		return changedValuesByStore(pool, r.current, r.previous)
	}
	return r.oldValues.await()
}

// releasePrevious hands back the preceding block's reservations, which are needed only for the old-value
// read. Called as soon as that read finishes, so the databases resume flushing while this block waits its
// turn to be folded rather than after it.
//
// Idempotent: it clears previous, and release covers whatever is left.
func (r *hashRequest) releasePrevious() error {
	previous := r.previous
	r.previous = nil

	var errs []error
	for name, snap := range previous {
		if err := snap.Release(); err != nil {
			errs = append(errs, fmt.Errorf("release previous %s snapshot at version %d: %w",
				name, r.version, err))
		}
	}
	return errors.Join(errs...)
}

// release hands back every reservation this request still holds, so the databases can resume writing out
// later blocks.
//
// Every reservation is handed back even if one of them fails, because a reservation left held stalls its
// database's flushes indefinitely. The failures are joined and returned.
func (r *hashRequest) release() error {
	// A read started ahead of time reads through these snapshots and hands the preceding block's back
	// itself, so it has to finish before this decides what is left. Awaiting here rather than at each
	// caller is what stops a path that abandons a block without hashing it from releasing snapshots out
	// from under a read that is still running.
	if r.oldValues != nil {
		_, _ = r.oldValues.await()
	}

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
