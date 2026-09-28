//go:build upgrade_v68 && offline_upgrade && upgrade_source

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

var v68OfflineSourceStores = []string{"bank"}

func TestV68OfflineUpgradeSource(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "source")
	testApp := openOfflineUpgradeApp(t, root, true)
	ctx := testApp.GetContextForDeliverTx(nil).WithBlockTime(time.Now().UTC())
	retained := seedV68OfflineUpgradeState(t, testApp, ctx)
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
	storeNames := make([]string, 0, len(v68OfflineSourceStores))
	for _, name := range v68OfflineSourceStores {
		if testApp.GetKey(name) == nil {
			continue
		}
		storeNames = append(storeNames, name)
	}
	stores := snapshotOfflineUpgradeStores(t, testApp, ctx, storeNames)
	closeOfflineUpgradeApp(t, testApp)
	writeOfflineUpgradeArtifact(t, root, offlineUpgradeArtifact{
		Upgrade: plan.Name, SourceHeight: sourceHeight, UpgradeHeight: upgradeHeight,
		ModuleVersions: moduleVersions, Stores: stores, Retained: retained,
	})
	requireV68OfflineUnupgradedHalt(t, root, sourceHeight, upgradeHeight)
	copyV68OfflineUpgradeInfo(t, root, upgradeHeight)
}

// TestV68OfflineUpgradeReopen verifies that v6.7 cannot reopen a database whose Oracle tree v6.8 deleted.
func TestV68OfflineUpgradeReopen(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "reopen")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, "v6.8", artifact.Upgrade)
	require.NotEmpty(t, artifact.UpgradeHash)
	migrated := offlineUpgradeMigratedDatabase(t, root, artifact)
	reopenRoot := filepath.Join(root, "reopen")
	copyOfflineUpgradeDatabase(t, migrated, reopenRoot)

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		testApp := openOfflineUpgradeApp(t, reopenRoot, false)
		closeOfflineUpgradeApp(t, testApp)
	}()
	require.NotNil(t, recovered,
		"v6.7 binary reopened a database whose oracle tree was deleted")
	require.Contains(t, fmt.Sprint(recovered), `store "oracle"`)
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

func copyV68OfflineUpgradeInfo(t *testing.T, root string, upgradeHeight int64) {
	t.Helper()
	source := filepath.Join(root, "unupgraded-halt", "home", "data", "upgrade-info.json")
	info, err := os.Stat(source)
	require.NoError(t, err)
	require.False(t, info.IsDir())
	data, err := os.ReadFile(source)
	require.NoError(t, err)
	var upgradeInfo struct {
		Name   string `json:"name"`
		Height int64  `json:"height"`
	}
	require.NoError(t, json.Unmarshal(data, &upgradeInfo))
	require.Equal(t, "v6.8", upgradeInfo.Name)
	require.Equal(t, upgradeHeight, upgradeInfo.Height)
	target := filepath.Join(root, "home", "data", "upgrade-info.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	copyOfflineUpgradeFile(t, source, target)
}

func seedV68OfflineUpgradeState(t *testing.T, testApp *App, ctx sdk.Context) offlineUpgradeRetainedState {
	t.Helper()
	ctx.KVStore(testApp.GetKey("oracle")).Set([]byte("historical"), []byte("retained"))
	return offlineUpgradeRetainedState{}
}
