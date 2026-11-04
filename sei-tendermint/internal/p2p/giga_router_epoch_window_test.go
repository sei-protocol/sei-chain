package p2p

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// epochWindowTestApp is an EVM app over testApp that records the epoch window
// each block is finalized with and serves a fixed committed window.
type epochWindowTestApp struct {
	// The router does not call the EVM read methods, so they stay nil.
	proxy.EVMApplication
	app         *testApp
	finalizeErr error
	windows     map[int64]utils.Option[epoch.Window]
	committed   utils.Option[epoch.Window]
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

func (a *epochWindowTestApp) CommittedEpochWindow() (utils.Option[epoch.Window], error) {
	return a.committed, nil
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

func TestBackfillAppHashesStagesFromCommittedEpochWindow(t *testing.T) {
	rng := utils.TestRng()
	key := atypes.GenSecretKey(rng)
	genesis := utils.OrPanic1(atypes.NewCommittee(map[atypes.PublicKey]uint64{key.Public(): 1}))
	genesisRegistry := utils.OrPanic1(epoch.NewRegistry(genesis, 1, time.Time{}, utils.None[string]()))
	qc, blocks := data.TestCommitQCAtRoad(genesisRegistry.MustEpoch(0), []atypes.SecretKey{key}, genesisRegistry.LastRoad(0))
	db := newAppHashTestBlockStore(t)
	require.NoError(t, db.WriteBlock(atypes.NewRoadBlock(qc, blocks)))
	require.NoError(t, db.Flush())

	app := newEpochWindowTestApp()
	hashBuilder, state, router := newAppHashTestRouterOver(t, rng, key, app, db)
	_, err := app.InitChain(router.cfg.GenDoc.ToRequestInitChain())
	require.NoError(t, err)
	window := utils.OrPanic1(state.Registry().Window(0, map[atypes.PublicKey]uint64{key.Public(): 5}))
	app.committed = utils.Some(window)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("persist", func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		n := qc.Index()
		if got := state.NextAppProposal(); got != n {
			return fmt.Errorf("NextAppProposal() = %v, want %v", got, n)
		}
		hashBuilder.add(testAppHashData(n))
		return router.backfillAppHashes(ctx, n+1)
	}))
	require.Equal(t, utils.Some(window.Next()), state.Registry().Pending())
}
