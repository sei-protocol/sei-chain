package evmrpc

import (
	"sync"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// blockHeaderEvent is the in-process payload delivered to SubscriptionAPI
// for each committed block. It mirrors the data evmrpc needs to build an
// Ethereum block header, without going through the Tendermint event bus.
//
// hash is the autobahn lane-block header hash passed as Hash to
// app.FinalizeBlock — NOT a hash computed over the (partially-populated)
// Tendermint Header we synthesize for this event. This is the same value
// the eth_getBlockBy* and receipt API surfaces report as blockHash (the
// receipt store on disk records a zero blockHash; evmrpc overlays this
// hash at read time, see evmrpc/tx.go). Surfacing the same hash here
// keeps eth_newHeads consistent with the rest of the EVM RPC surface.
type blockHeaderEvent struct {
	hash     []byte
	header   *tmproto.Header
	response *abci.ResponseFinalizeBlock
}

// BlockHeaderNotifier feeds eth_subscribe("newHeads") via a direct
// in-process channel. The sei-chain App stashes FinalizeBlock outputs
// (Stash) and publishes them after a successful Commit (PublishStashed),
// so subscribers only observe committed state. The single consumer is
// SubscriptionAPI's fan-out goroutine, which broadcasts to all per-client
// subscribers.
//
// Channel semantics: OnBlockCommitted is non-blocking and overwrite-on-
// full. If the consumer is lagging, the oldest buffered event is dropped
// in favour of the newest — for eth_newHeads the latest head is always
// more useful than a stale one.
//
// Stash/ClearStash/PublishStashed protect the FinalizeBlock→Commit
// pairing with an internal mutex. Callers do NOT need to serialize
// externally; calling on a nil receiver is a no-op (so the App can
// invoke unconditionally when Autobahn isn't enabled).
type BlockHeaderNotifier struct {
	ch chan blockHeaderEvent

	mu      sync.Mutex
	pending *blockHeaderEvent
}

func NewBlockHeaderNotifier(capacity int) *BlockHeaderNotifier {
	return &BlockHeaderNotifier{ch: make(chan blockHeaderEvent, capacity)}
}

// Stash records FinalizeBlock outputs for publication on the next
// successful Commit. Any previously-stashed event is overwritten — the
// expectation is exactly one Stash per FinalizeBlock invocation, with
// the FinalizeBlocker entry calling ClearStash to defend against return
// paths that don't reach a Stash.
//
// Safe to call on a nil receiver. Callers must pass non-nil req and resp.
func (n *BlockHeaderNotifier) Stash(req *abci.RequestFinalizeBlock, resp *abci.ResponseFinalizeBlock) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pending = &blockHeaderEvent{hash: req.Hash, header: req.Header, response: resp}
}

// ClearStash drops any pending stash without publishing. Called at
// FinalizeBlocker entry so a stale tuple from a prior block whose
// Commit failed (or whose FinalizeBlocker took a return path that
// didn't Stash) cannot be republished by a later Commit.
//
// Safe to call on a nil receiver.
func (n *BlockHeaderNotifier) ClearStash() {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pending = nil
}

// PublishStashed publishes the currently-stashed event (if any) on the
// fan-out channel and clears the stash. Returns true if an event was
// published, false otherwise (no stash, or nil receiver). Called after
// a successful Commit.
//
// Safe to call on a nil receiver.
func (n *BlockHeaderNotifier) PublishStashed() bool {
	if n == nil {
		return false
	}
	n.mu.Lock()
	evt := n.pending
	n.pending = nil
	n.mu.Unlock()
	if evt == nil {
		return false
	}
	n.publish(*evt)
	return true
}

// OnBlockCommitted publishes a committed-block event directly to the
// fan-out channel without going through the Stash/Publish pairing. The
// App layer uses Stash + PublishStashed; this entry point exists for
// producers that already serialize their FinalizeBlock/Commit and want
// to push without an intermediate stash.
func (n *BlockHeaderNotifier) OnBlockCommitted(hash []byte, header *tmproto.Header, response *abci.ResponseFinalizeBlock) {
	if n == nil {
		return
	}
	n.publish(blockHeaderEvent{hash: hash, header: header, response: response})
}

// publish pushes evt onto the fan-out channel with overwrite-on-full
// semantics. Used by both PublishStashed and OnBlockCommitted so the
// channel-write code lives in one place.
func (n *BlockHeaderNotifier) publish(evt blockHeaderEvent) {
	select {
	case n.ch <- evt:
		return
	default:
	}
	// Buffer full: drain one stale event to make room for the new one.
	// With a single producer, draining one slot is sufficient and the
	// second send always succeeds. With multiple producers a racing
	// publisher could refill the slot between the drain and the send,
	// in which case the default branch drops the new event — that is
	// still consistent with overwrite-on-full (some recent head wins).
	select {
	case <-n.ch:
	default:
	}
	select {
	case n.ch <- evt:
	default:
	}
}

func (n *BlockHeaderNotifier) recv() <-chan blockHeaderEvent {
	return n.ch
}
