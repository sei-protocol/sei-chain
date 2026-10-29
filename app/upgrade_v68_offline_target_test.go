//go:build upgrade_v68 && offline_upgrade && upgrade_target

package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/migration"
	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/address"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	xauthsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

const v68OfflineUpgradeName = "v6.8"

const (
	v68OfflineAuthVersionAfter   uint64 = 4
	v68OfflineWasmVersionAfter   uint64 = 2
	v68OfflinePostUpgradeFee     int64  = 200000
	v68OfflineLegacyVestingTypes        = "/cosmos.vesting.v1beta1."
)

// v68OfflineRemovedModules are the module version map entries v6.8 deletes.
var v68OfflineRemovedModules = []string{"capability", "feegrant", "ibc", "oracle", "tokenfactory", "transfer", "vesting"}

// v68OfflineDeletedStores are the stores v6.8 deletes from the multistore.
var v68OfflineDeletedStores = []string{"capability", "feegrant", "ibc", "oracle", "tokenfactory", "transfer"}
var v68OfflineUpgradeBlockTime = time.Unix(1_700_000_000, 0).UTC()

func TestV68OfflineUpgradeTarget(t *testing.T) {
	t.Run("fixture", testV68OfflineUpgradeTargetFixture)
	t.Run("snapshot", testV68OfflineUpgradeTargetSnapshot)
}

func testV68OfflineUpgradeTargetFixture(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, v68OfflineUpgradeName, artifact.Upgrade)
	require.NotEmpty(t, artifact.VestingAccounts, "the source phase recorded no vesting accounts")
	require.NotEmpty(t, artifact.Retained.TxSender)
	require.NotEmpty(t, artifact.Retained.TxSenderKey)
	require.NotEmpty(t, artifact.Retained.TxRecipient)
	require.Equal(t, artifact.SourceHeight+1, artifact.UpgradeHeight)

	t.Setenv("UPGRADE_VERSION_LIST", LatestUpgrade)

	cleanRoot := filepath.Join(root, offlineUpgradeMigratedDir)
	crashRoot := filepath.Join(root, "crash")
	noUpgradeInfoRoot := filepath.Join(root, "no-upgrade-info")
	copyOfflineUpgradeDatabase(t, root, cleanRoot)
	copyOfflineUpgradeDatabase(t, root, crashRoot)
	copyOfflineUpgradeDatabase(t, root, noUpgradeInfoRoot)

	requireV68OfflineRefusesRetainedStores(t, noUpgradeInfoRoot, artifact)

	cleanHash := applyV68OfflineUpgradeClean(t, cleanRoot, artifact)
	crashHash := applyV68OfflineUpgradeCrashReplay(t, crashRoot, artifact)
	require.Equalf(t, cleanHash, crashHash,
		"crash-replay application hash diverged from the clean single-pass hash: clean=%x crash-replay=%x",
		cleanHash, crashHash)
	recoveredHash := applyV68OfflineUpgradeAfterRefusal(t, noUpgradeInfoRoot, artifact)
	require.Equalf(t, cleanHash, recoveredHash,
		"the upgrade after restoring upgrade-info.json diverged from the clean hash: clean=%x recovered=%x",
		cleanHash, recoveredHash)

	artifact.MigratedRoot = offlineUpgradeMigratedDir
	artifact.UpgradeHash = offlineUpgradeHashString(cleanHash)
	writeOfflineUpgradeArtifact(t, root, artifact)

	txRoot := filepath.Join(root, "post-upgrade-tx")
	copyOfflineUpgradeDatabase(t, cleanRoot, txRoot)
	requireV68OfflineLockedBalanceSpend(t, txRoot, artifact.Retained)
}

func applyV68OfflineUpgradeClean(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV68OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, testApp),
		"v6.8 did not reopen the v6.7 module version map")
	requireV68OfflineLegacyAccountsStored(t, testApp, artifact)

	finalizeV68OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV68OfflineMigrated(t, reopened, artifact)
	hash := committedOfflineUpgradeHash(t, reopened)
	closeOfflineUpgradeApp(t, reopened)
	requireV68OfflineTreesDeleted(t, root)
	return hash
}

func applyV68OfflineUpgradeCrashReplay(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV68OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.SourceHeight, testApp.LastBlockHeight())
	finalizeV68OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	closeOfflineUpgradeApp(t, testApp)

	interrupted := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.SourceHeight, interrupted.LastBlockHeight(),
		"closing without commit left the crash-replay database above the pre-upgrade height")
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, interrupted),
		"closing without commit mutated the pre-upgrade version map")
	requireV68OfflineLegacyAccountsStored(t, interrupted, artifact)
	finalizeV68OfflineUpgrade(t, interrupted, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, interrupted)
	closeOfflineUpgradeApp(t, interrupted)

	reopened := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV68OfflineMigrated(t, reopened, artifact)
	hash := committedOfflineUpgradeHash(t, reopened)
	closeOfflineUpgradeApp(t, reopened)
	requireV68OfflineTreesDeleted(t, root)
	return hash
}

// requireV68OfflineMigrated checks a reopened migrated database: the upgrade
// is recorded, the retired module versions and stores are gone, the vesting
// accounts are rewritten, the retired IBC proposal reads back as a text
// proposal, the upgraded IBC client record, the denom allowlists and the
// tokenfactory parameters are pruned, the wasm flag parameters are stored,
// MigrateCosmos is enabled, the state sync trust period is stored, the IBC
// voucher balance and supply are intact and spendable, every delegation is
// indexed by validator, and nothing else in the retained stores changed.
func requireV68OfflineMigrated(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	requireV68OfflineAppliedName(t, testApp, artifact)
	requireV68OfflineVersionMap(t, testApp, artifact.ModuleVersions)
	requireV68OfflineStoresDeleted(t, testApp)
	requireV68OfflineAccountsRewritten(t, testApp, artifact)
	requireV68OfflineProposalRewritten(t, testApp, artifact.Retained)
	requireV68OfflineUpgradedIBCStatePruned(t, testApp, artifact.Retained)
	requireV68OfflineDenomAllowListsPruned(t, testApp, artifact.Stores[banktypes.StoreKey])
	requireV68OfflineTokenfactoryParamsPruned(t, testApp, artifact.Stores[paramstypes.StoreKey])
	requireV68OfflineWasmFlagsStored(t, testApp, artifact.Stores[paramstypes.StoreKey])
	requireV68OfflineMigrateCosmosEnabled(t, testApp, artifact.Stores[paramstypes.StoreKey])
	requireV68OfflineStateSyncTrustPeriodStored(t, testApp, artifact.Stores[paramstypes.StoreKey])
	requireV68OfflineVoucher(t, testApp, artifact.Retained)
	requireV68OfflineDelegationIndex(t, testApp)
	requireOfflineUpgradeRetainedStoresExcept(t, testApp, artifact.Stores, v68OfflineTouchedKey(t, artifact))
}

// v68OfflineTouchedKey reports the retained-store keys the v6.8 upgrade block
// is specified to change: the vesting accounts the auth migration rewrites,
// the rewritten IBC proposal, the pruned upgraded IBC state, the plan, done
// and version-map entries of the upgrade store, the pruned denom allowlists
// and tokenfactory parameters, and the bank entries block rewards move every
// block.
func v68OfflineTouchedKey(t *testing.T, artifact offlineUpgradeArtifact) func(storeName string, key []byte) bool {
	t.Helper()
	rewritten := make(map[string]struct{}, len(artifact.VestingAccounts))
	for _, recorded := range artifact.VestingAccounts {
		rewritten[string(authtypes.AddressStoreKey(sdk.MustAccAddressFromBech32(recorded.Address)))] = struct{}{}
	}
	proposalKey := string(govtypes.ProposalKey(artifact.Retained.IBCProposalID))
	return func(storeName string, key []byte) bool {
		switch storeName {
		case authtypes.StoreKey:
			_, ok := rewritten[string(key)]
			return ok
		case govtypes.StoreKey:
			return string(key) == proposalKey
		case upgradetypes.StoreKey:
			if strings.HasPrefix(string(key), upgradedIBCStateKeyPrefix) {
				return true
			}
			return len(key) > 0 && (key[0] == upgradetypes.PlanByte || key[0] == upgradetypes.DoneByte || key[0] == upgradetypes.VersionMapByte)
		case banktypes.StoreKey:
			return v68OfflineBlockRewardKey(key) || bytes.HasPrefix(key, banktypes.DenomAllowListPrefix)
		case paramstypes.StoreKey:
			return bytes.HasPrefix(key, tokenfactoryParamsPrefix)
		}
		return false
	}
}

// v68OfflineBlockRewardKey reports bank keys that minting and distribution
// rewrite on every block: the usei supply and the balances of the module
// accounts rewards flow through.
func v68OfflineBlockRewardKey(key []byte) bool {
	if bytes.HasPrefix(key, banktypes.SupplyKey) {
		return string(key[len(banktypes.SupplyKey):]) == sdk.DefaultBondDenom
	}
	for _, prefix := range [][]byte{banktypes.BalancesPrefix, banktypes.WeiBalancesPrefix} {
		if !bytes.HasPrefix(key, prefix) {
			continue
		}
		for _, name := range []string{authtypes.FeeCollectorName, distrtypes.ModuleName, minttypes.ModuleName} {
			if bytes.HasPrefix(key[len(prefix):], address.MustLengthPrefix(authtypes.NewModuleAddress(name))) {
				return true
			}
		}
	}
	return false
}

func requireV68OfflinePersistedPlanHasHandler(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found, "committed upgrade plan did not survive the process boundary")
	require.Equal(t, artifact.Upgrade, plan.Name)
	require.Equal(t, artifact.UpgradeHeight, plan.Height)
	require.Equal(t, LatestUpgrade, plan.Name)
	require.True(t, testApp.UpgradeKeeper.HasHandler(plan.Name),
		"target binary has no handler for persisted plan name %q", plan.Name)
}

func requireV68OfflineAppliedName(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(
		offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()))
	require.Equal(t, artifact.Upgrade, lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.True(t, testApp.UpgradeKeeper.HasHandler(lastName),
		"target binary has no handler for applied plan name %q", lastName)
}

func finalizeV68OfflineUpgrade(t *testing.T, testApp *App, height int64) {
	t.Helper()
	_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID, Height: height, Time: v68OfflineUpgradeBlockTime,
		},
	})
	require.NoError(t, err)
}

// requireV68OfflineRefusesRetainedStores requires the v6.8 handler to halt a
// node that reaches the upgrade height without upgrade-info.json, which leaves
// the retired stores undeleted.
func requireV68OfflineRefusesRetainedStores(t *testing.T, root string, artifact offlineUpgradeArtifact) {
	t.Helper()
	require.NoError(t, os.Remove(filepath.Join(root, "home", "data", "upgrade-info.json")))
	testApp := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, testApp)
	for _, name := range v68OfflineDeletedStores {
		require.True(t, testApp.rootStore.MayHoldStore(name), "%s was deleted without upgrade-info.json", name)
	}

	var refusal string
	func() {
		defer func() {
			if r := recover(); r != nil {
				refusal = fmt.Sprint(r)
			}
		}()
		_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Hash: []byte("offline-upgrade"),
			Header: &tmproto.Header{
				ChainID: offlineUpgradeChainID, Height: artifact.UpgradeHeight, Time: v68OfflineUpgradeBlockTime,
			},
		})
		if err != nil {
			refusal = err.Error()
		}
	}()
	require.Contains(t, refusal, fmt.Sprintf("store %q was not deleted before the v6.8 upgrade handler ran", v68DeletedStores[0]))
	require.Contains(t, refusal, v68OfflineUpgradeInfo(artifact.UpgradeHeight))
}

// applyV68OfflineUpgradeAfterRefusal writes upgrade-info.json as the refusal
// tells the operator to, then applies v6.8 and returns the application hash.
func applyV68OfflineUpgradeAfterRefusal(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	target := filepath.Join(root, "home", "data", "upgrade-info.json")
	require.NoError(t, os.WriteFile(target, []byte(v68OfflineUpgradeInfo(artifact.UpgradeHeight)), 0o600))
	return applyV68OfflineUpgradeClean(t, root, artifact)
}

// v68OfflineUpgradeInfo is the upgrade-info.json content naming v6.8 at height.
func v68OfflineUpgradeInfo(height int64) string {
	return fmt.Sprintf(`{"name":%q,"height":%d}`, v68OfflineUpgradeName, height)
}

// requireV68OfflineLegacyAccountsStored requires every recorded vesting account
// to be committed under its vesting type, which the v6.8 binary reads only as
// bytes.
func requireV68OfflineLegacyAccountsStored(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	store := testApp.CommitMultiStore().GetCommitKVStore(testApp.GetKey(authtypes.StoreKey))
	for _, recorded := range artifact.VestingAccounts {
		encoded := store.Get(authtypes.AddressStoreKey(sdk.MustAccAddressFromBech32(recorded.Address)))
		require.Equal(t, recorded.TypeURL, string(v68OfflineTypeURL(encoded)),
			"the source phase did not commit %s under its vesting type", recorded.Address)
	}
}

func requireV68OfflineVersionMap(t *testing.T, testApp *App, before []string) {
	t.Helper()
	after := offlineUpgradeModuleVersions(t, testApp)
	// v6.7 already dropped the feegrant and IBC module versions, so only oracle,
	// tokenfactory and vesting are expected to leave the map here; the feegrant
	// and IBC entries must not be present.
	removed := offlineUpgradeDifference(before, after)
	require.Subset(t, v68OfflineRemovedModules, removed, "v6.8 removed an unexpected set of module versions")
	require.Contains(t, removed, "oracle")
	require.Contains(t, removed, "tokenfactory")
	require.Contains(t, removed, "vesting")
	require.Empty(t, offlineUpgradeDifference(after, before), "v6.8 added a module version")
	for _, module := range v68OfflineRemovedModules {
		require.False(t, offlineUpgradeHasModuleVersion(t, testApp, module),
			"upgrade store still has a version-map entry for %s", module)
	}
	versions := testApp.UpgradeKeeper.GetModuleVersionMap(offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()))
	require.Equal(t, v68OfflineAuthVersionAfter, versions[authtypes.ModuleName])
	require.Equal(t, v68OfflineWasmVersionAfter, versions[wasmtypes.ModuleName])
}

func requireV68OfflineStoresDeleted(t *testing.T, testApp *App) {
	t.Helper()
	for _, name := range v68OfflineDeletedStores {
		require.Nil(t, testApp.GetKey(name))
	}
	for _, key := range testApp.CommitMultiStore().StoreKeys() {
		require.NotContains(t, v68OfflineDeletedStores, key.Name())
	}
}

// requireV68OfflineDelegationIndex requires the delegation-by-validator index
// to be ready and to hold exactly one entry per stored delegation.
func requireV68OfflineDelegationIndex(t *testing.T, testApp *App) {
	t.Helper()
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	require.True(t, testApp.StakingKeeper.DelegationByValIndexReady(ctx))
	store := ctx.KVStore(testApp.GetKey(stakingtypes.StoreKey))

	delegations := map[string]struct{}{}
	iterator := sdk.KVStorePrefixIterator(store, stakingtypes.DelegationKey)
	for ; iterator.Valid(); iterator.Next() {
		delegations[string(iterator.Key())] = struct{}{}
	}
	require.NoError(t, iterator.Close())
	require.NotEmpty(t, delegations, "the source phase stored no delegations")

	indexed := map[string]struct{}{}
	iterator = sdk.KVStorePrefixIterator(store, stakingtypes.DelegationByValIndexKey)
	for ; iterator.Valid(); iterator.Next() {
		indexed[string(stakingtypes.GetDelegationKeyFromValIndexKey(iterator.Key()))] = struct{}{}
	}
	require.NoError(t, iterator.Close())
	require.Equal(t, delegations, indexed, "the index does not hold exactly one entry per delegation")
}

func requireV68OfflineProposalRewritten(t *testing.T, testApp *App, retained offlineUpgradeRetainedState) {
	t.Helper()
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	proposal, found := testApp.GovKeeper.GetProposal(ctx, retained.IBCProposalID)
	require.True(t, found)
	require.Equal(t, govtypes.StatusPassed, proposal.Status)
	require.Equal(t, "/cosmos.gov.v1beta1.TextProposal", proposal.Content.TypeUrl)
	content := proposal.GetContent()
	require.NotNil(t, content)
	require.Equal(t, retained.IBCProposalTitle, content.GetTitle())
	require.Equal(t, retained.IBCProposalDescription, content.GetDescription())
	requireV68OfflineNoRetiredProposals(t, testApp)
}

func requireV68OfflineNoRetiredProposals(t *testing.T, testApp *App) {
	t.Helper()
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	testApp.GovKeeper.IterateProposals(ctx, func(proposal govtypes.Proposal) bool {
		require.NotNil(t, proposal.GetContent(), "proposal %d has undecodable content", proposal.ProposalId)
		require.False(t, strings.HasPrefix(proposal.Content.TypeUrl, retiredIBCProposalTypeURLPrefix))
		return false
	})
}

func requireV68OfflineUpgradedIBCStatePruned(t *testing.T, testApp *App, retained offlineUpgradeRetainedState) {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(retained.UpgradedIBCStateKey)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(key), upgradedIBCStateKeyPrefix))
	store := committedUpgradeStore(t, testApp)
	require.False(t, store.Has(key))
	iterator := sdk.KVStorePrefixIterator(store, []byte(upgradedIBCStateKeyPrefix))
	defer iterator.Close()
	require.False(t, iterator.Valid())
}

// requireV68OfflineDenomAllowListsPruned requires the source phase's bank
// snapshot to hold a denom transfer allowlist and the committed bank store to
// hold none.
func requireV68OfflineDenomAllowListsPruned(t *testing.T, testApp *App, sourceBank map[string]string) {
	t.Helper()
	recorded := false
	for encodedKey := range sourceBank {
		key, err := base64.StdEncoding.DecodeString(encodedKey)
		require.NoError(t, err)
		recorded = recorded || bytes.HasPrefix(key, banktypes.DenomAllowListPrefix)
	}
	require.True(t, recorded, "the source phase recorded no denom allowlist")
	requireV68OfflineNoDenomAllowLists(t, testApp)
}

// requireV68OfflineNoDenomAllowLists requires the committed bank store to hold
// no denom transfer allowlist.
func requireV68OfflineNoDenomAllowLists(t *testing.T, testApp *App) {
	t.Helper()
	store := testApp.CommitMultiStore().GetCommitKVStore(testApp.GetKey(banktypes.StoreKey))
	iterator := sdk.KVStorePrefixIterator(store, banktypes.DenomAllowListPrefix)
	defer func() {
		require.NoError(t, iterator.Close())
	}()
	require.False(t, iterator.Valid(), "v6.8 left a denom allowlist in the bank store")
}

// requireV68OfflineTokenfactoryParamsPruned requires the source phase's params
// snapshot to hold a tokenfactory parameter and the committed params store to
// hold none.
func requireV68OfflineTokenfactoryParamsPruned(t *testing.T, testApp *App, sourceParams map[string]string) {
	t.Helper()
	recorded := false
	for encodedKey := range sourceParams {
		key, err := base64.StdEncoding.DecodeString(encodedKey)
		require.NoError(t, err)
		recorded = recorded || bytes.HasPrefix(key, tokenfactoryParamsPrefix)
	}
	require.True(t, recorded, "the source phase recorded no tokenfactory parameter")
	requireV68OfflineNoTokenfactoryParams(t, testApp)
}

// requireV68OfflineNoTokenfactoryParams requires the committed params store to
// hold no tokenfactory parameter.
func requireV68OfflineNoTokenfactoryParams(t *testing.T, testApp *App) {
	t.Helper()
	store := testApp.CommitMultiStore().GetCommitKVStore(testApp.GetKey(paramstypes.StoreKey))
	iterator := sdk.KVStorePrefixIterator(store, tokenfactoryParamsPrefix)
	defer func() {
		require.NoError(t, iterator.Close())
	}()
	require.False(t, iterator.Valid(), "v6.8 left a tokenfactory parameter in the params store")
}

// v68OfflineWasmFlagKeys are the params-store keys of the wasm
// msgExecuteContractDisabled and contractExecutionDisabled parameters.
var v68OfflineWasmFlagKeys = [][]byte{
	[]byte(wasmtypes.ModuleName + "/" + string(wasmtypes.ParamStoreKeyMsgExecuteContractDisabled)),
	[]byte(wasmtypes.ModuleName + "/" + string(wasmtypes.ParamStoreKeyContractExecutionDisabled)),
}

// requireV68OfflineWasmFlagsStored requires the source phase's params snapshot
// to lack every wasm flag parameter and the committed params store to hold each
// as false.
func requireV68OfflineWasmFlagsStored(t *testing.T, testApp *App, sourceParams map[string]string) {
	t.Helper()
	store := testApp.CommitMultiStore().GetCommitKVStore(testApp.GetKey(paramstypes.StoreKey))
	for _, key := range v68OfflineWasmFlagKeys {
		_, recorded := sourceParams[encodeOfflineUpgradeKey(key)]
		require.False(t, recorded, "the source phase already stored %s", key)
		require.Equal(t, []byte("false"), store.Get(key), "v6.8 did not store %s as false", key)
	}
}

// requireV68OfflineMigrateCosmosEnabled requires the source phase's params
// snapshot to hold no MigrateCosmosEnabled parameter and the committed params
// store to hold it set to true.
func requireV68OfflineMigrateCosmosEnabled(t *testing.T, testApp *App, sourceParams map[string]string) {
	t.Helper()
	key := []byte(migration.SubspaceName + "/" + string(migration.KeyMigrateCosmosEnabled))
	_, recorded := sourceParams[encodeOfflineUpgradeKey(key)]
	require.False(t, recorded, "the source phase already held MigrateCosmosEnabled")
	store := testApp.CommitMultiStore().GetCommitKVStore(testApp.GetKey(paramstypes.StoreKey))
	require.Equal(t, []byte("true"), store.Get(key), "v6.8 did not enable MigrateCosmos")
}

// requireV68OfflineStateSyncTrustPeriodStored requires the source phase's params
// snapshot to hold no state sync trust period and the committed consensus params
// to hold two thirds of the unbonding time, at most v68MaxStateSyncTrustPeriod.
func requireV68OfflineStateSyncTrustPeriodStored(t *testing.T, testApp *App, sourceParams map[string]string) {
	t.Helper()
	key := []byte(baseapp.Paramspace + "/" + string(baseapp.ParamStoreKeyStateSyncParams))
	_, recorded := sourceParams[encodeOfflineUpgradeKey(key)]
	require.False(t, recorded, "the source phase already held a state sync trust period")
	ctx, _ := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()).CacheContext()
	stateSync := testApp.GetConsensusParams(ctx).StateSync
	require.NotNil(t, stateSync, "v6.8 did not store the state sync params")
	require.NotNil(t, stateSync.TrustPeriod, "v6.8 stored state sync params without a trust period")
	want := min(testApp.StakingKeeper.UnbondingTime(ctx)*2/3, v68MaxStateSyncTrustPeriod)
	require.Equal(t, want, *stateSync.TrustPeriod)
}

func requireV68OfflineVoucher(t *testing.T, testApp *App, retained offlineUpgradeRetainedState) {
	t.Helper()
	ctx, _ := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()).CacheContext()
	holder, err := sdk.AccAddressFromBech32(retained.VoucherHolder)
	require.NoError(t, err)
	amount, ok := sdk.NewIntFromString(retained.VoucherAmount)
	require.True(t, ok)
	supply, ok := sdk.NewIntFromString(retained.VoucherSupply)
	require.True(t, ok)
	voucher := sdk.NewCoin(retained.TransferIBCDenom, amount)
	require.Equal(t, voucher, testApp.BankKeeper.GetBalance(ctx, holder, voucher.Denom))
	require.Equal(t, sdk.NewCoin(voucher.Denom, supply), testApp.BankKeeper.GetSupply(ctx, voucher.Denom))

	recipient := sdk.AccAddress("v68-voucher-receiver")
	testApp.AccountKeeper.SetAccount(ctx, testApp.AccountKeeper.NewAccountWithAddress(ctx, recipient))
	send := sdk.NewCoin(voucher.Denom, sdk.OneInt())
	require.NoError(t, testApp.BankKeeper.SendCoins(ctx, holder, recipient, sdk.NewCoins(send)))
	require.Equal(t, send, testApp.BankKeeper.GetBalance(ctx, recipient, voucher.Denom))
	require.Equal(t, voucher.Sub(send), testApp.BankKeeper.GetBalance(ctx, holder, voucher.Denom))
	require.Equal(t, sdk.NewCoin(voucher.Denom, supply), testApp.BankKeeper.GetSupply(ctx, voucher.Denom))
}

func requireV68OfflineTreesDeleted(t *testing.T, root string) {
	t.Helper()
	store := memiavl.NewCommitStore(filepath.Join(root, "home"), memiavl.DefaultConfig())
	defer func() {
		require.NoError(t, store.Close())
	}()
	latestVersion, err := store.GetLatestVersion()
	require.NoError(t, err)
	require.NotZero(t, latestVersion)
	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)
	require.Equal(t, latestVersion, store.Version())
	require.NotNil(t, store.GetDB().TreeByName("bank"))
	for _, name := range v68OfflineDeletedStores {
		require.Nil(t, store.GetDB().TreeByName(name))
	}
}

// requireV68OfflineAccountsRewritten requires every recorded vesting account to
// be stored as the base account it embedded, holding the same balance, all of
// it spendable; every other account the source phase stored to be unchanged;
// and no account to be left under a vesting type.
func requireV68OfflineAccountsRewritten(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	after := snapshotCommittedOfflineUpgradeStore(t, testApp, authtypes.StoreKey)

	rewritten := make(map[string]struct{}, len(artifact.VestingAccounts))
	for _, recorded := range artifact.VestingAccounts {
		address := sdk.MustAccAddressFromBech32(recorded.Address)
		want, err := testApp.AccountKeeper.MarshalAccount(v68OfflineRecordedBaseAccount(t, recorded))
		require.NoError(t, err)
		key := encodeOfflineUpgradeKey(authtypes.AddressStoreKey(address))
		require.Equal(t, base64.StdEncoding.EncodeToString(want), after[key],
			"the %s was not rewritten as the base account it embeds", recorded.TypeURL)
		rewritten[key] = struct{}{}

		balance, err := sdk.ParseCoinsNormalized(recorded.Balance)
		require.NoError(t, err)
		require.Equal(t, balance, testApp.BankKeeper.GetAllBalances(ctx, address),
			"v6.8 changed the balance of the %s", recorded.TypeURL)
		require.Equal(t, balance, testApp.BankKeeper.SpendableCoins(ctx, address),
			"the balance the %s locked is not spendable after v6.8", recorded.TypeURL)
	}

	for key, value := range artifact.Stores[authtypes.StoreKey] {
		if _, ok := rewritten[key]; ok || !v68OfflineIsAccountKey(t, key) {
			continue
		}
		require.Equal(t, value, after[key], "v6.8 changed an account that was not a vesting account")
	}
	for key, value := range after {
		if !v68OfflineIsAccountKey(t, key) {
			continue
		}
		encoded, err := base64.StdEncoding.DecodeString(value)
		require.NoError(t, err)
		require.False(t, bytes.HasPrefix(v68OfflineTypeURL(encoded), []byte(v68OfflineLegacyVestingTypes)),
			"an account is still stored under %s after v6.8", v68OfflineTypeURL(encoded))
	}
}

// v68OfflineRecordedBaseAccount returns the base account a recorded vesting
// account embedded.
func v68OfflineRecordedBaseAccount(t *testing.T, recorded offlineUpgradeVestingAccount) *authtypes.BaseAccount {
	t.Helper()
	pubKey, err := hex.DecodeString(recorded.PubKey)
	require.NoError(t, err)
	return authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(recorded.Address),
		&secp256k1.PubKey{Key: pubKey}, recorded.AccountNumber, recorded.Sequence)
}

// v68OfflineIsAccountKey reports whether an encoded account store key holds an
// account rather than the global account number.
func v68OfflineIsAccountKey(t *testing.T, encodedKey string) bool {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	require.NoError(t, err)
	return bytes.HasPrefix(key, authtypes.AddressStoreKeyPrefix)
}

// v68OfflineTypeURL returns the type URL an encoded google.protobuf.Any leads
// with, or nil when it does not lead with one.
func v68OfflineTypeURL(encoded []byte) []byte {
	num, wireType, n := protowire.ConsumeTag(encoded)
	if n < 0 || num != 1 || wireType != protowire.BytesType {
		return nil
	}
	typeURL, m := protowire.ConsumeBytes(encoded[n:])
	if m < 0 {
		return nil
	}
	return typeURL
}

// requireV68OfflineLockedBalanceSpend delivers a signed bank send of the whole
// balance the recorded sender's schedule locked before v6.8, and requires the
// committed balances and sequence to move.
func requireV68OfflineLockedBalanceSpend(t *testing.T, root string, retained offlineUpgradeRetainedState) {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	upgradeHeight := testApp.LastBlockHeight()

	privBytes, err := hex.DecodeString(retained.TxSenderKey)
	require.NoError(t, err)
	priv := &secp256k1.PrivKey{Key: privBytes}
	sender := sdk.MustAccAddressFromBech32(retained.TxSender)
	require.Equal(t, sender, sdk.AccAddress(priv.PubKey().Address()),
		"recorded sender key does not match recorded sender address")
	recipient := sdk.MustAccAddressFromBech32(retained.TxRecipient)

	beforeCtx := offlineUpgradeReadContext(testApp, upgradeHeight)
	balance := testApp.BankKeeper.GetBalance(beforeCtx, sender, "usei")
	recipientBefore := testApp.BankKeeper.GetBalance(beforeCtx, recipient, "usei")
	sequence := testApp.AccountKeeper.GetAccount(beforeCtx, sender).GetSequence()
	amount := balance.Amount.Int64() - v68OfflinePostUpgradeFee

	txBz := signV68OfflineBankSend(t, testApp, priv, recipient, amount, v68OfflinePostUpgradeFee)
	res, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade-tx"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID,
			Height:  upgradeHeight + 1,
			Time:    v68OfflineUpgradeBlockTime.Add(time.Second),
		},
		Txs: [][]byte{txBz},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 1)
	require.Equal(t, uint32(abci.CodeTypeOK), res.TxResults[0].Code, res.TxResults[0].Log)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, upgradeHeight+1, reopened.LastBlockHeight())
	afterCtx := offlineUpgradeReadContext(reopened, reopened.LastBlockHeight())
	require.True(t, reopened.BankKeeper.GetBalance(afterCtx, sender, "usei").IsZero(),
		"the sender kept part of the balance its schedule had locked")
	require.Equal(t, recipientBefore.Add(sdk.NewInt64Coin("usei", amount)),
		reopened.BankKeeper.GetBalance(afterCtx, recipient, "usei"),
		"the bank send of the formerly locked balance did not credit the recipient")
	require.Equal(t, sequence+1, reopened.AccountKeeper.GetAccount(afterCtx, sender).GetSequence(),
		"the bank send did not advance the sender sequence")
}

func signV68OfflineBankSend(t *testing.T, testApp *App, priv *secp256k1.PrivKey, to sdk.AccAddress, amount, fee int64) []byte {
	t.Helper()
	from := sdk.AccAddress(priv.PubKey().Address())
	txConfig := testApp.GetTxConfig()
	txBuilder := txConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(banktypes.NewMsgSend(from, to, sdk.NewCoins(sdk.NewInt64Coin("usei", amount)))))
	txBuilder.SetGasLimit(1_000_000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", fee)))

	acc := testApp.AccountKeeper.GetAccount(offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()), from)
	require.NotNil(t, acc, "sender account is missing from the migrated database")
	signerData := xauthsigning.SignerData{
		ChainID:       offlineUpgradeChainID,
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}
	signMode := txConfig.SignModeHandler().DefaultMode()
	require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: signMode},
		Sequence: acc.GetSequence(),
	}))
	bytesToSign, err := txConfig.SignModeHandler().GetSignBytes(signMode, signerData, txBuilder.GetTx())
	require.NoError(t, err)
	signature, err := priv.Sign(bytesToSign)
	require.NoError(t, err)
	require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: signMode, Signature: signature},
		Sequence: acc.GetSequence(),
	}))
	bz, err := txConfig.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	return bz
}

// testV68OfflineUpgradeTargetSnapshot applies v6.8 to a real pre-v6.8 node
// home and logs how long the account rewrite took against it.
func testV68OfflineUpgradeTargetSnapshot(t *testing.T) {
	home := requireOfflineUpgradeSnapshotHome(t)
	t.Setenv("UPGRADE_VERSION_LIST", v68OfflineUpgradeName)
	chainID := readOfflineUpgradeGenesisChainID(t, home)

	// The store loader deletes the retired stores only when upgrade-info.json
	// names v6.8, as it does after the previous binary halts at the plan.
	halted := openOfflineUpgradeSnapshotApp(t, home, chainID)
	sourceHeight := halted.LastBlockHeight()
	require.NoError(t, halted.UpgradeKeeper.DumpUpgradeInfoToDisk(sourceHeight+1, v68OfflineUpgradeName))
	closeOfflineUpgradeApp(t, halted)

	testApp := openOfflineUpgradeSnapshotApp(t, home, chainID)
	require.Equal(t, sourceHeight, testApp.LastBlockHeight())
	beforeVersions := offlineUpgradeModuleVersions(t, testApp)
	require.Contains(t, beforeVersions, "oracle")
	require.Contains(t, beforeVersions, "tokenfactory")
	require.Contains(t, beforeVersions, "vesting",
		"%s is not a pre-v6.8 snapshot: module version map is missing vesting", home)
	legacy := v68OfflineLegacyVestingAccounts(t, testApp, offlineUpgradeContext(testApp, sourceHeight, chainID))
	balances := make(map[string]sdk.Coins, len(legacy))
	for _, address := range legacy {
		balances[address.String()] = testApp.BankKeeper.GetAllBalances(
			offlineUpgradeContext(testApp, sourceHeight, chainID), address)
	}

	require.True(t, testApp.UpgradeKeeper.HasHandler(v68OfflineUpgradeName),
		"v6.8 upgrade handler is not registered; set UPGRADE_VERSION_LIST=v6.8")
	upgradeHeight := sourceHeight + 1
	started := time.Now()
	testApp.UpgradeKeeper.ApplyUpgrade(offlineUpgradeContext(testApp, upgradeHeight, chainID), upgradetypes.Plan{
		Name:   v68OfflineUpgradeName,
		Height: upgradeHeight,
	})
	t.Logf("v6.8 rewrote %d vesting accounts of %s in %s", len(legacy), home, time.Since(started))
	testApp.CommitMultiStore().Commit(true)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeSnapshotApp(t, home, chainID)
	defer closeOfflineUpgradeApp(t, reopened)
	requireV68OfflineVersionMap(t, reopened, beforeVersions)
	requireV68OfflineNoRetiredProposals(t, reopened)
	requireV68OfflineNoDenomAllowLists(t, reopened)
	requireV68OfflineNoTokenfactoryParams(t, reopened)
	ctx := offlineUpgradeContext(reopened, reopened.LastBlockHeight(), chainID)
	require.Empty(t, v68OfflineLegacyVestingAccounts(t, reopened, ctx), "accounts are still stored under a vesting type")
	for _, address := range legacy {
		_, ok := reopened.AccountKeeper.GetAccount(ctx, address).(*authtypes.BaseAccount)
		require.True(t, ok, "%s was not rewritten as a base account", address)
		require.Equal(t, balances[address.String()], reopened.BankKeeper.GetAllBalances(ctx, address),
			"v6.8 changed the balance of %s", address)
	}
}

// v68OfflineLegacyVestingAccounts returns the address of every account stored
// under a vesting type.
func v68OfflineLegacyVestingAccounts(t *testing.T, testApp *App, ctx sdk.Context) []sdk.AccAddress {
	t.Helper()
	store := prefix.NewStore(ctx.KVStore(testApp.GetKey(authtypes.StoreKey)), authtypes.AddressStoreKeyPrefix)
	iterator := store.Iterator(nil, nil)
	defer func() {
		require.NoError(t, iterator.Close())
	}()
	var addresses []sdk.AccAddress
	for ; iterator.Valid(); iterator.Next() {
		if bytes.HasPrefix(v68OfflineTypeURL(iterator.Value()), []byte(v68OfflineLegacyVestingTypes)) {
			addresses = append(addresses, append(sdk.AccAddress(nil), iterator.Key()...))
		}
	}
	return addresses
}
