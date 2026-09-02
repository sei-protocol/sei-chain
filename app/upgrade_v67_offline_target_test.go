//go:build upgrade_v67 && offline_upgrade && upgrade_target

package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

var v67OfflineRemovedModules = []string{
	"capability",
	"feegrant",
	"ibc",
	"transfer",
}

var v67OfflineUpgradeBlockTime = time.Unix(1_700_000_000, 0).UTC()

func TestV67OfflineUpgradeTarget(t *testing.T) {
	t.Run("fixture", testV67OfflineUpgradeTargetFixture)
	t.Run("snapshot", testV67OfflineUpgradeTargetSnapshot)
}

func testV67OfflineUpgradeTargetFixture(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, "v6.7", artifact.Upgrade)
	requireV67OfflineRetainedIdentities(t, artifact.Retained)
	require.Equal(t, artifact.SourceHeight+1, artifact.UpgradeHeight)

	t.Setenv("UPGRADE_VERSION_LIST", LatestUpgrade)

	cleanRoot := filepath.Join(root, offlineUpgradeMigratedDir)
	crashRoot := filepath.Join(root, "crash")
	copyOfflineUpgradeDatabase(t, root, cleanRoot)
	copyOfflineUpgradeDatabase(t, root, crashRoot)

	cleanHash := applyV67OfflineUpgradeClean(t, cleanRoot, artifact)
	crashHash := applyV67OfflineUpgradeCrashReplay(t, crashRoot, artifact)
	require.Equalf(t, cleanHash, crashHash,
		"crash-replay application hash diverged from the clean single-pass hash: clean=%x crash-replay=%x",
		cleanHash, crashHash)

	artifact.MigratedRoot = offlineUpgradeMigratedDir
	artifact.UpgradeHash = offlineUpgradeHashString(cleanHash)
	writeOfflineUpgradeArtifact(t, root, artifact)
}

func applyV67OfflineUpgradeClean(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV67OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, testApp),
		"v6.7 did not reopen the v6.6 module version map")
	requireV67OfflineRetainedStores(t, testApp, artifact)
	requireV67OfflineBankState(t, testApp, artifact.Retained)

	finalizeV67OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV67OfflineAppliedName(t, reopened, artifact)
	requireV67OfflineVersionMap(t, reopened, artifact.ModuleVersions)
	requireV67OfflineRetainedStores(t, reopened, artifact)
	requireV67OfflineBankState(t, reopened, artifact.Retained)
	requireV67OfflineVoucherSend(t, reopened, artifact.Retained)
	return committedOfflineUpgradeHash(t, reopened)
}

func applyV67OfflineUpgradeCrashReplay(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV67OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.SourceHeight, testApp.LastBlockHeight())
	finalizeV67OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	closeOfflineUpgradeApp(t, testApp)

	interrupted := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.SourceHeight, interrupted.LastBlockHeight(),
		"closing without commit left the crash-replay database above the pre-upgrade height")
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, interrupted),
		"closing without commit mutated the pre-upgrade version map")
	finalizeV67OfflineUpgrade(t, interrupted, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, interrupted)
	closeOfflineUpgradeApp(t, interrupted)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV67OfflineAppliedName(t, reopened, artifact)
	requireV67OfflineVersionMap(t, reopened, artifact.ModuleVersions)
	return committedOfflineUpgradeHash(t, reopened)
}

func requireV67OfflinePersistedPlanHasHandler(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found, "committed upgrade plan did not survive the process boundary")
	require.Equal(t, artifact.Upgrade, plan.Name)
	require.Equal(t, artifact.UpgradeHeight, plan.Height)
	require.Equal(t, LatestUpgrade, plan.Name)
	require.True(t, testApp.UpgradeKeeper.HasHandler(plan.Name),
		"target binary has no handler for persisted plan name %q", plan.Name)
}

func requireV67OfflineAppliedName(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(
		offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()))
	require.Equal(t, artifact.Upgrade, lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.True(t, testApp.UpgradeKeeper.HasHandler(lastName),
		"target binary has no handler for applied plan name %q", lastName)
}

func finalizeV67OfflineUpgrade(t *testing.T, testApp *App, height int64) {
	t.Helper()
	_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID,
			Height:  height,
			Time:    v67OfflineUpgradeBlockTime,
		},
	})
	require.NoError(t, err)
}

func testV67OfflineUpgradeTargetSnapshot(t *testing.T) {
	home := requireOfflineUpgradeSnapshotHome(t)
	t.Setenv("UPGRADE_VERSION_LIST", "v6.7")
	chainID := readOfflineUpgradeGenesisChainID(t, home)

	testApp := openOfflineUpgradeSnapshotApp(t, home, chainID)
	sourceHeight := testApp.LastBlockHeight()
	readCtx := offlineUpgradeContext(testApp, sourceHeight, chainID)
	beforeVersions := offlineUpgradeModuleVersions(t, testApp)
	for _, module := range v67OfflineRemovedModules {
		require.Contains(t, beforeVersions, module,
			"%s is not a pre-v6.7 snapshot: module version map is missing %s", home, module)
	}
	require.Contains(t, beforeVersions, "oracle")
	beforeStores := snapshotOfflineUpgradeStores(t, testApp, readCtx, v67OfflineRemovedModules)

	require.True(t, testApp.UpgradeKeeper.HasHandler("v6.7"),
		"v6.7 upgrade handler is not registered; set UPGRADE_VERSION_LIST=v6.7")
	upgradeHeight := sourceHeight + 1
	upgradeCtx := offlineUpgradeContext(testApp, upgradeHeight, chainID)
	testApp.UpgradeKeeper.ApplyUpgrade(upgradeCtx, upgradetypes.Plan{
		Name:   "v6.7",
		Height: upgradeHeight,
	})
	testApp.CommitMultiStore().Commit(true)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeSnapshotApp(t, home, chainID)
	defer closeOfflineUpgradeApp(t, reopened)
	requireV67OfflineVersionMap(t, reopened, beforeVersions)
	requireOfflineUpgradeRetainedStores(t, reopened, beforeStores)
}

func requireV67OfflineVersionMap(t *testing.T, testApp *App, before []string) {
	t.Helper()
	after := offlineUpgradeModuleVersions(t, testApp)
	require.Equal(t, v67OfflineRemovedModules,
		offlineUpgradeDifference(before, after),
		"v6.7 removed an unexpected set of module versions")
	require.Contains(t, after, "oracle")
	require.True(t, offlineUpgradeHasModuleVersion(t, testApp, "oracle"),
		"upgrade store dropped the oracle version-map entry")
	for _, module := range v67OfflineRemovedModules {
		require.False(t, offlineUpgradeHasModuleVersion(t, testApp, module),
			"upgrade store still has a version-map entry for %s", module)
	}
}

func requireV67OfflineRetainedStores(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	requireOfflineUpgradeRetainedStores(t, testApp, artifact.Stores)
	for storeName, want := range artifact.Stores {
		requireV67OfflineRetainedStoreKeys(t, storeName, want, artifact.Retained)
	}
}

func requireV67OfflineRetainedIdentities(t *testing.T, retained offlineUpgradeRetainedState) {
	t.Helper()
	require.NotEmpty(t, retained.FeegrantGranter)
	require.NotEmpty(t, retained.FeegrantGrantee)
	require.NotEmpty(t, retained.FeegrantKey)
	require.NotEmpty(t, retained.CapabilityName)
	require.NotZero(t, retained.CapabilityIndex)
	require.NotEmpty(t, retained.CapabilityOwnersKey)
	require.NotEmpty(t, retained.IBCClientID)
	require.NotEmpty(t, retained.IBCClientStateKey)
	require.NotEmpty(t, retained.IBCConnectionID)
	require.NotEmpty(t, retained.IBCConnectionKey)
	require.NotEmpty(t, retained.IBCPortID)
	require.NotEmpty(t, retained.IBCChannelID)
	require.NotEmpty(t, retained.IBCChannelKey)
	require.NotEmpty(t, retained.TransferDenomHash)
	require.NotEmpty(t, retained.TransferIBCDenom)
	require.NotEmpty(t, retained.TransferTraceKey)
	require.NotEmpty(t, retained.EscrowAddress)
	require.NotEmpty(t, retained.EscrowAmount)
	require.NotEmpty(t, retained.EscrowSupply)
	require.NotEmpty(t, retained.VoucherHolder)
	require.NotEmpty(t, retained.VoucherAmount)
	require.NotEmpty(t, retained.VoucherSupply)
}

// requireV67OfflineBankState asserts the recorded IBC escrow and voucher bank
// balances and total supplies.
func requireV67OfflineBankState(t *testing.T, testApp *App, retained offlineUpgradeRetainedState) {
	t.Helper()
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())

	escrowAddr, err := sdk.AccAddressFromBech32(retained.EscrowAddress)
	require.NoError(t, err)
	escrowCoin := offlineUpgradeRecordedCoin(t, "usei", retained.EscrowAmount)
	require.Equal(t, escrowCoin, testApp.BankKeeper.GetBalance(ctx, escrowAddr, escrowCoin.Denom),
		"v6.7 changed the IBC escrow balance")
	require.Equal(t, offlineUpgradeRecordedCoin(t, "usei", retained.EscrowSupply),
		testApp.BankKeeper.GetSupply(ctx, "usei"),
		"v6.7 changed usei total supply; escrowed coins must remain counted")

	holder, err := sdk.AccAddressFromBech32(retained.VoucherHolder)
	require.NoError(t, err)
	voucherCoin := offlineUpgradeRecordedCoin(t, retained.TransferIBCDenom, retained.VoucherAmount)
	require.Equal(t, voucherCoin, testApp.BankKeeper.GetBalance(ctx, holder, voucherCoin.Denom),
		"v6.7 changed the IBC voucher holder balance")
	require.Equal(t, offlineUpgradeRecordedCoin(t, retained.TransferIBCDenom, retained.VoucherSupply),
		testApp.BankKeeper.GetSupply(ctx, voucherCoin.Denom),
		"v6.7 changed IBC voucher total supply")
}

// requireV67OfflineVoucherSend sends the recorded IBC voucher from its holder to
// another account through the bank keeper.
func requireV67OfflineVoucherSend(t *testing.T, testApp *App, retained offlineUpgradeRetainedState) {
	t.Helper()
	ctx, _ := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()).CacheContext()
	holder, err := sdk.AccAddressFromBech32(retained.VoucherHolder)
	require.NoError(t, err)
	voucher := offlineUpgradeRecordedCoin(t, retained.TransferIBCDenom, retained.VoucherAmount)
	require.True(t, voucher.Amount.GT(sdk.OneInt()), "voucher amount is too small to send")

	recipient := sdk.AccAddress("v67-voucher-receiver")
	testApp.AccountKeeper.SetAccount(ctx, testApp.AccountKeeper.NewAccountWithAddress(ctx, recipient))
	send := sdk.NewCoin(voucher.Denom, sdk.OneInt())
	require.NoError(t, testApp.BankKeeper.SendCoins(ctx, holder, recipient, sdk.NewCoins(send)),
		"bank send of an IBC voucher failed after v6.7")
	require.Equal(t, send, testApp.BankKeeper.GetBalance(ctx, recipient, voucher.Denom),
		"bank send of an IBC voucher did not credit the recipient")
	require.Equal(t, voucher.Sub(send), testApp.BankKeeper.GetBalance(ctx, holder, voucher.Denom),
		"bank send of an IBC voucher did not debit the holder")
	require.Equal(t, offlineUpgradeRecordedCoin(t, voucher.Denom, retained.VoucherSupply),
		testApp.BankKeeper.GetSupply(ctx, voucher.Denom),
		"sending an IBC voucher changed its total supply")
}

func offlineUpgradeRecordedCoin(t *testing.T, denom, amount string) sdk.Coin {
	t.Helper()
	parsed, ok := sdk.NewIntFromString(amount)
	require.True(t, ok, "invalid recorded amount %q for denom %s", amount, denom)
	return sdk.NewCoin(denom, parsed)
}

func requireV67OfflineRetainedStoreKeys(
	t *testing.T,
	storeName string,
	snapshot map[string]string,
	retained offlineUpgradeRetainedState,
) {
	t.Helper()
	switch storeName {
	case "feegrant":
		requireOfflineUpgradeStoreKey(t, snapshot, "feegrant allowance", retained.FeegrantKey)
	case "capability":
		requireOfflineUpgradeStoreKey(t, snapshot, "capability owner set", retained.CapabilityOwnersKey)
	case "ibc":
		requireOfflineUpgradeStoreKey(t, snapshot, "IBC client "+retained.IBCClientID, retained.IBCClientStateKey)
		requireOfflineUpgradeStoreKey(t, snapshot, "IBC connection "+retained.IBCConnectionID, retained.IBCConnectionKey)
		requireOfflineUpgradeStoreKey(t, snapshot, "IBC channel "+retained.IBCPortID+"/"+retained.IBCChannelID, retained.IBCChannelKey)
	case "transfer":
		requireOfflineUpgradeStoreKey(t, snapshot, "transfer denom trace "+retained.TransferIBCDenom, retained.TransferTraceKey)
	}
}
