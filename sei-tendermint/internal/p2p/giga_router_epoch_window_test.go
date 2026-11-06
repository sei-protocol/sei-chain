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

// consensusStateTestApp is an EVM app over testApp that records the consensus state
// each block is finalized with. Its EVM read methods return errEVMNotServed.
type consensusStateTestApp struct {
	*testApp
	finalizeErr utils.Option[error]
	states      map[int64]utils.Option[proxy.ConsensusState]
}

var _ proxy.EVMApplication = (*consensusStateTestApp)(nil)

func newConsensusStateTestApp() *consensusStateTestApp {
	return &consensusStateTestApp{testApp: newTestApp(), states: map[int64]utils.Option[proxy.ConsensusState]{}}
}

func (a *consensusStateTestApp) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return a.FinalizeBlockWithConsensusState(ctx, req, utils.None[proxy.ConsensusState]())
}

func (a *consensusStateTestApp) FinalizeBlockWithConsensusState(
	ctx context.Context,
	req *abci.RequestFinalizeBlock,
	state utils.Option[proxy.ConsensusState],
) (*abci.ResponseFinalizeBlock, error) {
	if err, ok := a.finalizeErr.Get(); ok {
		return nil, err
	}
	a.states[req.Header.Height] = state
	return a.testApp.FinalizeBlock(ctx, req)
}

var errEVMNotServed = errors.New("consensusStateTestApp does not serve EVM reads")

func (*consensusStateTestApp) EvmCall(context.Context, *core.Message) (*core.ExecutionResult, error) {
	return nil, errEVMNotServed
}

func (*consensusStateTestApp) EvmEstimateGas(context.Context, *core.Message, uint64) (uint64, []byte, error) {
	return 0, nil, errEVMNotServed
}

func (*consensusStateTestApp) EvmCallAt(context.Context, *core.Message, *ethtypes.Header) (*core.ExecutionResult, error) {
	return nil, errEVMNotServed
}

func (*consensusStateTestApp) EvmEstimateGasAt(context.Context, *core.Message, uint64, *ethtypes.Header) (uint64, []byte, error) {
	return 0, nil, errEVMNotServed
}

func (*consensusStateTestApp) EvmEstimateGasAfterCalls(context.Context, []*core.Message, *core.Message, uint64) (uint64, []byte, error) {
	return 0, nil, errEVMNotServed
}

func (*consensusStateTestApp) EvmCreateAccessList(context.Context, *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	return nil, nil, errEVMNotServed
}

func (*consensusStateTestApp) EvmChainID() uint64                  { return 0 }
func (*consensusStateTestApp) EvmChainConfig() *params.ChainConfig { return params.TestChainConfig }
func (*consensusStateTestApp) EvmBaseFee() *big.Int                { return new(big.Int) }
func (*consensusStateTestApp) EvmGasLimit() uint64                 { return 0 }
func (*consensusStateTestApp) EvmMinGasPrice() *big.Int            { return new(big.Int) }

func (*consensusStateTestApp) EvmBlockContext(*ethtypes.Header) (vm.BlockContext, error) {
	return vm.BlockContext{}, errEVMNotServed
}

func (*consensusStateTestApp) EvmStateAt(context.Context, int64) (vm.StateDB, func(), error) {
	return nil, func() {}, errEVMNotServed
}

func (*consensusStateTestApp) EvmStateBeforeTx(context.Context, *ethtypes.Header, [][]byte) (vm.StateDB, func(), error) {
	return nil, func() {}, errEVMNotServed
}

var errFinalizeFailed = errors.New("finalize failed")

func TestStartExecuteBlockReturnsEVMFinalizeError(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	app := newConsensusStateTestApp()
	app.finalizeErr = utils.Some(errFinalizeFailed)
	_, _, router := newAppHashTestRouterOver(t, rng, key, app, newAppHashTestBlockStore(t))

	_, err := router.startExecuteBlock(t.Context(), &atypes.SuperBlock{Height: 1, Timestamp: time.Now()})
	require.ErrorIs(t, err, errFinalizeFailed)
}

func TestStartExecuteBlockCommitsConsensusStateWithFirstRoad(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	app := newConsensusStateTestApp()
	_, state, router := newAppHashTestRouterOver(t, rng, key, app, newAppHashTestBlockStore(t))
	_, err := app.InitChain(router.cfg.GenDoc.ToRequestInitChain())
	require.NoError(t, err)
	last := state.Registry().LastRoad(0)

	for _, n := range []atypes.RoadIndex{last, last + 1} {
		_, err := router.startExecuteBlock(t.Context(), &atypes.SuperBlock{Height: n, Timestamp: time.Now()})
		require.NoError(t, err)
	}

	weights := utils.OrPanic1(committeeWeights(app.GetValidators()))
	current := utils.OrPanic1(state.Registry().Epoch(1))
	committee := utils.OrPanic1(current.Committee().DeriveNext(weights, 2))
	roads := atypes.RoadRange{First: current.RoadRange().Next, Next: state.Registry().LastRoad(2) + 1}
	want := atypes.NewEpoch(2, roads, committee)
	require.Equal(t, utils.None[proxy.ConsensusState](), app.states[int64(last)])
	got, ok := app.states[int64(last+1)].Get()
	require.True(t, ok)
	require.Empty(t, got.EpochRegistry)
	require.Equal(t, epoch.MarshalEpoch(want), got.NextEpoch)
	require.Equal(t, utils.Some(want), state.Registry().Pending())
}
