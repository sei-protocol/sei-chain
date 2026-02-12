package upgrade_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
)

type TestSuite struct {
	keeper  keeper.Keeper
	querier sdk.Querier
	handler govtypes.Handler
	ctx     sdk.Context
}

const minorUpgradeInfo = `{"upgradeType":"minor"}`

var s TestSuite

func setupTest(t *testing.T, height int64, skip map[int64]bool) TestSuite {
	db := dbm.NewMemDB()
	app := seiapp.SetupWithDB(t, db, false, false, false)
	genesisState := seiapp.NewDefaultGenesisState(app.AppCodec())
	stateBytes, err := json.MarshalIndent(genesisState, "", "  ")
	if err != nil {
		panic(err)
	}
	app.InitChain(
		context.Background(), &abci.RequestInitChain{
			Validators:    []abci.ValidatorUpdate{},
			AppStateBytes: stateBytes,
		},
	)

	s.keeper = app.UpgradeKeeper
	s.ctx = app.BaseApp.NewContext(false, tmproto.Header{Height: height, Time: time.Now()})

	s.querier = upgrade.NewAppModule(s.keeper).LegacyQuerierHandler(app.LegacyAmino())
	s.handler = upgrade.NewSoftwareUpgradeProposalHandler(s.keeper)
	return s
}

func TestRequireName(t *testing.T) {
	s := setupTest(t, 10, map[int64]bool{})

	err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{}})
	require.Error(t, err)
	require.True(t, errors.Is(sdkerrors.ErrInvalidRequest, err), err)
}

func TestRequireFutureBlock(t *testing.T) {
	s := setupTest(t, 10, map[int64]bool{})
	err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{Name: "test", Height: s.ctx.BlockHeight() - 1}})
	require.Error(t, err)
	require.True(t, errors.Is(sdkerrors.ErrInvalidRequest, err), err)
}

func TestDoHeightUpgrade(t *testing.T) {
	s := setupTest(t, 10, map[int64]bool{})
	t.Log("Verify can schedule an upgrade")
	err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{Name: "test", Height: s.ctx.BlockHeight() + 1}})
	require.NoError(t, err)

	VerifyDoUpgrade(t)
}

func TestCanOverwriteScheduleUpgrade(t *testing.T) {
	s := setupTest(t, 10, map[int64]bool{})
	t.Log("Can overwrite plan")
	err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{Name: "bad_test", Height: s.ctx.BlockHeight() + 10}})
	require.NoError(t, err)
	err = s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{Name: "test", Height: s.ctx.BlockHeight() + 1}})
	require.NoError(t, err)

	VerifyDoUpgrade(t)
}

func VerifyDoUpgrade(t *testing.T) {
	t.Log("Verify that a panic happens at the upgrade height")
	newCtx := s.ctx.WithBlockHeight(s.ctx.BlockHeight() + 1).WithBlockTime(time.Now())

	require.Panics(t, func() {
		upgrade.BeginBlocker(s.keeper, newCtx)
	})

	t.Log("Verify that the upgrade can be successfully applied with a handler")
	s.keeper.SetUpgradeHandler("test", func(ctx sdk.Context, plan types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		return vm, nil
	})
	require.NotPanics(t, func() {
		upgrade.BeginBlocker(s.keeper, newCtx)
	})

	VerifyCleared(t, newCtx)
}

func VerifyDoUpgradeWithCtx(t *testing.T, newCtx sdk.Context, proposalName string) {
	t.Log("Verify that a panic happens at the upgrade height")
	require.Panics(t, func() {
		upgrade.BeginBlocker(s.keeper, newCtx)
	})

	t.Log("Verify that the upgrade can be successfully applied with a handler")
	s.keeper.SetUpgradeHandler(proposalName, func(ctx sdk.Context, plan types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		return vm, nil
	})
	require.NotPanics(t, func() {
		upgrade.BeginBlocker(s.keeper, newCtx)
	})

	VerifyCleared(t, newCtx)
}

func TestHaltIfTooNew(t *testing.T) {
	s := setupTest(t, 10, map[int64]bool{})
	t.Log("Verify that we don't panic with registered plan not in database at all")
	var called int
	s.keeper.SetUpgradeHandler("future", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
		called++
		return vm, nil
	})

	newCtx := s.ctx.WithBlockHeight(s.ctx.BlockHeight() + 1).WithBlockTime(time.Now())
	require.NotPanics(t, func() {
		upgrade.BeginBlocker(s.keeper, newCtx)
	})
	require.Equal(t, 0, called)

	t.Log("Verify we panic if we have a registered handler ahead of time")
	err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{Name: "future", Height: s.ctx.BlockHeight() + 3}})
	require.NoError(t, err)
	require.Panics(t, func() {
		upgrade.BeginBlocker(s.keeper, newCtx)
	})
	require.Equal(t, 0, called)

	t.Log("Verify we no longer panic if the plan is on time")

	futCtx := s.ctx.WithBlockHeight(s.ctx.BlockHeight() + 3).WithBlockTime(time.Now())
	require.NotPanics(t, func() {
		upgrade.BeginBlocker(s.keeper, futCtx)
	})
	require.Equal(t, 1, called)

	VerifyCleared(t, futCtx)
}

func VerifyCleared(t *testing.T, newCtx sdk.Context) {
	t.Log("Verify that the upgrade plan has been cleared")
	bz, err := s.querier(newCtx, []string{types.QueryCurrent}, abci.RequestQuery{})
	require.NoError(t, err)
	require.Nil(t, bz)
}

func TestCanClear(t *testing.T) {
	s := setupTest(t, 10, map[int64]bool{})
	t.Log("Verify upgrade is scheduled")
	err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{Name: "test", Height: s.ctx.BlockHeight() + 100}})
	require.NoError(t, err)

	err = s.handler(s.ctx, &types.CancelSoftwareUpgradeProposal{Title: "cancel"})
	require.NoError(t, err)

	VerifyCleared(t, s.ctx)
}

func TestCantApplySameUpgradeTwice(t *testing.T) {
	s := setupTest(t, 10, map[int64]bool{})
	height := s.ctx.BlockHeader().Height + 1
	err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{Name: "test", Height: height}})
	require.NoError(t, err)
	VerifyDoUpgrade(t)
	t.Log("Verify an executed upgrade \"test\" can't be rescheduled")
	err = s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "prop", Plan: types.Plan{Name: "test", Height: height}})
	require.Error(t, err)
	require.True(t, errors.Is(sdkerrors.ErrInvalidRequest, err), err)
}

func TestNoSpuriousUpgrades(t *testing.T) {
	s := setupTest(t, 10, map[int64]bool{})
	t.Log("Verify that no upgrade panic is triggered in the BeginBlocker when we haven't scheduled an upgrade")
	require.NotPanics(t, func() {
		upgrade.BeginBlocker(s.keeper, s.ctx)
	})
}

func TestPlanStringer(t *testing.T) {
	require.Equal(t, `Upgrade Plan
  Name: test
  height: 100
  Info: .`, types.Plan{Name: "test", Height: 100, Info: ""}.String())

	require.Equal(t, fmt.Sprintf(`Upgrade Plan
  Name: test
  height: 100
  Info: .`), types.Plan{Name: "test", Height: 100, Info: ""}.String())
}

func VerifyNotDone(t *testing.T, newCtx sdk.Context, name string) {
	t.Log("Verify that upgrade was not done")
	height := s.keeper.GetDoneHeight(newCtx, name)
	require.Zero(t, height)
}

func VerifyDone(t *testing.T, newCtx sdk.Context, name string) {
	t.Log("Verify that the upgrade plan has been executed")
	height := s.keeper.GetDoneHeight(newCtx, name)
	require.NotZero(t, height)
}

func VerifySet(t *testing.T, skipUpgradeHeights map[int64]bool) {
	t.Log("Verify if the skip upgrade has been set")

	for k := range skipUpgradeHeights {
		require.True(t, s.keeper.IsSkipHeight(k))
	}
}

// TODO: add testcase to for `no upgrade handler is present for last applied upgrade`.
func TestBinaryVersion(t *testing.T) {
	var skipHeight int64 = 15
	s := setupTest(t, 10, map[int64]bool{skipHeight: true})

	testCases := []struct {
		name        string
		preRun      func() (sdk.Context, abci.RequestBeginBlock)
		expectPanic bool
	}{
		{
			"test not panic: no scheduled upgrade or applied upgrade is present",
			func() (sdk.Context, abci.RequestBeginBlock) {
				req := abci.RequestBeginBlock{Header: s.ctx.BlockHeader()}
				return s.ctx, req
			},
			false,
		},
		{
			"test not panic: upgrade handler is present for last applied upgrade",
			func() (sdk.Context, abci.RequestBeginBlock) {
				s.keeper.SetUpgradeHandler("test0", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
					return vm, nil
				})

				err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "Upgrade test", Plan: types.Plan{Name: "test0", Height: s.ctx.BlockHeight() + 2}})
				require.NoError(t, err)

				newCtx := s.ctx.WithBlockHeight(12)
				s.keeper.ApplyUpgrade(newCtx, types.Plan{
					Name:   "test0",
					Height: 12,
				})

				req := abci.RequestBeginBlock{Header: newCtx.BlockHeader()}
				return newCtx, req
			},
			false,
		},
		{
			"test panic: upgrade needed",
			func() (sdk.Context, abci.RequestBeginBlock) {
				err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{Title: "Upgrade test", Plan: types.Plan{Name: "test2", Height: 13}})
				require.NoError(t, err)

				newCtx := s.ctx.WithBlockHeight(13)
				req := abci.RequestBeginBlock{Header: newCtx.BlockHeader()}
				return newCtx, req
			},
			true,
		},
		{
			"test not panic: minor version upgrade in future and not applied",
			func() (sdk.Context, abci.RequestBeginBlock) {
				err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{
					Title: "Upgrade test",
					Plan:  types.Plan{Name: "test2", Height: 105, Info: minorUpgradeInfo},
				})
				require.NoError(t, err)

				newCtx := s.ctx.WithBlockHeight(100)
				req := abci.RequestBeginBlock{Header: newCtx.BlockHeader()}
				return newCtx, req
			},
			false,
		},
		{
			"test panic: minor version upgrade is due",
			func() (sdk.Context, abci.RequestBeginBlock) {
				err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{
					Title: "Upgrade test",
					Plan:  types.Plan{Name: "test2", Height: 13, Info: minorUpgradeInfo},
				})
				require.NoError(t, err)

				newCtx := s.ctx.WithBlockHeight(13)
				req := abci.RequestBeginBlock{Header: newCtx.BlockHeader()}
				return newCtx, req
			},
			true,
		},
		{
			"test not panic: minor upgrade is in future and already applied",
			func() (sdk.Context, abci.RequestBeginBlock) {
				s.keeper.SetUpgradeHandler("test3", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
					return vm, nil
				})

				err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{
					Title: "Upgrade test",
					Plan:  types.Plan{Name: "test3", Height: s.ctx.BlockHeight() + 10, Info: minorUpgradeInfo},
				})
				require.NoError(t, err)

				newCtx := s.ctx.WithBlockHeight(12)
				s.keeper.ApplyUpgrade(newCtx, types.Plan{
					Name:   "test3",
					Height: 12,
				})

				req := abci.RequestBeginBlock{Header: newCtx.BlockHeader()}
				return newCtx, req
			},
			false,
		},
		{
			"test not panic: minor upgrade should apply",
			func() (sdk.Context, abci.RequestBeginBlock) {
				s.keeper.SetUpgradeHandler("test4", func(_ sdk.Context, _ types.Plan, vm module.VersionMap) (module.VersionMap, error) {
					return vm, nil
				})

				err := s.handler(s.ctx, &types.SoftwareUpgradeProposal{
					Title: "Upgrade test",
					Plan:  types.Plan{Name: "test4", Height: s.ctx.BlockHeight() + 10, Info: minorUpgradeInfo},
				})
				require.NoError(t, err)

				newCtx := s.ctx.WithBlockHeight(12)
				req := abci.RequestBeginBlock{Header: newCtx.BlockHeader()}
				return newCtx, req
			},
			false,
		},
	}

	for _, tc := range testCases {
		ctx, _ := tc.preRun()
		if tc.expectPanic {
			require.Panics(t, func() {
				upgrade.BeginBlocker(s.keeper, ctx)
			})
		} else {
			require.NotPanics(t, func() {
				upgrade.BeginBlocker(s.keeper, ctx)
			})
		}
	}
}
