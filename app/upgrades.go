package app

import (
	"embed"
	"os"
	"slices"
	"strings"

	"github.com/sei-protocol/sei-chain/app/retiredoracle"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"golang.org/x/mod/semver"
	"google.golang.org/protobuf/encoding/protowire"
)

// retiredIBCStores are the IBC module stores that v6.7 left mounted for
// historical access.
var retiredIBCStores = []string{"capability", "ibc", "transfer"}

// v68DeletedStores are the KV stores deleted from the multistore at the v6.8
// upgrade height. Their module version map entries go with them.
var v68DeletedStores = append([]string{retiredoracle.ModuleName}, retiredIBCStores...)

// retiredIBCProposalTypeURLPrefix matches governance proposal content types
// whose decoders no longer exist.
const retiredIBCProposalTypeURLPrefix = "/ibc."

// upgradedIBCStateKeyPrefix is the upgrade-store prefix under which the cosmos
// upgrade module recorded planned IBC client state before IBC was retired.
const upgradedIBCStateKeyPrefix = "upgradedIBCState"

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
				app.deleteRetiredModuleVersions(ctx)
				return newVM, nil
			}

			if upgradeName == "v6.8" {
				newVM, err := app.mm.RunMigrations(ctx, app.configurator, fromVM)
				if err != nil {
					return nil, err
				}
				for _, name := range v68DeletedStores {
					app.UpgradeKeeper.DeleteModuleVersion(ctx, name)
				}
				app.UpgradeKeeper.DeleteModuleVersion(ctx, feegrantModuleName)
				app.rewriteRetiredIBCProposals(ctx)
				app.pruneUpgradedIBCState(ctx)
				return newVM, nil
			}

			return app.mm.RunMigrations(ctx, app.configurator, fromVM)
		})
	}
}

// deleteRetiredModuleVersions drops the module version map entries of the
// modules removed in v6.7.
func (app *App) deleteRetiredModuleVersions(ctx sdk.Context) {
	for _, name := range retiredIBCStores {
		app.UpgradeKeeper.DeleteModuleVersion(ctx, name)
	}
	app.UpgradeKeeper.DeleteModuleVersion(ctx, feegrantModuleName)
}

// rewriteRetiredIBCProposals replaces the content of every stored governance
// proposal whose type lives under an IBC protobuf package with a TextProposal
// carrying the original title and description. The proposal record, its
// deposits, votes and tally indexes are left untouched.
func (app *App) rewriteRetiredIBCProposals(ctx sdk.Context) {
	store := ctx.KVStore(app.GetKey(govtypes.StoreKey))
	iterator := sdk.KVStorePrefixIterator(store, govtypes.ProposalsKeyPrefix)
	var retired []govtypes.Proposal
	for ; iterator.Valid(); iterator.Next() {
		var proposal govtypes.Proposal
		if err := proposal.Unmarshal(iterator.Value()); err != nil {
			panic(err)
		}
		if proposal.Content != nil && strings.HasPrefix(proposal.Content.TypeUrl, retiredIBCProposalTypeURLPrefix) {
			retired = append(retired, proposal)
		}
	}
	if err := iterator.Close(); err != nil {
		panic(err)
	}
	for _, proposal := range retired {
		title, description := retiredProposalText(proposal.Content.Value)
		content, err := codectypes.NewAnyWithValue(&govtypes.TextProposal{Title: title, Description: description})
		if err != nil {
			panic(err)
		}
		typeURL := proposal.Content.TypeUrl
		proposal.Content = content
		store.Set(govtypes.ProposalKey(proposal.ProposalId), app.GovKeeper.MustMarshalProposal(proposal))
		logger.Info("rewrote retired IBC governance proposal as a text proposal", "proposal_id", proposal.ProposalId, "type_url", typeURL)
	}
}

// retiredProposalText reads the title (field 1) and description (field 2)
// string fields from an encoded governance proposal content message.
func retiredProposalText(bz []byte) (title, description string) {
	for len(bz) > 0 {
		num, typ, n := protowire.ConsumeTag(bz)
		if n < 0 {
			panic(protowire.ParseError(n))
		}
		bz = bz[n:]
		if typ == protowire.BytesType && (num == 1 || num == 2) {
			value, n := protowire.ConsumeBytes(bz)
			if n < 0 {
				panic(protowire.ParseError(n))
			}
			if num == 1 {
				title = string(value)
			} else {
				description = string(value)
			}
			bz = bz[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, bz)
		if n < 0 {
			panic(protowire.ParseError(n))
		}
		bz = bz[n:]
	}
	return title, description
}

// pruneUpgradedIBCState removes any planned IBC client state the upgrade module recorded.
func (app *App) pruneUpgradedIBCState(ctx sdk.Context) {
	deleteByPrefix(ctx.KVStore(app.GetKey(upgradetypes.StoreKey)), []byte(upgradedIBCStateKeyPrefix))
}

func deleteByPrefix(store sdk.KVStore, prefix []byte) {
	iterator := sdk.KVStorePrefixIterator(store, prefix)
	var keys [][]byte
	for ; iterator.Valid(); iterator.Next() {
		keys = append(keys, iterator.Key())
	}
	if err := iterator.Close(); err != nil {
		panic(err)
	}
	for _, key := range keys {
		store.Delete(key)
	}
}

const v606UpgradeHeight = 151573570
