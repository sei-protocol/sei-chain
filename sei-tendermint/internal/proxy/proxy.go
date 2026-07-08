package proxy

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// Proxy wraps an ABCI application and records ABCI method timings.
type Proxy struct {
	app     types.Application
	metrics *Metrics
}

// New creates a proxied application interface around the provided ABCI application.
func New(app types.Application, metrics *Metrics) *Proxy {
	return &Proxy{app: app, metrics: metrics}
}

func (app *Proxy) InitChain(ctx context.Context, req *types.RequestInitChain) (*types.ResponseInitChain, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("init_chain", "sync"))()
	return app.app.InitChain(ctx, req)
}

func (app *Proxy) ProcessProposal(ctx context.Context, req *types.RequestProcessProposal) (*types.ResponseProcessProposal, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("process_proposal", "sync"))()
	return app.app.ProcessProposal(ctx, req)
}

func (app *Proxy) FinalizeBlock(ctx context.Context, req *types.RequestFinalizeBlock) (*types.ResponseFinalizeBlock, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("finalize_block", "sync"))()
	return app.app.FinalizeBlock(ctx, req)
}

func (app *Proxy) GetTxPriorityHint(ctx context.Context, req *types.RequestGetTxPriorityHintV2) (*types.ResponseGetTxPriorityHint, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("get_tx_priority", "sync"))()
	return app.app.GetTxPriorityHint(ctx, req)
}

func (app *Proxy) EvmNonce(addr common.Address) uint64 {
	defer addTimeSample(app.metrics.MethodTimingAt("evm_nonce", "sync"))()
	return app.app.EvmNonce(addr)
}

func (app *Proxy) EvmBalance(addr common.Address, seiAddr []byte) uint256.Int {
	defer addTimeSample(app.metrics.MethodTimingAt("evm_balance", "sync"))()
	return app.app.EvmBalance(addr, seiAddr)
}

func (app *Proxy) Commit(ctx context.Context) (*types.ResponseCommit, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("commit", "sync"))()
	return app.app.Commit(ctx)
}

func (app *Proxy) CheckTxSafe(ctx context.Context, req *types.RequestCheckTxV2) (res *types.ResponseCheckTxV2, err error) {
	defer addTimeSample(app.metrics.MethodTimingAt("check_tx", "sync"))()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered in CheckTxSafe: %v\n%v", r, string(debug.Stack()))
		}
	}()
	res = app.app.CheckTx(ctx, req)
	if res == nil {
		return nil, fmt.Errorf("nil response")
	}
	if res.IsEVM {
		if res.EVMHash == (common.Hash{}) {
			return nil, fmt.Errorf("EVM response missing EVMHash")
		}
		if len(res.SeiSenderAddress) == 0 {
			return nil, fmt.Errorf("EVM response missing SeiSenderAddress")
		}
	}
	return res, nil
}

func (app *Proxy) Info(ctx context.Context, req *types.RequestInfo) (*types.ResponseInfo, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("info", "sync"))()
	return app.app.Info(ctx, req)
}

func (app *Proxy) Query(ctx context.Context, req *types.RequestQuery) (*types.ResponseQuery, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("query", "sync"))()
	return app.app.Query(ctx, req)
}

func (app *Proxy) GetValidators() []types.ValidatorUpdate {
	return app.app.GetValidators()
}

func (app *Proxy) LastBlockHeight() int64 {
	return app.app.LastBlockHeight()
}

func (app *Proxy) ListSnapshots(ctx context.Context, req *types.RequestListSnapshots) (*types.ResponseListSnapshots, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("list_snapshots", "sync"))()
	return app.app.ListSnapshots(ctx, req)
}

func (app *Proxy) OfferSnapshot(ctx context.Context, req *types.RequestOfferSnapshot) (*types.ResponseOfferSnapshot, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("offer_snapshot", "sync"))()
	return app.app.OfferSnapshot(ctx, req)
}

func (app *Proxy) LoadSnapshotChunk(ctx context.Context, req *types.RequestLoadSnapshotChunk) (*types.ResponseLoadSnapshotChunk, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("load_snapshot_chunk", "sync"))()
	return app.app.LoadSnapshotChunk(ctx, req)
}

func (app *Proxy) ApplySnapshotChunk(ctx context.Context, req *types.RequestApplySnapshotChunk) (*types.ResponseApplySnapshotChunk, error) {
	defer addTimeSample(app.metrics.MethodTimingAt("apply_snapshot_chunk", "sync"))()
	return app.app.ApplySnapshotChunk(ctx, req)
}

// addTimeSample returns a function that, when called, adds an observation to m.
// The observation added to m is the number of seconds ellapsed since addTimeSample
// was initially called. addTimeSample is meant to be called in a defer to calculate
// the amount of time a function takes to complete.
func addTimeSample(m prometheus.Observer) func() {
	start := time.Now()
	return func() { m.Observe(time.Since(start).Seconds()) }
}
