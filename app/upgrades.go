package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// Set this to the name of your upgrade
const UpgradeName = "test1"

func (app App) RegisterUpgradeHandlers() {
	app.UpgradeKeeper.SetUpgradeHandler(UpgradeName, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		// Upgrade specific logic goes here
		delete(fromVM, "dex")
		delete(fromVM, "epoch")
		return app.mm.RunMigrations(ctx, app.configurator, fromVM)
	})
}
