package types

import (
	abci "github.com/tendermint/tendermint/abci/types"
)

// InitChainer initializes application state at genesis
type InitChainer func(ctx Context, req abci.RequestInitChain) abci.ResponseInitChain

// BeginBlocker runs code before the transactions in a block
//
// Note: applications which set create_empty_blocks=false will not have regular block timing and should use
// e.g. BFT timestamps rather than block height for any periodic BeginBlock logic
type BeginBlocker func(ctx Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock

// MidBlocker runs code after the early transactions in a block and return any relevant events
type MidBlocker func(ctx Context, height int64) []abci.Event

// EndBlocker runs code after the transactions in a block and return updates to the validator set
//
// Note: applications which set create_empty_blocks=false will not have regular block timing and should use
// e.g. BFT timestamps rather than block height for any periodic EndBlock logic
type EndBlocker func(ctx Context, req abci.RequestEndBlock) abci.ResponseEndBlock

// PeerFilter responds to p2p filtering queries from Tendermint
type PeerFilter func(info string) abci.ResponseQuery

type PrepareProposalHandler func(ctx Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error)

type ProcessProposalHandler func(ctx Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error)

type FinalizeBlocker func(ctx Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error)

type LoadVersionHandler func() error

type PreCommitHandler func(ctx Context) error
type CloseHandler func() error
