package tests

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/app"
)

func mockUpgrade(version string, height int64) func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		a.UpgradeKeeper.SetDone(ctx.WithBlockHeight(height), version)
	}
}
