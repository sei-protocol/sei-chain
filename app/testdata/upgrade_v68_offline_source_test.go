//go:build upgrade_v68 && offline_upgrade && upgrade_source

package app

import (
	"sort"
	"testing"

	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/stretchr/testify/require"
)

const v68OfflineUpgradeName = "v6.8"

func v68OfflineStoreNames(testApp *App) []string {
	keys := testApp.CommitMultiStore().StoreKeys()
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, key.Name())
	}
	sort.Strings(names)
	return names
}

func TestV68OfflineUpgradeSource(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "source")
	testApp := openOfflineUpgradeApp(t, root, true)
	ctx := testApp.GetContextForDeliverTx(nil)
	upgradeHeight := ctx.BlockHeight() + 1
	require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{
		Name:   v68OfflineUpgradeName,
		Height: upgradeHeight,
	}))
	commitOfflineUpgradeApp(t, testApp)
	sourceHeight := testApp.LastBlockHeight()
	moduleVersions := offlineUpgradeModuleVersions(t, testApp)
	stores := make(map[string]map[string]string)
	for _, name := range v68OfflineStoreNames(testApp) {
		stores[name] = map[string]string{}
	}
	require.NotEmpty(t, moduleVersions)
	require.NotEmpty(t, stores)

	closeOfflineUpgradeApp(t, testApp)

	writeOfflineUpgradeArtifact(t, root, offlineUpgradeArtifact{
		Upgrade:        v68OfflineUpgradeName,
		SourceHeight:   sourceHeight,
		UpgradeHeight:  upgradeHeight,
		ModuleVersions: moduleVersions,
		Stores:         stores,
	})
}

func TestV68OfflineUpgradeReopen(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "reopen")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, v68OfflineUpgradeName, artifact.Upgrade)
	require.NotEmpty(t, artifact.UpgradeHash)

	testApp := openOfflineUpgradeApp(t, offlineUpgradeMigratedDatabase(t, root, artifact), false)
	defer closeOfflineUpgradeApp(t, testApp)

	require.Equal(t, artifact.UpgradeHeight, testApp.LastBlockHeight())
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, testApp))
	requireOfflineUpgradeStoresMounted(t, testApp, sortedOfflineStoreNames(artifact.Stores))
}

func sortedOfflineStoreNames(stores map[string]map[string]string) []string {
	names := make([]string, 0, len(stores))
	for name := range stores {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
