package keeper_test

import (
	"github.com/sei-protocol/sei-chain/app"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// returns context and app with params set on account keeper
func createTestApp(isCheckTx bool) (*app.App, sdk.Context) {
	app := app.Setup(isCheckTx, false, false)
	ctx := app.BaseApp.NewContext(isCheckTx, tmproto.Header{})
	app.AccountKeeper.SetParams(ctx, authtypes.DefaultParams())

	return app, ctx
}
