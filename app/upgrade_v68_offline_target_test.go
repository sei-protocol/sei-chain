//go:build upgrade_v68 && offline_upgrade && upgrade_target

package app

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

var v68OfflineRemovedModules = []string{"capability", "ibc", "oracle", "transfer"}
var v68OfflineUpgradeBlockTime = time.Unix(1_700_000_000, 0).UTC()

func TestV68OfflineUpgradeTarget(t *testing.T) {
	t.Run("fixture", testV68OfflineUpgradeTargetFixture)
	t.Run("snapshot", testV68OfflineUpgradeTargetSnapshot)
}

func testV68OfflineUpgradeTargetFixture(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, "v6.8", artifact.Upgrade)
	require.Equal(t, artifact.SourceHeight+1, artifact.UpgradeHeight)
	t.Setenv("UPGRADE_VERSION_LIST", LatestUpgrade)
	cleanRoot := filepath.Join(root, offlineUpgradeMigratedDir)
	crashRoot := filepath.Join(root, "crash")
	copyOfflineUpgradeDatabase(t, root, cleanRoot)
	copyOfflineUpgradeDatabase(t, root, crashRoot)
	cleanHash := applyV68OfflineUpgradeClean(t, cleanRoot, artifact)
	crashHash := applyV68OfflineUpgradeCrashReplay(t, crashRoot, artifact)
	require.Equalf(t, cleanHash, crashHash,
		"crash-replay application hash diverged: clean=%x crash=%x", cleanHash, crashHash)
	artifact.MigratedRoot = offlineUpgradeMigratedDir
	artifact.UpgradeHash = offlineUpgradeHashString(cleanHash)
	writeOfflineUpgradeArtifact(t, root, artifact)
}

func applyV68OfflineUpgradeClean(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV68OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, testApp))
	finalizeV68OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)
	reopened := openOfflineUpgradeApp(t, root, false)
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
	require.Equal(t, artifact.SourceHeight, interrupted.LastBlockHeight())
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, interrupted))
	finalizeV68OfflineUpgrade(t, interrupted, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, interrupted)
	closeOfflineUpgradeApp(t, interrupted)
	reopened := openOfflineUpgradeApp(t, root, false)
	requireV68OfflineMigrated(t, reopened, artifact)
	hash := committedOfflineUpgradeHash(t, reopened)
	closeOfflineUpgradeApp(t, reopened)
	requireV68OfflineTreesDeleted(t, root)
	return hash
}

// requireV68OfflineMigrated checks a reopened migrated database: the upgrade
// is recorded, the retired module versions and stores are gone, the retired
// IBC proposal reads back as a text proposal, the upgraded IBC client record
// is pruned, and the IBC voucher balance and supply are intact and spendable.
func requireV68OfflineMigrated(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	requireV68OfflineAppliedName(t, testApp, artifact)
	requireV68OfflineVersionMap(t, testApp, artifact.ModuleVersions)
	requireV68OfflineStoresDeleted(t, testApp)
	requireV68OfflineProposalRewritten(t, testApp, artifact.Retained)
	requireV68OfflineUpgradedIBCStatePruned(t, testApp, artifact.Retained)
	requireV68OfflineVoucher(t, testApp, artifact.Retained)
}

func requireV68OfflinePersistedPlanHasHandler(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found)
	require.Equal(t, artifact.Upgrade, plan.Name)
	require.Equal(t, artifact.UpgradeHeight, plan.Height)
	require.True(t, testApp.UpgradeKeeper.HasHandler(plan.Name))
}

func requireV68OfflineAppliedName(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(
		offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()))
	require.Equal(t, artifact.Upgrade, lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.True(t, testApp.UpgradeKeeper.HasHandler(lastName))
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

func testV68OfflineUpgradeTargetSnapshot(t *testing.T) {
	home := requireOfflineUpgradeSnapshotHome(t)
	t.Setenv("UPGRADE_VERSION_LIST", "v6.8")
	chainID := readOfflineUpgradeGenesisChainID(t, home)
	testApp := openOfflineUpgradeSnapshotApp(t, home, chainID)
	sourceHeight := testApp.LastBlockHeight()
	beforeVersions := offlineUpgradeModuleVersions(t, testApp)
	require.Contains(t, beforeVersions, "oracle")
	require.True(t, testApp.UpgradeKeeper.HasHandler("v6.8"))
	upgradeHeight := sourceHeight + 1
	upgradeCtx := offlineUpgradeContext(testApp, upgradeHeight, chainID)
	testApp.UpgradeKeeper.ApplyUpgrade(upgradeCtx, upgradetypes.Plan{Name: "v6.8", Height: upgradeHeight})
	testApp.CommitMultiStore().Commit(true)
	closeOfflineUpgradeApp(t, testApp)
	reopened := openOfflineUpgradeSnapshotApp(t, home, chainID)
	defer closeOfflineUpgradeApp(t, reopened)
	requireV68OfflineVersionMap(t, reopened, beforeVersions)
	requireV68OfflineNoRetiredProposals(t, reopened)
}

func requireV68OfflineStoresDeleted(t *testing.T, testApp *App) {
	t.Helper()
	for _, name := range v68OfflineRemovedModules {
		require.Nil(t, testApp.GetKey(name))
	}
	for _, key := range testApp.CommitMultiStore().StoreKeys() {
		require.NotContains(t, v68OfflineRemovedModules, key.Name())
	}
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
	for _, name := range v68OfflineRemovedModules {
		require.Nil(t, store.GetDB().TreeByName(name))
	}
}

func requireV68OfflineVersionMap(t *testing.T, testApp *App, before []string) {
	t.Helper()
	after := offlineUpgradeModuleVersions(t, testApp)
	require.Subset(t, v68OfflineRemovedModules, offlineUpgradeDifference(before, after))
	require.Contains(t, offlineUpgradeDifference(before, after), "oracle")
	for _, module := range v68OfflineRemovedModules {
		require.False(t, offlineUpgradeHasModuleVersion(t, testApp, module))
	}
}
