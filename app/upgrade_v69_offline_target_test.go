//go:build upgrade_v69 && offline_upgrade && upgrade_target

package app

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

const v69OfflineUpgradeName = "v6.9"

// The native pointers the source phase registers, whose denoms share a registry prefix.
const (
	v69OfflineShortDenom = "factory/sei1v69offline/pamp"
	v69OfflineLongDenom  = v69OfflineShortDenom + "/vest"
)

var (
	v69OfflineShortPointer = common.HexToAddress("0x00000000000000000000000000000000000069a1")
	v69OfflineLongPointer  = common.HexToAddress("0x00000000000000000000000000000000000069a2")

	v69OfflineRetiredParamKey  = []byte(evmtypes.ModuleName + "/KeyRegisterPointerDisabled")
	v69OfflineUpgradeBlockTime = time.Unix(1_700_000_000, 0).UTC()
)

func TestV69OfflineUpgradeTarget(t *testing.T) {
	t.Run("fixture", testV69OfflineUpgradeTargetFixture)
	t.Run("snapshot", testV69OfflineUpgradeTargetSnapshot)
}

func testV69OfflineUpgradeTargetFixture(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, v69OfflineUpgradeName, artifact.Upgrade)
	require.Equal(t, artifact.SourceHeight+1, artifact.UpgradeHeight)

	t.Setenv("UPGRADE_VERSION_LIST", LatestUpgrade)

	cleanRoot := filepath.Join(root, offlineUpgradeMigratedDir)
	copyOfflineUpgradeDatabase(t, root, cleanRoot)
	testApp := openOfflineUpgradeApp(t, cleanRoot, false)
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found, "committed upgrade plan did not survive the process boundary")
	require.Equal(t, artifact.Upgrade, plan.Name)
	require.Equal(t, artifact.UpgradeHeight, plan.Height)
	require.True(t, testApp.UpgradeKeeper.HasHandler(plan.Name),
		"target binary has no handler for persisted plan name %q", plan.Name)
	finalizeV69OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, cleanRoot, false)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV69OfflineMigrated(t, reopened, artifact)
	hash := committedOfflineUpgradeHash(t, reopened)
	closeOfflineUpgradeApp(t, reopened)

	artifact.MigratedRoot = offlineUpgradeMigratedDir
	artifact.UpgradeHash = offlineUpgradeHashString(hash)
	writeOfflineUpgradeArtifact(t, root, artifact)
}

// requireV69OfflineMigrated checks a reopened migrated database: v6.9 is recorded as
// applied, register_pointer_disabled is gone, both native pointers resolve to their own
// denom, and nothing else in the pointer registry, the module version map or the params,
// gov and upgrade stores changed.
func requireV69OfflineMigrated(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(ctx)
	require.Equal(t, artifact.Upgrade, lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, testApp))
	require.Nil(t, ctx.KVStore(testApp.GetKey(paramstypes.StoreKey)).Get(v69OfflineRetiredParamKey),
		"v6.9 left register_pointer_disabled in the params store")
	for denom, want := range map[string]common.Address{
		v69OfflineShortDenom: v69OfflineShortPointer,
		v69OfflineLongDenom:  v69OfflineLongPointer,
	} {
		pointer, _, exists := testApp.EvmKeeper.GetERC20NativePointer(ctx, denom)
		require.True(t, exists, denom)
		require.Equal(t, want, pointer, denom)
	}
	requireOfflineUpgradeRetainedStoresExcept(t, testApp, artifact.Stores, v69OfflineTouchedKey)
}

// v69OfflineTouchedKey reports the retained-store keys the v6.9 upgrade block is specified
// to change: the deleted register_pointer_disabled parameter, the plan and done entries of
// the upgrade store, and the keys every block rewrites, which are gov's deadline boundary
// time and the EVM keys outside the pointer registry.
func v69OfflineTouchedKey(storeName string, key []byte) bool {
	switch storeName {
	case paramstypes.StoreKey:
		return bytes.Equal(key, v69OfflineRetiredParamKey)
	case govtypes.StoreKey:
		return bytes.Equal(key, govtypes.DeadlineBoundaryBlockTimeKey)
	case upgradetypes.StoreKey:
		return len(key) > 0 && (key[0] == upgradetypes.PlanByte || key[0] == upgradetypes.DoneByte)
	case evmtypes.StoreKey:
		for _, registry := range [][]byte{
			evmtypes.PointerRegistryPrefix, evmtypes.PointerCWCodePrefix, evmtypes.PointerReverseRegistryPrefix,
		} {
			if bytes.HasPrefix(key, registry) {
				return false
			}
		}
		return true
	}
	return false
}

func finalizeV69OfflineUpgrade(t *testing.T, testApp *App, height int64) {
	t.Helper()
	_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID, Height: height, Time: v69OfflineUpgradeBlockTime,
		},
	})
	require.NoError(t, err)
}

// testV69OfflineUpgradeTargetSnapshot applies v6.9 to the node home named by
// UPGRADE_TEST_SNAPSHOT_HOME and requires the module version map to be unchanged and
// register_pointer_disabled to be gone.
func testV69OfflineUpgradeTargetSnapshot(t *testing.T) {
	home := requireOfflineUpgradeSnapshotHome(t)
	t.Setenv("UPGRADE_VERSION_LIST", v69OfflineUpgradeName)
	chainID := readOfflineUpgradeGenesisChainID(t, home)

	testApp := openOfflineUpgradeSnapshotApp(t, home, chainID)
	sourceHeight := testApp.LastBlockHeight()
	beforeVersions := testApp.UpgradeKeeper.GetModuleVersionMap(offlineUpgradeContext(testApp, sourceHeight, chainID))
	upgradeHeight := sourceHeight + 1
	testApp.UpgradeKeeper.ApplyUpgrade(offlineUpgradeContext(testApp, upgradeHeight, chainID), upgradetypes.Plan{
		Name:   v69OfflineUpgradeName,
		Height: upgradeHeight,
	})
	testApp.CommitMultiStore().Commit(true)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeSnapshotApp(t, home, chainID)
	defer closeOfflineUpgradeApp(t, reopened)
	ctx := offlineUpgradeContext(reopened, reopened.LastBlockHeight(), chainID)
	require.Equal(t, beforeVersions, reopened.UpgradeKeeper.GetModuleVersionMap(ctx))
	require.Nil(t, ctx.KVStore(reopened.GetKey(paramstypes.StoreKey)).Get(v69OfflineRetiredParamKey),
		"v6.9 left register_pointer_disabled in the params store")
}
