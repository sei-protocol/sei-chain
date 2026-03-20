package staking_test

import (
	"math/big"
	"testing"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"

	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func init() {
	sdk.DefaultPowerReduction = sdk.NewIntFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
}

// nolint:deadcode,unused,varcheck
var (
	priv1 = secp256k1.GenPrivKey()
	addr1 = sdk.AccAddress(priv1.PubKey().Address())
	priv2 = secp256k1.GenPrivKey()
	addr2 = sdk.AccAddress(priv2.PubKey().Address())

	valKey  = ed25519.GenPrivKey()
	valAddr = sdk.AccAddress(valKey.PubKey().Address())

	commissionRates = types.NewCommissionRates(sdk.NewDecWithPrec(5, 2), sdk.NewDecWithPrec(5, 2), sdk.ZeroDec())

	PKs = seiapp.CreateTestPubKeys(500)
)

// getBaseSimappWithCustomKeeper Returns a simapp with custom StakingKeeper
// to avoid messing with the hooks.
func getBaseSimappWithCustomKeeper(t *testing.T) (*codec.LegacyAmino, *seiapp.App, sdk.Context) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	appCodec := app.AppCodec()

	app.StakingKeeper = keeper.NewKeeper(
		appCodec,
		app.GetKey(types.StoreKey),
		app.AccountKeeper,
		app.BankKeeper,
		app.GetSubspace(types.ModuleName),
	)
	app.StakingKeeper.SetParams(ctx, types.DefaultParams())

	return codec.NewLegacyAmino(), app, ctx
}

// generateAddresses generates numAddrs of normal AccAddrs and ValAddrs
func generateAddresses(app *seiapp.App, ctx sdk.Context, numAddrs int, accAmount sdk.Int) ([]sdk.AccAddress, []sdk.ValAddress) {
	addrDels := seiapp.AddTestAddrsIncremental(app, ctx, numAddrs, accAmount)
	addrVals := seiapp.ConvertAddrsToValAddrs(addrDels)

	return addrDels, addrVals
}
