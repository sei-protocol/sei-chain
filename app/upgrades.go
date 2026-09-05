package app

import (
	"embed"
	"os"
	"slices"
	"strings"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	storekeys "github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"golang.org/x/mod/semver"
)

//go:embed tags
var f embed.FS

// NOTE: When performing upgrades, make sure to keep / register the handlers
// for both the current (n) and the previous (n-1) upgrade name. There is a bug
// in a missing value in a log statement for which the fix is not released
var upgradesList []string

// releaseUpgrades is the embedded list, kept apart from upgradesList because
// UPGRADE_VERSION_LIST replaces the latter in place and never restores it. A
// caller asking which upgrades this build ships has to be answered from a value
// no test can have already overwritten.
var releaseUpgrades []string

var LatestUpgrade string

func init() {
	content, err := f.ReadFile("tags")
	if err != nil {
		panic(err)
	}
	releaseUpgrades = parseUpgradesList(string(content))
	upgradesList = slices.Clone(releaseUpgrades)
	LatestUpgrade = releaseUpgrades[len(releaseUpgrades)-1]
}

// ReleaseUpgrades returns the upgrade names this build embeds, in semver order,
// the last of which is LatestUpgrade. UPGRADE_VERSION_LIST does not affect it.
func ReleaseUpgrades() []string {
	return slices.Clone(releaseUpgrades)
}

func parseUpgradesList(list string) []string {
	upgrades := strings.FieldsFunc(list, func(r rune) bool {
		return r == '\n' || r == ','
	})
	// Upgrades names must be in alphabetical order
	// https://github.com/cosmos/cosmos-sdk/issues/11707
	semver.Sort(upgrades)
	return upgrades
}

// if there is an override list, use that instead, for integration tests
func overrideList() {
	// if there is an override list, use that instead, for integration tests
	envList := os.Getenv("UPGRADE_VERSION_LIST")
	if envList != "" {
		upgradesList = parseUpgradesList(envList)
	}
}

func (app *App) RegisterUpgradeHandlers() {
	// if there is an override list, use that instead, for integration tests
	overrideList()
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

			if upgradeName == "v6.0.2" {
				newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
				if err != nil {
					return newVM, err
				}

				cp := app.GetConsensusParams(ctx)
				cp.Block.MinTxsInBlock = 10
				app.StoreConsensusParams(ctx, cp)
				return newVM, err
			}

			if upgradeName == "v6.0.5" {
				newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
				if err != nil {
					return newVM, err
				}

				cp := app.GetConsensusParams(ctx)
				cp.Block.MaxGasWanted = 50000000 // 50 mil
				app.StoreConsensusParams(ctx, cp)
				return newVM, err
			}

			if upgradeName == "v6.7" {
				newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
				if err != nil {
					return nil, err
				}
				app.UpgradeKeeper.DeleteModuleVersion(ctx, storekeys.IBCStoreKey)
				app.UpgradeKeeper.DeleteModuleVersion(ctx, capabilityModuleName)
				app.UpgradeKeeper.DeleteModuleVersion(ctx, feegrantModuleName)
				app.UpgradeKeeper.DeleteModuleVersion(ctx, transferModuleName)
				return newVM, nil
			}

			return app.mm.RunMigrations(ctx, app.configurator, fromVM)
		})
	}
}

const v606UpgradeHeight = 151573570
