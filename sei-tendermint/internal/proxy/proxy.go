package proxy

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// Proxy wraps an ABCI application and records ABCI method timings.
type Proxy struct {
	app types.Application
	evm utils.Option[*EVMProxy]
}

// New creates a proxied application interface around the provided ABCI application.
func New(app types.Application) *Proxy {
	p := &Proxy{app: app}
	// Resolved once here so no EVM method needs a per-call type assertion.
	if evmApp, ok := app.(EVMApplication); ok {
		p.evm = utils.Some(&EVMProxy{app: evmApp})
	}
	return p
}

// EVM returns the proxy for the wrapped application's EVM methods, if it is an
// EVMApplication.
func (app *Proxy) EVM() utils.Option[*EVMProxy] {
	return app.evm
}

func (app *Proxy) InitChain(req *types.RequestInitChain) (*types.ResponseInitChain, error) {
	defer addTimeSample(Global.MethodTimingAt("init_chain", "sync"))()
	return app.app.InitChain(req)
}

func (app *Proxy) ProcessProposal(ctx context.Context, req *types.RequestProcessProposal) (*types.ResponseProcessProposal, error) {
	defer addTimeSample(Global.MethodTimingAt("process_proposal", "sync"))()
	return app.app.ProcessProposal(ctx, req)
}

func (app *Proxy) FinalizeBlock(ctx context.Context, req *types.RequestFinalizeBlock) (*types.ResponseFinalizeBlock, error) {
	defer addTimeSample(Global.MethodTimingAt("finalize_block", "sync"))()
	return app.app.FinalizeBlock(ctx, req)
}

func (app *Proxy) GetTxPriorityHint(ctx context.Context, req *types.RequestGetTxPriorityHintV2) (*types.ResponseGetTxPriorityHint, error) {
	defer addTimeSample(Global.MethodTimingAt("get_tx_priority", "sync"))()
	return app.app.GetTxPriorityHint(ctx, req)
}

func (app *Proxy) EvmNonce(addr common.Address) uint64 {
	defer addTimeSample(Global.MethodTimingAt("evm_nonce", "sync"))()
	return app.app.EvmNonce(addr)
}

func (app *Proxy) EvmBalance(addr common.Address, seiAddr []byte) uint256.Int {
	defer addTimeSample(Global.MethodTimingAt("evm_balance", "sync"))()
	return app.app.EvmBalance(addr, seiAddr)
}

// commitSettler is implemented by applications whose block state commits land
// behind FinalizeBlock.
type commitSettler interface {
	AwaitCommits() error
}

// AwaitCommits blocks until every block the wrapped application has finalized
// is in its store, and reports the first commit that failed. It returns nil for
// an application that commits synchronously.
func (app *Proxy) AwaitCommits() error {
	defer addTimeSample(Global.MethodTimingAt("await_commits", "sync"))()
	settler, ok := app.app.(commitSettler)
	if !ok {
		return nil
	}
	return settler.AwaitCommits()
}

type epochWindowApp interface {
	SetEpochWindow(atypes.GlobalBlockNumber, epoch.StateWindow) error
	CommittedEpochWindow() (utils.Option[epoch.StateWindow], error)
}

// SupportsEpochWindow reports whether the wrapped application stores Autobahn
// epoch state.
func (app *Proxy) SupportsEpochWindow() bool {
	_, ok := app.app.(epochWindowApp)
	return ok
}

// SetEpochWindow stages an epoch window on applications that store Autobahn
// epoch state.
func (app *Proxy) SetEpochWindow(height atypes.GlobalBlockNumber, window epoch.StateWindow) error {
	defer addTimeSample(Global.MethodTimingAt("set_epoch_window", "sync"))()
	windowApp, ok := app.app.(epochWindowApp)
	if !ok {
		return nil
	}
	return windowApp.SetEpochWindow(height, window)
}

// CommittedEpochWindow returns the epoch window in the wrapped application's
// committed state. It returns None if the application does not store Autobahn
// epoch state or no epoch window was committed.
func (app *Proxy) CommittedEpochWindow() (utils.Option[epoch.StateWindow], error) {
	defer addTimeSample(Global.MethodTimingAt("committed_epoch_window", "sync"))()
	windowApp, ok := app.app.(epochWindowApp)
	if !ok {
		return utils.None[epoch.StateWindow](), nil
	}
	return windowApp.CommittedEpochWindow()
}

// evmCodeReader is implemented by applications that can read contract code
// from their current EVM state.
type evmCodeReader interface {
	EvmCode(common.Address) []byte
}

// EvmCode returns the contract code at addr in the wrapped application's
// current EVM state. It errors if that application does not expose EVM code.
func (app *Proxy) EvmCode(addr common.Address) ([]byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_code", "sync"))()
	reader, ok := app.app.(evmCodeReader)
	if !ok {
		return nil, fmt.Errorf("application does not expose EVM code")
	}
	return reader.EvmCode(addr), nil
}

// evmStorageReader is implemented by applications that can read contract
// storage from their current EVM state.
type evmStorageReader interface {
	EvmStorage(common.Address, common.Hash) common.Hash
}

// EvmStorage returns the value at key in addr's storage in the wrapped
// application's current EVM state. It errors if that application does not
// expose EVM storage.
func (app *Proxy) EvmStorage(addr common.Address, key common.Hash) (common.Hash, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_storage", "sync"))()
	reader, ok := app.app.(evmStorageReader)
	if !ok {
		return common.Hash{}, fmt.Errorf("application does not expose EVM storage")
	}
	return reader.EvmStorage(addr, key), nil
}

// evmHistoryReader is implemented by applications that serve their EVM state
// after past heights.
type evmHistoryReader interface {
	EvmBalanceAt(common.Address, int64) (uint256.Int, error)
	EvmCodeAt(common.Address, int64) ([]byte, error)
	EvmNonceAt(common.Address, int64) (uint64, error)
	EvmStorageAt(common.Address, common.Hash, int64) (common.Hash, error)
}

var errNoEVMHistory = errors.New("application does not serve historical EVM state")

// EvmBalanceAt returns addr's balance in the wrapped application's EVM state
// after height. It errors if that application serves no history.
func (app *Proxy) EvmBalanceAt(addr common.Address, height int64) (uint256.Int, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_balance_at", "sync"))()
	reader, ok := app.app.(evmHistoryReader)
	if !ok {
		return uint256.Int{}, errNoEVMHistory
	}
	return reader.EvmBalanceAt(addr, height)
}

// EvmCodeAt returns the contract code at addr in the wrapped application's EVM
// state after height. It errors if that application serves no history.
func (app *Proxy) EvmCodeAt(addr common.Address, height int64) ([]byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_code_at", "sync"))()
	reader, ok := app.app.(evmHistoryReader)
	if !ok {
		return nil, errNoEVMHistory
	}
	return reader.EvmCodeAt(addr, height)
}

// EvmNonceAt returns addr's nonce in the wrapped application's EVM state after
// height. It errors if that application serves no history.
func (app *Proxy) EvmNonceAt(addr common.Address, height int64) (uint64, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_nonce_at", "sync"))()
	reader, ok := app.app.(evmHistoryReader)
	if !ok {
		return 0, errNoEVMHistory
	}
	return reader.EvmNonceAt(addr, height)
}

// EvmStorageAt returns the value at key in addr's storage in the wrapped application's EVM state
// after height. It errors if that application serves no history.
func (app *Proxy) EvmStorageAt(addr common.Address, key common.Hash, height int64) (common.Hash, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_storage_at", "sync"))()
	reader, ok := app.app.(evmHistoryReader)
	if !ok {
		return common.Hash{}, errNoEVMHistory
	}
	return reader.EvmStorageAt(addr, key, height)
}

func (app *Proxy) Commit(ctx context.Context) (*types.ResponseCommit, error) {
	defer addTimeSample(Global.MethodTimingAt("commit", "sync"))()
	return app.app.Commit(ctx)
}

func (app *Proxy) CheckTxSafe(ctx context.Context, req *types.RequestCheckTxV2) (res *types.ResponseCheckTxV2, err error) {
	defer addTimeSample(Global.MethodTimingAt("check_tx", "sync"))()
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

func (app *Proxy) InitLastHeader(lastHeader *tmproto.Header) {
	app.app.InitLastHeader(lastHeader)
}

func (app *Proxy) Info() *types.ResponseInfo {
	defer addTimeSample(Global.MethodTimingAt("info", "sync"))()
	return app.app.Info()
}

func (app *Proxy) Query(ctx context.Context, req *types.RequestQuery) (*types.ResponseQuery, error) {
	defer addTimeSample(Global.MethodTimingAt("query", "sync"))()
	return app.app.Query(ctx, req)
}

func (app *Proxy) GetValidators() []types.ValidatorUpdate {
	return app.app.GetValidators()
}

func (app *Proxy) LastBlockHeight() int64 {
	return app.app.LastBlockHeight()
}

func (app *Proxy) ListSnapshots(ctx context.Context, req *types.RequestListSnapshots) (*types.ResponseListSnapshots, error) {
	defer addTimeSample(Global.MethodTimingAt("list_snapshots", "sync"))()
	return app.app.ListSnapshots(ctx, req)
}

func (app *Proxy) OfferSnapshot(ctx context.Context, req *types.RequestOfferSnapshot) (*types.ResponseOfferSnapshot, error) {
	defer addTimeSample(Global.MethodTimingAt("offer_snapshot", "sync"))()
	return app.app.OfferSnapshot(ctx, req)
}

func (app *Proxy) LoadSnapshotChunk(ctx context.Context, req *types.RequestLoadSnapshotChunk) (*types.ResponseLoadSnapshotChunk, error) {
	defer addTimeSample(Global.MethodTimingAt("load_snapshot_chunk", "sync"))()
	return app.app.LoadSnapshotChunk(ctx, req)
}

func (app *Proxy) ApplySnapshotChunk(ctx context.Context, req *types.RequestApplySnapshotChunk) (*types.ResponseApplySnapshotChunk, error) {
	defer addTimeSample(Global.MethodTimingAt("apply_snapshot_chunk", "sync"))()
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
