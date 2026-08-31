package app

import (
	"embed"
	"os"
	"strings"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"golang.org/x/mod/semver"
)

//go:embed tags
var f embed.FS

// NOTE: When performing upgrades, make sure to keep / register the handlers
// for both the current (n) and the previous (n-1) upgrade name. There is a bug
// in a missing value in a log statement for which the fix is not released
var upgradesList []string

var LatestUpgrade string

func init() {
	content, err := f.ReadFile("tags")
	if err != nil {
		panic(err)
	}
	upgradesList = parseUpgradesList(string(content))
	LatestUpgrade = upgradesList[len(upgradesList)-1]
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
				app.UpgradeKeeper.DeleteModuleVersion(ctx, capabilityModuleName)
				app.UpgradeKeeper.DeleteModuleVersion(ctx, feegrantModuleName)
				if err := migrateDelegationByValIndex(ctx, app); err != nil {
					return nil, err
				}
				return newVM, nil
			}

			return app.mm.RunMigrations(ctx, app.configurator, fromVM)
		})
	}
}

const v606UpgradeHeight = 151573570

// migrateDelegationByValIndex populates the validator-indexed delegation store and
// marks it ready, so the staking precompile's validatorDelegations can answer from a
// per-validator prefix instead of scanning every delegation.
//
// It runs on an infinite gas meter: the cost is a property of chain size at the
// upgrade height, not of anything a transaction chose to spend.
func migrateDelegationByValIndex(ctx sdk.Context, app *App) error {
	result, err := app.StakingKeeper.MigrateDelegationByValIndex(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)))
	if err != nil {
		return err
	}
	logger.Info(
		"populated delegation-by-validator index",
		"total_delegations", result.TotalDelegations,
		"index_written", result.IndexWritten,
		"already_ready", result.AlreadyReady,
		"elapsed", result.Elapsed.String(),
	)
	return nil
}
