//go:build upgrade_v69 && offline_upgrade && upgrade_source

package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

const v69OfflineUpgradeName = "v6.9"

// Two native pointers whose denoms share a registry prefix. v6.8 resolves the shorter
// denom to the longer denom's pointer.
const (
	v69OfflineShortDenom = "factory/sei1v69offline/pamp"
	v69OfflineLongDenom  = v69OfflineShortDenom + "/vest"
)

var (
	v69OfflineShortPointer = common.HexToAddress("0x00000000000000000000000000000000000069a1")
	v69OfflineLongPointer  = common.HexToAddress("0x00000000000000000000000000000000000069a2")

	v69OfflineRetiredParamKey = []byte(evmtypes.ModuleName + "/KeyRegisterPointerDisabled")
	v69OfflineSourceStores    = []string{evmtypes.StoreKey, "gov", paramstypes.StoreKey, upgradetypes.StoreKey}
)

func TestV69OfflineUpgradeSource(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "source")
	testApp := openOfflineUpgradeApp(t, root, true)
	ctx := testApp.GetContextForDeliverTx(nil).WithBlockTime(time.Now().UTC())
	require.NoError(t, testApp.EvmKeeper.SetERC20NativePointer(ctx, v69OfflineShortDenom, v69OfflineShortPointer))
	require.NoError(t, testApp.EvmKeeper.SetERC20NativePointer(ctx, v69OfflineLongDenom, v69OfflineLongPointer))
	pointer, _, exists := testApp.EvmKeeper.GetERC20NativePointer(ctx, v69OfflineShortDenom)
	require.True(t, exists)
	require.Equal(t, v69OfflineLongPointer, pointer,
		"v6.8 no longer resolves a denom to the pointer of a longer denom sharing its prefix")
	// EVM migration 18 stored true on every live chain.
	params := testApp.EvmKeeper.GetParams(ctx)
	params.RegisterPointerDisabled = true
	testApp.EvmKeeper.SetParams(ctx, params)
	require.Equal(t, []byte("true"), ctx.KVStore(testApp.GetKey(paramstypes.StoreKey)).Get(v69OfflineRetiredParamKey))

	upgradeHeight := ctx.BlockHeight() + 2
	require.NoError(t, testApp.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{
		Name: v69OfflineUpgradeName, Height: upgradeHeight,
	}))
	commitOfflineUpgradeApp(t, testApp)
	sourceHeight := testApp.LastBlockHeight()
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found, "scheduled upgrade plan was not committed")
	require.Equal(t, v69OfflineUpgradeName, plan.Name)
	require.Equal(t, upgradeHeight, plan.Height)
	moduleVersions := offlineUpgradeModuleVersions(t, testApp)
	stores := snapshotOfflineUpgradeStores(t, testApp, ctx, v69OfflineSourceStores)
	closeOfflineUpgradeApp(t, testApp)
	writeOfflineUpgradeArtifact(t, root, offlineUpgradeArtifact{
		Upgrade: plan.Name, SourceHeight: sourceHeight, UpgradeHeight: upgradeHeight,
		ModuleVersions: moduleVersions, Stores: stores,
	})
}

// TestV69OfflineUpgradeReopen requires v6.8 to reopen the database v6.9 migrated, since
// v6.9 deletes no store, and to read the EVM parameters and native pointers it stored.
func TestV69OfflineUpgradeReopen(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "reopen")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, v69OfflineUpgradeName, artifact.Upgrade)
	require.NotEmpty(t, artifact.UpgradeHash)
	migrated := offlineUpgradeMigratedDatabase(t, root, artifact)
	reopenRoot := filepath.Join(root, "reopen")
	copyOfflineUpgradeDatabase(t, migrated, reopenRoot)

	testApp := openOfflineUpgradeApp(t, reopenRoot, false)
	defer closeOfflineUpgradeApp(t, testApp)
	require.Equal(t, artifact.UpgradeHeight, testApp.LastBlockHeight())
	require.Equal(t, artifact.UpgradeHash, offlineUpgradeHashString(committedOfflineUpgradeHash(t, testApp)))
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	require.False(t, testApp.EvmKeeper.GetParams(ctx).RegisterPointerDisabled,
		"v6.8 still reads the register_pointer_disabled value v6.9 deleted")
	pointer, _, exists := testApp.EvmKeeper.GetERC20NativePointer(ctx, v69OfflineLongDenom)
	require.True(t, exists)
	require.Equal(t, v69OfflineLongPointer, pointer)
}
