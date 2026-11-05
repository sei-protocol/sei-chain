package p2p

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// epochWindowTestApp is an EVM app over testApp that records the epoch window
// each block is finalized with. Its EVM read methods return errEVMNotServed.
type epochWindowTestApp struct {
	*testApp
	finalizeErr utils.Option[error]
	windows     map[int64]utils.Option[epoch.Window]
}

var _ proxy.EVMApplication = (*epochWindowTestApp)(nil)

func newEpochWindowTestApp() *epochWindowTestApp {
	return &epochWindowTestApp{testApp: newTestApp(), windows: map[int64]utils.Option[epoch.Window]{}}
}

func (a *epochWindowTestApp) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return a.FinalizeBlockWithEpochWindow(ctx, req, utils.None[epoch.Window]())
}

func (a *epochWindowTestApp) FinalizeBlockWithEpochWindow(
	ctx context.Context,
	req *abci.RequestFinalizeBlock,
	window utils.Option[epoch.Window],
) (*abci.ResponseFinalizeBlock, error) {
	if err, ok := a.finalizeErr.Get(); ok {
		return nil, err
	}
	a.windows[req.Header.Height] = window
	return a.testApp.FinalizeBlock(ctx, req)
}

var errEVMNotServed = errors.New("epochWindowTestApp does not serve EVM reads")

func (*epochWindowTestApp) EvmCall(context.Context, *core.Message) (*core.ExecutionResult, error) {
	return nil, errEVMNotServed
}

func (*epochWindowTestApp) EvmEstimateGas(context.Context, *core.Message, uint64) (uint64, []byte, error) {
	return 0, nil, errEVMNotServed
}

func (*epochWindowTestApp) EvmCallAt(context.Context, *core.Message, *ethtypes.Header) (*core.ExecutionResult, error) {
	return nil, errEVMNotServed
}

func (*epochWindowTestApp) EvmEstimateGasAt(context.Context, *core.Message, uint64, *ethtypes.Header) (uint64, []byte, error) {
	return 0, nil, errEVMNotServed
}

func (*epochWindowTestApp) EvmEstimateGasAfterCalls(context.Context, []*core.Message, *core.Message, uint64) (uint64, []byte, error) {
	return 0, nil, errEVMNotServed
}

func (*epochWindowTestApp) EvmCreateAccessList(context.Context, *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	return nil, nil, errEVMNotServed
}

func (*epochWindowTestApp) EvmChainID() uint64                  { return 0 }
func (*epochWindowTestApp) EvmChainConfig() *params.ChainConfig { return params.TestChainConfig }
func (*epochWindowTestApp) EvmBaseFee() *big.Int                { return new(big.Int) }
func (*epochWindowTestApp) EvmGasLimit() uint64                 { return 0 }
func (*epochWindowTestApp) EvmMinGasPrice() *big.Int            { return new(big.Int) }

func (*epochWindowTestApp) EvmBlockContext(*ethtypes.Header) (vm.BlockContext, error) {
	return vm.BlockContext{}, errEVMNotServed
}

func (*epochWindowTestApp) EvmStateAt(context.Context, int64) (vm.StateDB, func(), error) {
	return nil, func() {}, errEVMNotServed
}

func (*epochWindowTestApp) EvmStateBeforeTx(context.Context, *ethtypes.Header, [][]byte) (vm.StateDB, func(), error) {
	return nil, func() {}, errEVMNotServed
}

var errFinalizeFailed = errors.New("finalize failed")

func TestStartExecuteBlockReturnsEVMFinalizeError(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	app := newEpochWindowTestApp()
	app.finalizeErr = utils.Some(errFinalizeFailed)
	_, _, router := newAppHashTestRouterOver(t, rng, key, app, newAppHashTestBlockStore(t))

	_, err := router.startExecuteBlock(t.Context(), &atypes.SuperBlock{Height: 1, Timestamp: time.Now()})
	require.ErrorIs(t, err, errFinalizeFailed)
}

func TestStartExecuteBlockCommitsEpochWindowWithFirstRoad(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	app := newEpochWindowTestApp()
	_, state, router := newAppHashTestRouterOver(t, rng, key, app, newAppHashTestBlockStore(t))
	_, err := app.InitChain(router.cfg.GenDoc.ToRequestInitChain())
	require.NoError(t, err)
	last := state.Registry().LastRoad(0)

	for _, n := range []atypes.RoadIndex{last, last + 1} {
		_, err := router.startExecuteBlock(t.Context(), &atypes.SuperBlock{Height: n, Timestamp: time.Now()})
		require.NoError(t, err)
	}

	weights := utils.OrPanic1(committeeWeights(app.GetValidators()))
	want := utils.OrPanic1(state.Registry().Window(0, weights))
	require.Equal(t, utils.None[epoch.Window](), app.windows[int64(last)])
	got, ok := app.windows[int64(last+1)].Get()
	require.True(t, ok)
	require.True(t, got.Equal(want))
}
