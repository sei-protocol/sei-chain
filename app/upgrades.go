package app

import (
	"log"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// UpgradeNameOracleModule - this will introduce the oracle module as well
var UpgradeNameOracleModule = "1.0.4beta"

// NOTE: When performing upgrades, make sure to keep / register the handlers
// for both the current (n) and the previous (n-1) upgrade name. There is a bug
// in a missing value in a log statement for which the fix is not released
var upgradesList = []string{
	// 1.0.2beta upgrades
	"1.0.2beta",
	"1.0.2beta-commit-timeout",
	// 1.0.3beta
	"1.0.3beta",
	// 1.0.4beta
	UpgradeNameOracleModule,
	// 1.0.5beta
	"1.0.5beta upgrade",
	// 1.0.6beta
	"1.0.6beta",
	// 1.0.7beta
	"1.0.7beta",
	// 1.0.7beta-postfix
	"1.0.7beta-postfix",
}

func (app App) RegisterUpgradeHandlers() {
	// Upgrades names must be in alphabetical order
	// https://github.com/cosmos/cosmos-sdk/issues/11707
	if !sort.StringsAreSorted(upgradesList) {
		log.Fatal("New upgrades must be appended to 'upgradesList' in alphabetical order")
	}
	for _, upgrade := range upgradesList {
		app.UpgradeKeeper.SetUpgradeHandler(upgrade, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			return app.mm.RunMigrations(ctx, app.configurator, fromVM)
		})
	}
}
