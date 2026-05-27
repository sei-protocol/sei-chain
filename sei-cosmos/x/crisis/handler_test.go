package crisis_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis"
	crisiskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/types"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

var (
	testModuleName        = "dummy"
	dummyRouteWhichPasses = types.NewInvarRoute(testModuleName, "which-passes", func(_ sdk.Context) (string, bool) { return "", false })
	dummyRouteWhichFails  = types.NewInvarRoute(testModuleName, "which-fails", func(_ sdk.Context) (string, bool) { return "whoops", true })
)

type noopSupplyKeeper struct{}

func (noopSupplyKeeper) SendCoinsFromAccountToModule(sdk.Context, sdk.AccAddress, string, sdk.Coins) error {
	return nil
}

func createTestKeeper() (crisiskeeper.Keeper, sdk.Context) {
	legacyAmino := codec.NewLegacyAmino()
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	key := sdk.NewKVStoreKey(paramstypes.StoreKey)
	tkey := sdk.NewTransientStoreKey(paramstypes.TStoreKey)
	paramsKeeper := paramskeeper.NewKeeper(cdc, legacyAmino, key, tkey)

	keeper := crisiskeeper.NewKeeper(paramsKeeper.Subspace(types.ModuleName), 1, noopSupplyKeeper{}, "fee_collector")
	keeper.RegisterRoute(testModuleName, dummyRouteWhichPasses.Route, dummyRouteWhichPasses.Invar)
	keeper.RegisterRoute(testModuleName, dummyRouteWhichFails.Route, dummyRouteWhichFails.Invar)

	return keeper, sdk.NewContext(nil, tmproto.Header{}, false)
}

// TestHandleMsgVerifyInvariant verifies that all VerifyInvariant calls return an
// error because the feature is currently unsupported.
func TestHandleMsgVerifyInvariant(t *testing.T) {
	keeper, ctx := createTestKeeper()
	h := crisis.NewHandler(keeper)

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
			msg := &types.MsgVerifyInvariant{
				Sender:              "sei1sender",
				InvariantModuleName: testModuleName,
				InvariantRoute:      tc.route,
			}
			res, err := h(ctx, msg)
			require.Error(t, err)
			require.Nil(t, res)
		})
	}
}

// TestHandleUnknownMsg verifies that unrecognized message types return an error.
func TestHandleUnknownMsg(t *testing.T) {
	keeper, ctx := createTestKeeper()
	h := crisis.NewHandler(keeper)

	res, err := h(ctx, nil)
	require.Error(t, err)
	require.Nil(t, res)
}
