package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// NOTE: When performing upgrades, make sure to keep / register the handlers
// for both the current (n) and the previous (n-1) upgrade name. There is a bug
// in a missing value in a log statement for which the fix is not released
const UpgradeNameOracleModule = "create-oracle-mod"
const UpgradeOracleStaleIndicator = "upgrade-oracle-stale-indicator"
const UpgradeIgniteCliRemoval = "ignite-cli-removal-upgrade"

// 1.0.2beta upgrades
const Upgrade102 = "1.0.2beta"
const Upgrade102CommitTimeout = "1.0.2beta-commit-timeout"

// 1.0.3beta
const Upgrade103 = "1.0.3beta"

func (app App) RegisterUpgradeHandlers() {
	app.UpgradeKeeper.SetUpgradeHandler(UpgradeNameOracleModule, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return app.mm.RunMigrations(ctx, app.configurator, fromVM)
	})
	app.UpgradeKeeper.SetUpgradeHandler(UpgradeOracleStaleIndicator, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return app.mm.RunMigrations(ctx, app.configurator, fromVM)
	})
	app.UpgradeKeeper.SetUpgradeHandler(UpgradeIgniteCliRemoval, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return app.mm.RunMigrations(ctx, app.configurator, fromVM)
	})
	app.UpgradeKeeper.SetUpgradeHandler(Upgrade102, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return app.mm.RunMigrations(ctx, app.configurator, fromVM)
	})
	app.UpgradeKeeper.SetUpgradeHandler(Upgrade102CommitTimeout, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return app.mm.RunMigrations(ctx, app.configurator, fromVM)
	})
	app.UpgradeKeeper.SetUpgradeHandler(Upgrade102CommitTimeout, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return app.mm.RunMigrations(ctx, app.configurator, fromVM)
	})
	app.UpgradeKeeper.SetUpgradeHandler(Upgrade103, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return app.mm.RunMigrations(ctx, app.configurator, fromVM)
	})
}
