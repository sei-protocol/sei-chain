package keeper_test

import (
	"github.com/sei-protocol/sei-chain/app"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

// returns context and app with params set on account keeper
func createTestApp(isCheckTx bool) (*app.App, sdk.Context) {
	app := app.SetupWithDefaultHome(isCheckTx, false, false)
	ctx := app.BaseApp.NewContext(isCheckTx, tmproto.Header{})
	app.AccountKeeper.SetParams(ctx, authtypes.DefaultParams())

	return app, ctx
}
