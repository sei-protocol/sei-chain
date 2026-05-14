package evmrpc

import (
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
// in-process channel. The sei-chain App pushes one event per committed
// block from its Commit override (see app/app.go). The single consumer
// is SubscriptionAPI's fan-out goroutine, which broadcasts to all
// per-client subscribers.
//
// OnBlockCommitted is non-blocking and uses overwrite-on-full semantics:
// if the consumer is lagging, the oldest buffered event is dropped in
// favour of the newest. For eth_newHeads, the latest head is always more
// useful than a stale one.
//
// Concurrency: designed for a single producer (the block-execution loop)
// and a single consumer. Under that invariant the drain+send sequence in
// OnBlockCommitted always lands the new event. With multiple concurrent
// producers the same drain/send sequence still terminates without
// blocking, but the "latest" survivor among any racing publishes is
// nondeterministic — which is still acceptable for newHeads (we promise
// only that some recent head wins, not strict ordering across concurrent
// publishers).
type BlockHeaderNotifier struct {
	ch chan blockHeaderEvent
}

func NewBlockHeaderNotifier(capacity int) *BlockHeaderNotifier {
	return &BlockHeaderNotifier{ch: make(chan blockHeaderEvent, capacity)}
}

// OnBlockCommitted publishes a committed-block event to the fan-out channel.
func (n *BlockHeaderNotifier) OnBlockCommitted(hash []byte, header *tmproto.Header, response *abci.ResponseFinalizeBlock) {
	if n == nil {
		return
	}
	evt := blockHeaderEvent{hash: hash, header: header, response: response}
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
