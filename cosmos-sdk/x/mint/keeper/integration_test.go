package keeper_test

import (
	tmproto "github.com/sei-protocol/sei-chain/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/simapp"
	sdk "github.com/sei-protocol/sei-chain/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/x/mint/types"
)

// returns context and an app with updated mint keeper
func createTestApp(isCheckTx bool) (*simapp.SimApp, sdk.Context) {
	app := simapp.Setup(isCheckTx)

	ctx := app.BaseApp.NewContext(isCheckTx, tmproto.Header{})
	app.MintKeeper.SetParams(ctx, types.DefaultParams())
	app.MintKeeper.SetMinter(ctx, types.DefaultInitialMinter())

	return app, ctx
}
