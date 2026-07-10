package types

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// Application is an interface that enables any finite, deterministic state machine
// to be driven by a blockchain-based replication engine via the ABCI.
//
//go:generate ../../scripts/mockery_generate.sh Application
type Application interface {
	// Info/Query Connection
	Info(context.Context, *RequestInfo) (*ResponseInfo, error)    // Return application info
	Query(context.Context, *RequestQuery) (*ResponseQuery, error) // Query for state
	GetValidators() []ValidatorUpdate
	// LastBlockHeight returns the height of the most recently committed
	// block, as maintained by the app. Used by /status — must be a fast
	// in-memory read; Info() is too heavy for the hot path.
	LastBlockHeight() int64

	// Mempool Connection
	CheckTx(context.Context, *RequestCheckTxV2) *ResponseCheckTxV2                                      // Validate a tx for the mempool
	GetTxPriorityHint(context.Context, *RequestGetTxPriorityHintV2) (*ResponseGetTxPriorityHint, error) // Get tx priority before checkTx
	EvmNonce(common.Address) uint64
	EvmBalance(common.Address, []byte) uint256.Int

	// Consensus Connection
	InitChain(context.Context, *RequestInitChain) (*ResponseInitChain, error) // Initialize blockchain w validators/other info from TendermintCore
	InitLastHeader(lastHeader *tmproto.Header)
	ProcessProposal(context.Context, *RequestProcessProposal) (*ResponseProcessProposal, error)
	// Commit the state and return the application Merkle root hash
	Commit(context.Context) (*ResponseCommit, error)
	// Deliver the decided block with its txs to the Application
	FinalizeBlock(context.Context, *RequestFinalizeBlock) (*ResponseFinalizeBlock, error)

	// State Sync Connection
	ListSnapshots(context.Context, *RequestListSnapshots) (*ResponseListSnapshots, error)                // List available snapshots
	OfferSnapshot(context.Context, *RequestOfferSnapshot) (*ResponseOfferSnapshot, error)                // Offer a snapshot to the application
	LoadSnapshotChunk(context.Context, *RequestLoadSnapshotChunk) (*ResponseLoadSnapshotChunk, error)    // Load a snapshot chunk
	ApplySnapshotChunk(context.Context, *RequestApplySnapshotChunk) (*ResponseApplySnapshotChunk, error) // Apply a shapshot chunk
}

//-------------------------------------------------------
// BaseApplication is a base form of Application

var _ Application = BaseApplication{}

type BaseApplication struct{}

func (BaseApplication) Info(_ context.Context, req *RequestInfo) (*ResponseInfo, error) {
	return &ResponseInfo{}, nil
}
func (BaseApplication) GetValidators() []ValidatorUpdate { return nil }
func (BaseApplication) LastBlockHeight() int64           { return 0 }

func (BaseApplication) CheckTx(_ context.Context, req *RequestCheckTxV2) *ResponseCheckTxV2 {
	return &ResponseCheckTxV2{ResponseCheckTx: &ResponseCheckTx{Code: CodeTypeOK}}
}

func (BaseApplication) Commit(_ context.Context) (*ResponseCommit, error) {
	return &ResponseCommit{}, nil
}

func (BaseApplication) Query(_ context.Context, req *RequestQuery) (*ResponseQuery, error) {
	return &ResponseQuery{Code: CodeTypeOK}, nil
}

func (BaseApplication) InitLastHeader(lastHeader *tmproto.Header) {}

func (BaseApplication) InitChain(_ context.Context, req *RequestInitChain) (*ResponseInitChain, error) {
	return &ResponseInitChain{}, nil
}

func (BaseApplication) ListSnapshots(_ context.Context, req *RequestListSnapshots) (*ResponseListSnapshots, error) {
	return &ResponseListSnapshots{}, nil
}

func (BaseApplication) OfferSnapshot(_ context.Context, req *RequestOfferSnapshot) (*ResponseOfferSnapshot, error) {
	return &ResponseOfferSnapshot{}, nil
}

func (BaseApplication) LoadSnapshotChunk(_ context.Context, _ *RequestLoadSnapshotChunk) (*ResponseLoadSnapshotChunk, error) {
	return &ResponseLoadSnapshotChunk{}, nil
}

func (BaseApplication) ApplySnapshotChunk(_ context.Context, req *RequestApplySnapshotChunk) (*ResponseApplySnapshotChunk, error) {
	return &ResponseApplySnapshotChunk{}, nil
}

func (BaseApplication) ProcessProposal(_ context.Context, req *RequestProcessProposal) (*ResponseProcessProposal, error) {
	return &ResponseProcessProposal{Status: ResponseProcessProposal_ACCEPT}, nil
}

func (BaseApplication) GetTxPriorityHint(context.Context, *RequestGetTxPriorityHintV2) (*ResponseGetTxPriorityHint, error) {
	return &ResponseGetTxPriorityHint{}, nil
}

func (BaseApplication) EvmNonce(common.Address) uint64 {
	return 0
}

func (BaseApplication) EvmBalance(common.Address, []byte) uint256.Int {
	return uint256.Int{}
}

func (BaseApplication) FinalizeBlock(_ context.Context, req *RequestFinalizeBlock) (*ResponseFinalizeBlock, error) {
	txs := make([]*ExecTxResult, len(req.Txs))
	for i := range req.Txs {
		txs[i] = &ExecTxResult{Code: CodeTypeOK}
	}
	return &ResponseFinalizeBlock{TxResults: txs}, nil
}
