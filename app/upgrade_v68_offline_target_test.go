//go:build upgrade_v68 && offline_upgrade && upgrade_target

package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

var v68OfflineRemovedModules = []string{"oracle"}
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
	requireV68OfflineAppliedName(t, reopened, artifact)
	requireV68OfflineVersionMap(t, reopened, artifact.ModuleVersions)
	requireV68OfflineOracleStoreDeleted(t, reopened)
	hash := committedOfflineUpgradeHash(t, reopened)
	closeOfflineUpgradeApp(t, reopened)
	requireV68OfflineOracleTreeDeleted(t, root)
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
	requireV68OfflineAppliedName(t, reopened, artifact)
	requireV68OfflineVersionMap(t, reopened, artifact.ModuleVersions)
	requireV68OfflineOracleStoreDeleted(t, reopened)
	hash := committedOfflineUpgradeHash(t, reopened)
	closeOfflineUpgradeApp(t, reopened)
	requireV68OfflineOracleTreeDeleted(t, root)
	return hash
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
}

func requireV68OfflineOracleStoreDeleted(t *testing.T, testApp *App) {
	t.Helper()
	for _, key := range testApp.CommitMultiStore().StoreKeys() {
		require.NotEqual(t, "oracle", key.Name())
	}
}

func requireV68OfflineOracleTreeDeleted(t *testing.T, root string) {
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
	require.Nil(t, store.GetDB().TreeByName("oracle"))
}

func requireV68OfflineVersionMap(t *testing.T, testApp *App, before []string) {
	t.Helper()
	after := offlineUpgradeModuleVersions(t, testApp)
	require.Equal(t, v68OfflineRemovedModules, offlineUpgradeDifference(before, after))
	for _, module := range v68OfflineRemovedModules {
		require.False(t, offlineUpgradeHasModuleVersion(t, testApp, module))
	}
}
