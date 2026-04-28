package types

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/metrics"
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

	// Mempool Connection
	CheckTx(context.Context, *RequestCheckTxV2) (*ResponseCheckTxV2, error)                             // Validate a tx for the mempool
	GetTxPriorityHint(context.Context, *RequestGetTxPriorityHintV2) (*ResponseGetTxPriorityHint, error) // Get tx priority before checkTx

	// Consensus Connection
	InitChain(context.Context, *RequestInitChain) (*ResponseInitChain, error) // Initialize blockchain w validators/other info from TendermintCore
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

func (BaseApplication) CheckTx(_ context.Context, req *RequestCheckTxV2) (*ResponseCheckTxV2, error) {
	return &ResponseCheckTxV2{ResponseCheckTx: &ResponseCheckTx{Code: CodeTypeOK}}, nil
}

func (BaseApplication) Commit(_ context.Context) (*ResponseCommit, error) {
	return &ResponseCommit{}, nil
}

func (BaseApplication) Query(_ context.Context, req *RequestQuery) (*ResponseQuery, error) {
	return &ResponseQuery{Code: CodeTypeOK}, nil
}

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

func (BaseApplication) FinalizeBlock(_ context.Context, req *RequestFinalizeBlock) (*ResponseFinalizeBlock, error) {
	txs := make([]*ExecTxResult, len(req.Txs))
	for i := range req.Txs {
		txs[i] = &ExecTxResult{Code: CodeTypeOK}
	}
	return &ResponseFinalizeBlock{TxResults: txs}, nil
}

// ProxyApplication wraps an Application and records ABCI method timings.
type ProxyApplication struct {
	app     Application
	metrics *ProxyMetrics
}

// NewProxyApplication creates a proxied application interface around the
// provided ABCI application.
func NewProxyApplication(app Application, metrics *ProxyMetrics) *ProxyApplication {
	return &ProxyApplication{app: app, metrics: metrics}
}

func (app *ProxyApplication) InitChain(ctx context.Context, req *RequestInitChain) (*ResponseInitChain, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "init_chain", "type", "sync"))()
	return app.app.InitChain(ctx, req)
}

func (app *ProxyApplication) ProcessProposal(ctx context.Context, req *RequestProcessProposal) (*ResponseProcessProposal, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "process_proposal", "type", "sync"))()
	return app.app.ProcessProposal(ctx, req)
}

func (app *ProxyApplication) FinalizeBlock(ctx context.Context, req *RequestFinalizeBlock) (*ResponseFinalizeBlock, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "finalize_block", "type", "sync"))()
	return app.app.FinalizeBlock(ctx, req)
}

func (app *ProxyApplication) GetTxPriorityHint(ctx context.Context, req *RequestGetTxPriorityHintV2) (*ResponseGetTxPriorityHint, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "get_tx_priority", "type", "sync"))()
	return app.app.GetTxPriorityHint(ctx, req)
}

func (app *ProxyApplication) Commit(ctx context.Context) (*ResponseCommit, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "commit", "type", "sync"))()
	return app.app.Commit(ctx)
}

func (app *ProxyApplication) CheckTx(ctx context.Context, req *RequestCheckTxV2) (res *ResponseCheckTxV2, err error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "check_tx", "type", "sync"))()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered in CheckTx: %v", r)
		}
	}()
	return app.app.CheckTx(ctx, req)
}

func (app *ProxyApplication) Info(ctx context.Context, req *RequestInfo) (*ResponseInfo, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "info", "type", "sync"))()
	return app.app.Info(ctx, req)
}

func (app *ProxyApplication) Query(ctx context.Context, req *RequestQuery) (*ResponseQuery, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "query", "type", "sync"))()
	return app.app.Query(ctx, req)
}

func (app *ProxyApplication) GetValidators() []ValidatorUpdate {
	return app.app.GetValidators()
}

func (app *ProxyApplication) ListSnapshots(ctx context.Context, req *RequestListSnapshots) (*ResponseListSnapshots, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "list_snapshots", "type", "sync"))()
	return app.app.ListSnapshots(ctx, req)
}

func (app *ProxyApplication) OfferSnapshot(ctx context.Context, req *RequestOfferSnapshot) (*ResponseOfferSnapshot, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "offer_snapshot", "type", "sync"))()
	return app.app.OfferSnapshot(ctx, req)
}

func (app *ProxyApplication) LoadSnapshotChunk(ctx context.Context, req *RequestLoadSnapshotChunk) (*ResponseLoadSnapshotChunk, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "load_snapshot_chunk", "type", "sync"))()
	return app.app.LoadSnapshotChunk(ctx, req)
}

func (app *ProxyApplication) ApplySnapshotChunk(ctx context.Context, req *RequestApplySnapshotChunk) (*ResponseApplySnapshotChunk, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "apply_snapshot_chunk", "type", "sync"))()
	return app.app.ApplySnapshotChunk(ctx, req)
}

// addTimeSample returns a function that, when called, adds an observation to m.
// The observation added to m is the number of seconds ellapsed since addTimeSample
// was initially called. addTimeSample is meant to be called in a defer to calculate
// the amount of time a function takes to complete.
func addTimeSample(m metrics.Histogram) func() {
	start := time.Now()
	return func() { m.Observe(time.Since(start).Seconds()) }
}
