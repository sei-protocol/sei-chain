package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

const UPGRADE_1_0_1_BETA = "upgrade-1.0.1beta"

func (app App) RegisterUpgradeHandlers() {
	app.UpgradeKeeper.SetUpgradeHandler(UPGRADE_1_0_1_BETA, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		// Upgrade specific logic goes here
		// For now, remove dex, epoch and oracle from the version map since
		// they do not yet have upgrade logic
		delete(fromVM, "dex")
		delete(fromVM, "epoch")
		delete(fromVM, "oracle")
		return app.mm.RunMigrations(ctx, app.configurator, fromVM)
	})
}
