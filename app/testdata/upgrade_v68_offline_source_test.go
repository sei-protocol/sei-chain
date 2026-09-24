//go:build upgrade_v68 && offline_upgrade && upgrade_source

package app

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

var v68OfflineSourceStores = []string{"oracle"}

func TestV68OfflineUpgradeSource(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "source")
	testApp := openOfflineUpgradeApp(t, root, true)
	ctx := testApp.GetContextForDeliverTx(nil).WithBlockTime(time.Now().UTC())
	retained := seedV68OfflineUpgradeState(t, testApp, ctx)
	stores := snapshotOfflineUpgradeStores(t, testApp, ctx, v68OfflineSourceStores)
	upgradeHeight := ctx.BlockHeight() + 2
	require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{
		Name: "v6.8", Height: upgradeHeight,
	}))
	commitOfflineUpgradeApp(t, testApp)
	sourceHeight := testApp.LastBlockHeight()
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found, "scheduled upgrade plan was not committed")
	require.Equal(t, "v6.8", plan.Name)
	require.Equal(t, upgradeHeight, plan.Height)
	moduleVersions := offlineUpgradeModuleVersions(t, testApp)
	require.Contains(t, moduleVersions, "oracle")
	closeOfflineUpgradeApp(t, testApp)
	writeOfflineUpgradeArtifact(t, root, offlineUpgradeArtifact{
		Upgrade: plan.Name, SourceHeight: sourceHeight, UpgradeHeight: upgradeHeight,
		ModuleVersions: moduleVersions, Stores: stores, Retained: retained,
	})
	requireV68OfflineUnupgradedHalt(t, root, sourceHeight, upgradeHeight)
}

func TestV68OfflineUpgradeReopen(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "reopen")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, "v6.8", artifact.Upgrade)
	require.NotEmpty(t, artifact.UpgradeHash)
	migrated := offlineUpgradeMigratedDatabase(t, root, artifact)
	reopenRoot := filepath.Join(root, "reopen")
	copyOfflineUpgradeDatabase(t, migrated, reopenRoot)
	testApp := openOfflineUpgradeApp(t, reopenRoot, false)
	defer closeOfflineUpgradeApp(t, testApp)
	require.Equal(t, artifact.UpgradeHeight, testApp.LastBlockHeight())
	versions := offlineUpgradeModuleVersions(t, testApp)
	require.NotContains(t, versions, "oracle")
	require.False(t, offlineUpgradeHasModuleVersion(t, testApp, "oracle"))
	requireOfflineUpgradeStoresMounted(t, testApp, v68OfflineSourceStores)
	for storeName, want := range artifact.Stores {
		require.Equal(t, want, snapshotCommittedOfflineUpgradeStore(t, testApp, storeName))
	}
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(ctx)
	require.Equal(t, artifact.Upgrade, lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.False(t, testApp.UpgradeKeeper.HasHandler("v6.8"))
}

func requireV68OfflineUnupgradedHalt(t *testing.T, root string, sourceHeight, upgradeHeight int64) {
	t.Helper()
	haltRoot := filepath.Join(root, "unupgraded-halt")
	copyOfflineUpgradeDatabase(t, root, haltRoot)
	testApp := openOfflineUpgradeApp(t, haltRoot, false)
	require.Equal(t, sourceHeight, testApp.LastBlockHeight())
	var panicked any
	func() {
		defer func() { panicked = recover() }()
		_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Hash:   []byte("offline-upgrade-unupgraded-halt"),
			Header: &tmproto.Header{ChainID: offlineUpgradeChainID, Height: upgradeHeight},
		})
		require.NoError(t, err)
	}()
	require.NotNil(t, panicked)
	msg := fmt.Sprint(panicked)
	require.Contains(t, msg, `UPGRADE "v6.8" NEEDED`)
	require.Contains(t, msg, fmt.Sprintf("height: %d", upgradeHeight))
	require.Equal(t, sourceHeight, testApp.LastBlockHeight())
	closeOfflineUpgradeApp(t, testApp)
	reopened := openOfflineUpgradeApp(t, haltRoot, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, sourceHeight, reopened.LastBlockHeight())
}

func seedV68OfflineUpgradeState(t *testing.T, testApp *App, ctx sdk.Context) offlineUpgradeRetainedState {
	t.Helper()
	ctx.KVStore(testApp.GetKey("oracle")).Set([]byte("historical"), []byte("retained"))
	return offlineUpgradeRetainedState{}
}
