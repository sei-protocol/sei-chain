package evmrpc

import (
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// blockHeaderEvent is the in-process payload delivered to SubscriptionAPI
// for each committed block. It mirrors the data evmrpc needs to build an
// Ethereum block header, without going through the Tendermint event bus.
//
// hash is the canonical block hash that EVM contracts and the receipt
// store see. Under Autobahn that is the autobahn lane-block header hash
// passed as Hash to app.FinalizeBlock — NOT a hash computed over the
// (partially-populated) Tendermint Header we synthesize for this event.
// Surfacing the receipt-store hash here keeps eth_newHeads consistent
// with what eth_getTransactionReceipt and eth_getBlockBy* return.
type blockHeaderEvent struct {
	hash     []byte
	header   *tmproto.Header
	response *abci.ResponseFinalizeBlock
}

// BlockHeaderNotifier implements sei-tendermint/types.BlockHeaderListener
// and feeds eth_subscribe("newHeads") via a direct in-process channel.
//
// Producers (e.g. the Autobahn block-execution path) call OnBlockCommitted
// once per committed block. The single consumer is SubscriptionAPI's
// fan-out goroutine, which broadcasts to all per-client subscribers.
//
// OnBlockCommitted is non-blocking and uses overwrite-on-full semantics:
// if the consumer is lagging, the oldest buffered event is dropped in
// favour of the newest. For eth_newHeads, the latest head is always more
// useful than a stale one.
//
// Concurrency: assumes a single producer (the block-execution loop) and a
// single consumer. With those invariants, after the drain step there is
// guaranteed space for the new event.
type BlockHeaderNotifier struct {
	ch chan blockHeaderEvent
}

func NewBlockHeaderNotifier(capacity int) *BlockHeaderNotifier {
	return &BlockHeaderNotifier{ch: make(chan blockHeaderEvent, capacity)}
}

// OnBlockCommitted implements types.BlockHeaderListener.
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
	// With a single producer, draining one slot is sufficient; the second
	// send must succeed under that invariant. The default branch is
	// defensive and unreachable in practice.
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
