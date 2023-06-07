package app

import (
	"log"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// NOTE: When performing upgrades, make sure to keep / register the handlers
// for both the current (n) and the previous (n-1) upgrade name. There is a bug
// in a missing value in a log statement for which the fix is not released
var upgradesList = []string{
	// 1.x.x versions use open source tendermint
	"1.0.2beta",
	"1.0.3beta",
	"1.0.4beta",
	"1.0.5beta upgrade",
	"1.0.6beta",
	"1.0.7beta",
	"1.0.7beta-postfix",
	"1.0.8beta",
	"1.0.9beta",
	"1.1.0beta",
	"1.1.1beta",
	"1.1.2beta-internal",
	"1.1.3beta",
	"1.1.4beta",
	"1.2.0beta",
	"1.2.1beta",
	"1.2.2beta",
	"1.2.2beta-postfix",
	// 2.x.x versions use forked sei-tendermint and forked sei-cosmos
	"2.0.29beta",
	"2.0.32beta",
	"2.0.36beta",
	"2.0.37beta",
	"2.0.38beta",
	"2.0.39beta",
	"2.0.40beta",
	"2.0.41beta",
	"2.0.42beta",
	"2.0.43beta",
	"2.0.44beta",
	"2.0.45beta",
	"2.0.46beta",
	"2.0.47beta",
	"2.0.48beta",
	// 3.x.x versions have a revamped and optimized dex changes. We also change naming conventions to remove "beta"
	"3.0.0",
	"3.0.1",
	"3.0.2",
	"3.0.3",
}

func (app App) RegisterUpgradeHandlers() {
	// Upgrades names must be in alphabetical order
	// https://github.com/cosmos/cosmos-sdk/issues/11707
	if !sort.StringsAreSorted(upgradesList) {
		log.Fatal("New upgrades must be appended to 'upgradesList' in alphabetical order")
	}
	for _, upgradeName := range upgradesList {
		app.UpgradeKeeper.SetUpgradeHandler(upgradeName, func(ctx sdk.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			// Set params to Distribution here when migrating
			if upgradeName == "1.2.3beta" {
				newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
				if err != nil {
					return newVM, err
				}

				params := app.DistrKeeper.GetParams(ctx)
				params.CommunityTax = sdk.NewDec(0)
				app.DistrKeeper.SetParams(ctx, params)

				return newVM, err
			}

			return app.mm.RunMigrations(ctx, app.configurator, fromVM)
		})
	}
}
