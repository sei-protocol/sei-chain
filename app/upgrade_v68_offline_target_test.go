//go:build upgrade_v68 && offline_upgrade && upgrade_target

package app

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

const v68OfflineUpgradeName = "v6.8"

const v68OfflineUpgradeBlockTimeUnix = 1_700_000_000

func v68OfflineStoreNames(testApp *App) []string {
	keys := testApp.CommitMultiStore().StoreKeys()
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, key.Name())
	}
	sort.Strings(names)
	return names
}

func TestV68OfflineUpgradeTarget(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, v68OfflineUpgradeName, artifact.Upgrade)
	t.Setenv("UPGRADE_VERSION_LIST", LatestUpgrade)

	testApp := openOfflineUpgradeApp(t, root, false)
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found)
	require.Equal(t, artifact.Upgrade, plan.Name)
	require.Equal(t, artifact.UpgradeHeight, plan.Height)
	require.True(t, testApp.UpgradeKeeper.HasHandler(plan.Name))
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, testApp))
	require.Equal(t, sortedOfflineStoreNames(artifact.Stores), v68OfflineStoreNames(testApp))

	_, err := testApp.FinalizeBlock(context.Background(), &types.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID,
			Height:  artifact.UpgradeHeight,
			Time:    time.Unix(v68OfflineUpgradeBlockTimeUnix, 0).UTC(),
		},
	})
	require.NoError(t, err)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	migrated := filepath.Join(root, offlineUpgradeMigratedDir)
	reopened := openOfflineUpgradeApp(t, migrated, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, reopened))
	require.Equal(t, sortedOfflineStoreNames(artifact.Stores), v68OfflineStoreNames(reopened))

	artifact.MigratedRoot = offlineUpgradeMigratedDir
	artifact.UpgradeHash = offlineUpgradeHashString(committedOfflineUpgradeHash(t, reopened))
	writeOfflineUpgradeArtifact(t, root, artifact)
}

func sortedOfflineStoreNames(stores map[string]map[string]string) []string {
	names := make([]string, 0, len(stores))
	for name := range stores {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
