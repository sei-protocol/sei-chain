package types

import (
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// BlockHeaderListener is invoked once a block has been finalized and
// committed by the application. It is intended as a lightweight in-process
// alternative to subscribing on the Tendermint event bus for consumers that
// only need to be notified that a new head is available.
//
// Implementations must not block: the call site sits on the block-execution
// hot path and a slow listener will stall block production.
type BlockHeaderListener interface {
	OnBlockCommitted(hash []byte, header *tmproto.Header, response *abci.ResponseFinalizeBlock)
}
