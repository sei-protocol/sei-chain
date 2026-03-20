package keeper_test

import (
	"math/big"
	"testing"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"

	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

var (
	PKs = seiapp.CreateTestPubKeys(500)
)

func init() {
	sdk.DefaultPowerReduction = sdk.NewIntFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
}

// createTestInput Returns a simapp with custom StakingKeeper
// to avoid messing with the hooks.
func createTestInput(t *testing.T) (*codec.LegacyAmino, *seiapp.App, sdk.Context) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	app.StakingKeeper = keeper.NewKeeper(
		app.AppCodec(),
		app.GetKey(types.StoreKey),
		app.AccountKeeper,
		app.BankKeeper,
		app.GetSubspace(types.ModuleName),
	)
	return app.LegacyAmino(), app, ctx
}

// intended to be used with require/assert:  require.True(ValEq(...))
func ValEq(t *testing.T, exp, got types.Validator) (*testing.T, bool, string, types.Validator, types.Validator) {
	return t, exp.MinEqual(&got), "expected:\n%v\ngot:\n%v", exp, got
}

// generateAddresses generates numAddrs of normal AccAddrs and ValAddrs
func generateAddresses(app *seiapp.App, ctx sdk.Context, numAddrs int) ([]sdk.AccAddress, []sdk.ValAddress) {
	addrDels := seiapp.AddTestAddrsIncremental(app, ctx, numAddrs, sdk.NewInt(10000))
	addrVals := seiapp.ConvertAddrsToValAddrs(addrDels)

	return addrDels, addrVals
}
