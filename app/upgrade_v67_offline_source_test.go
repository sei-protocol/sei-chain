//go:build upgrade_v67 && offline_upgrade && upgrade_source

package app

import (
	"testing"

	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/stretchr/testify/require"
)

var v67OfflineSourceStores = []string{
	"feegrant",
	"capability",
	"ibc",
	"transfer",
}

func TestV67OfflineUpgradeSource(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "source")
	testApp := openOfflineUpgradeApp(t, root, true)
	ctx := testApp.GetContextForDeliverTx(nil)

	moduleVersions := offlineUpgradeModuleVersions(testApp, ctx)
	expectedModules := append([]string(nil), v67OfflineSourceStores...)
	expectedModules = append(expectedModules, "oracle")
	for _, module := range expectedModules {
		require.Contains(t, moduleVersions, module,
			"v6.6 module version map does not contain %s", module)
	}
	stores := seedOfflineUpgradeStores(t, testApp, ctx, v67OfflineSourceStores)
	upgradeHeight := ctx.BlockHeight() + 2
	require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{
		Name:   "v6.7",
		Height: upgradeHeight,
	}))

	commitOfflineUpgradeApp(t, testApp)
	sourceHeight := testApp.LastBlockHeight()
	closeOfflineUpgradeApp(t, testApp)

	writeOfflineUpgradeArtifact(t, root, offlineUpgradeArtifact{
		Upgrade:        "v6.7",
		SourceHeight:   sourceHeight,
		UpgradeHeight:  upgradeHeight,
		ModuleVersions: moduleVersions,
		Stores:         stores,
	})
}
