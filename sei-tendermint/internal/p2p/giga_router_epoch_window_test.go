package p2p

import (
	"context"
	"errors"
	"testing"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// epochWindowTestApp is an EVM app over testApp that records the epoch window
// each block is finalized with.
type epochWindowTestApp struct {
	// The router does not call the EVM read methods, so they stay nil.
	proxy.EVMApplication
	app         *testApp
	finalizeErr error
	windows     map[int64]utils.Option[epoch.Window]
}

func newEpochWindowTestApp() *epochWindowTestApp {
	return &epochWindowTestApp{app: newTestApp(), windows: map[int64]utils.Option[epoch.Window]{}}
}

func (a *epochWindowTestApp) Info() *abci.ResponseInfo { return a.app.Info() }

func (a *epochWindowTestApp) GetValidators() []abci.ValidatorUpdate { return a.app.GetValidators() }

func (a *epochWindowTestApp) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return a.app.InitChain(req)
}

func (a *epochWindowTestApp) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return a.FinalizeBlockWithEpochWindow(ctx, req, utils.None[epoch.Window]())
}

func (a *epochWindowTestApp) FinalizeBlockWithEpochWindow(
	ctx context.Context,
	req *abci.RequestFinalizeBlock,
	window utils.Option[epoch.Window],
) (*abci.ResponseFinalizeBlock, error) {
	if a.finalizeErr != nil {
		return nil, a.finalizeErr
	}
	a.windows[req.Header.Height] = window
	return a.app.FinalizeBlock(ctx, req)
}

func (a *epochWindowTestApp) Commit(ctx context.Context) (*abci.ResponseCommit, error) {
	return a.app.Commit(ctx)
}

var errFinalizeFailed = errors.New("finalize failed")

func TestStartExecuteBlockReturnsEVMFinalizeError(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	app := newEpochWindowTestApp()
	app.finalizeErr = errFinalizeFailed
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
