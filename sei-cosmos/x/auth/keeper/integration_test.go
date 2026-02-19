package keeper_test

import (
	"github.com/sei-protocol/sei-chain/app"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// returns context and app with params set on account keeper
func createTestApp(isCheckTx bool) (*app.App, sdk.Context) {
	app := app.SetupWithDefaultHome(isCheckTx, false, false)
	ctx := app.BaseApp.NewContext(isCheckTx, sdk.Header{})
	app.AccountKeeper.SetParams(ctx, authtypes.DefaultParams())

	return app, ctx
}
