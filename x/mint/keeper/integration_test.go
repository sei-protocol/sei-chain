package keeper_test

import (
	"github.com/sei-protocol/sei-chain/app"
	tmproto "github.com/sei-protocol/sei-chain/tendermint/proto/tendermint/types"

	sdk "github.com/sei-protocol/sei-chain/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/mint/types"
)

// returns context and an app with updated mint keeper
func createTestApp(isCheckTx bool) (*app.App, sdk.Context) {
	app := app.Setup(isCheckTx, false, false)

	ctx := app.BaseApp.NewContext(isCheckTx, tmproto.Header{})
	app.MintKeeper.SetParams(ctx, types.DefaultParams())
	app.MintKeeper.SetMinter(ctx, types.DefaultInitialMinter())

	return app, ctx
}
