package crisis_test

import (
	"testing"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

var (
	testModuleName        = "dummy"
	dummyRouteWhichPasses = types.NewInvarRoute(testModuleName, "which-passes", func(_ sdk.Context) (string, bool) { return "", false })
	dummyRouteWhichFails  = types.NewInvarRoute(testModuleName, "which-fails", func(_ sdk.Context) (string, bool) { return "whoops", true })
)

func createTestApp(t *testing.T) (*seiapp.App, sdk.Context, []sdk.AccAddress) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.NewContext(false, tmproto.Header{})

	constantFee := sdk.NewInt64Coin(sdk.DefaultBondDenom, 10)
	app.CrisisKeeper.SetConstantFee(ctx, constantFee)
	app.StakingKeeper.SetParams(ctx, stakingtypes.DefaultParams())

	app.CrisisKeeper.RegisterRoute(testModuleName, dummyRouteWhichPasses.Route, dummyRouteWhichPasses.Invar)
	app.CrisisKeeper.RegisterRoute(testModuleName, dummyRouteWhichFails.Route, dummyRouteWhichFails.Invar)

	feePool := distrtypes.InitialFeePool()
	feePool.CommunityPool = sdk.NewDecCoinsFromCoins(sdk.NewCoins(constantFee)...)
	app.DistrKeeper.SetFeePool(ctx, feePool)

	addrs := seiapp.AddTestAddrs(app, ctx, 1, sdk.NewInt(10000))

	return app, ctx, addrs
}

// TestHandleMsgVerifyInvariant verifies that all VerifyInvariant calls return an
// error because the feature is currently unsupported.
func TestHandleMsgVerifyInvariant(t *testing.T) {
	app, ctx, addrs := createTestApp(t)
	sender := addrs[0]
	h := crisis.NewHandler(app.CrisisKeeper)

	cases := []struct {
		name  string
		route string
	}{
		{"passing invariant route", dummyRouteWhichPasses.Route},
		{"failing invariant route", dummyRouteWhichFails.Route},
		{"nonexistent route", "route-that-doesnt-exist"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			msg := types.NewMsgVerifyInvariant(sender, testModuleName, tc.route)
			res, err := h(ctx, msg)
			require.Error(t, err)
			require.Nil(t, res)
		})
	}
}

// TestHandleUnknownMsg verifies that unrecognized message types return an error.
func TestHandleUnknownMsg(t *testing.T) {
	app, ctx, _ := createTestApp(t)
	h := crisis.NewHandler(app.CrisisKeeper)

	res, err := h(ctx, nil)
	require.Error(t, err)
	require.Nil(t, res)
}
