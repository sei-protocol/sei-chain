package proxy

import (
	"context"
	"time"

	"github.com/go-kit/kit/metrics"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// proxyClient provides the application connection.
type proxyClient struct {
	app     types.Application
	metrics *Metrics
}

// New creates a proxy application interface around the provided ABCI application.
func New(app types.Application, metrics *Metrics) types.Application {
	return &proxyClient{
		metrics: metrics,
		app:     app,
	}
}

func (app *proxyClient) InitChain(ctx context.Context, req *types.RequestInitChain) (*types.ResponseInitChain, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "init_chain", "type", "sync"))()
	return app.app.InitChain(ctx, req)
}

func (app *proxyClient) PrepareProposal(ctx context.Context, req *types.RequestPrepareProposal) (*types.ResponsePrepareProposal, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "prepare_proposal", "type", "sync"))()
	return app.app.PrepareProposal(ctx, req)
}

func (app *proxyClient) ProcessProposal(ctx context.Context, req *types.RequestProcessProposal) (*types.ResponseProcessProposal, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "process_proposal", "type", "sync"))()
	return app.app.ProcessProposal(ctx, req)
}

func (app *proxyClient) FinalizeBlock(ctx context.Context, req *types.RequestFinalizeBlock) (*types.ResponseFinalizeBlock, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "finalize_block", "type", "sync"))()
	return app.app.FinalizeBlock(ctx, req)
}

func (app *proxyClient) GetTxPriorityHint(ctx context.Context, req *types.RequestGetTxPriorityHintV2) (*types.ResponseGetTxPriorityHint, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "get_tx_priority", "type", "sync"))()
	return app.app.GetTxPriorityHint(ctx, req)
}

func (app *proxyClient) Commit(ctx context.Context) (*types.ResponseCommit, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "commit", "type", "sync"))()
	return app.app.Commit(ctx)
}

func (app *proxyClient) CheckTx(ctx context.Context, req *types.RequestCheckTxV2) (*types.ResponseCheckTxV2, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "check_tx", "type", "sync"))()
	return app.app.CheckTx(ctx, req)
}

func (app *proxyClient) Info(ctx context.Context, req *types.RequestInfo) (*types.ResponseInfo, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "info", "type", "sync"))()
	return app.app.Info(ctx, req)
}

func (app *proxyClient) Query(ctx context.Context, req *types.RequestQuery) (*types.ResponseQuery, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "query", "type", "sync"))()
	return app.app.Query(ctx, req)
}

func (app *proxyClient) ListSnapshots(ctx context.Context, req *types.RequestListSnapshots) (*types.ResponseListSnapshots, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "list_snapshots", "type", "sync"))()
	return app.app.ListSnapshots(ctx, req)
}

func (app *proxyClient) OfferSnapshot(ctx context.Context, req *types.RequestOfferSnapshot) (*types.ResponseOfferSnapshot, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "offer_snapshot", "type", "sync"))()
	return app.app.OfferSnapshot(ctx, req)
}

func (app *proxyClient) LoadSnapshotChunk(ctx context.Context, req *types.RequestLoadSnapshotChunk) (*types.ResponseLoadSnapshotChunk, error) {
	defer addTimeSample(app.metrics.MethodTiming.With("method", "load_snapshot_chunk", "type", "sync"))()
	return app.app.LoadSnapshotChunk(ctx, req)
}

func (app *proxyClient) ApplySnapshotChunk(ctx context.Context, req *types.RequestApplySnapshotChunk) (*types.ResponseApplySnapshotChunk, error) {
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
