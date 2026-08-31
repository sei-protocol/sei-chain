//go:build upgrade_v67 && offline_upgrade && upgrade_target

package app

import (
	"context"
	"testing"
	"time"

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

func TestV67OfflineUpgradeTarget(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, "v6.7", artifact.Upgrade)

	t.Setenv("UPGRADE_VERSION_LIST", artifact.Upgrade)
	testApp := openOfflineUpgradeApp(t, root, false)
	readCtx := offlineUpgradeReadContext(testApp, artifact.SourceHeight)
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(testApp, readCtx),
		"v6.7 did not reopen the v6.6 module version map")
	for storeName, want := range artifact.Stores {
		require.Equal(t, want, snapshotOfflineUpgradeStore(t, testApp, readCtx, storeName),
			"v6.7 did not reopen the v6.6 %s store", storeName)
	}

	require.Equal(t, artifact.SourceHeight+1, artifact.UpgradeHeight)
	_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID,
			Height:  artifact.UpgradeHeight,
			Time:    time.Now(),
		},
	})
	require.NoError(t, err)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	reopenedCtx := offlineUpgradeReadContext(reopened, artifact.UpgradeHeight)
	moduleVersions := offlineUpgradeModuleVersions(reopened, reopenedCtx)
	require.Equal(t, v67OfflineRemovedModules,
		offlineUpgradeDifference(artifact.ModuleVersions, moduleVersions),
		"v6.7 removed an unexpected set of module versions")
	require.Contains(t, moduleVersions, "oracle")
	for storeName, want := range artifact.Stores {
		require.Equal(t, want, snapshotOfflineUpgradeStore(t, reopened, reopenedCtx, storeName),
			"v6.7 changed retained %s state", storeName)
	}
}
