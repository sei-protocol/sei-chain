//go:build upgrade_v69 && offline_upgrade && upgrade_source

package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

const (
	v69OfflineUpgradeName          = "v6.9"
	v69OfflineAuthzStore           = "authz"
	v69OfflineAccountAmount  int64 = 1_000_000_000
	v69OfflineGrantSpendable int64 = 1_000_000
)

// v69OfflineSourceStores are the retained stores the source phase snapshots so
// the target phase can prove v6.9 left them as v6.8 committed them.
var v69OfflineSourceStores = []string{authtypes.StoreKey, "bank", "gov", "params", "upgrade"}

func TestV69OfflineUpgradeSource(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "source")
	testApp := openOfflineUpgradeApp(t, root, true)
	ctx := testApp.GetContextForDeliverTx(nil).WithBlockTime(time.Now().UTC())
	retained := seedV69OfflineAuthzGrant(t, testApp, ctx)
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
	require.Contains(t, moduleVersions, v69OfflineAuthzStore, "v6.8 module version map does not contain authz")
	require.NotEmpty(t, snapshotCommittedOfflineUpgradeStore(t, testApp, v69OfflineAuthzStore),
		"v6.8 did not commit the authz grant")
	stores := snapshotOfflineUpgradeStores(t, testApp, ctx, v69OfflineSourceStores)
	closeOfflineUpgradeApp(t, testApp)
	writeOfflineUpgradeArtifact(t, root, offlineUpgradeArtifact{
		Upgrade: plan.Name, SourceHeight: sourceHeight, UpgradeHeight: upgradeHeight,
		ModuleVersions: moduleVersions, Stores: stores, Retained: retained,
	})
	requireV69OfflineUnupgradedHalt(t, root, sourceHeight, upgradeHeight)
	copyV69OfflineUpgradeInfo(t, root, upgradeHeight)
}

// TestV69OfflineUpgradeReopen verifies that v6.8 cannot reopen a database whose
// authz tree v6.9 deleted.
func TestV69OfflineUpgradeReopen(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "reopen")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, v69OfflineUpgradeName, artifact.Upgrade)
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
	require.NotNil(t, recovered, "v6.8 binary reopened a database whose authz tree was deleted")
	require.Contains(t, fmt.Sprint(recovered), fmt.Sprintf("store %q", v69OfflineAuthzStore),
		"panic does not name the deleted authz store")
}

// seedV69OfflineAuthzGrant funds a granter and a grantee and has the granter
// grant the grantee a send authorization through the v6.8 authz keeper. The
// grantee is recorded as the sender of a post-upgrade MsgExec that spends the
// grant, and a third account as the recipient of the send it wraps.
func seedV69OfflineAuthzGrant(t *testing.T, testApp *App, ctx sdk.Context) offlineUpgradeRetainedState {
	t.Helper()
	granter := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	granteeKey := secp256k1.GenPrivKey()
	grantee := sdk.AccAddress(granteeKey.PubKey().Address())
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	for _, address := range []sdk.AccAddress{granter, grantee, recipient} {
		testApp.AccountKeeper.SetAccount(ctx, testApp.AccountKeeper.NewAccountWithAddress(ctx, address))
	}
	funds := sdk.NewCoins(sdk.NewInt64Coin("usei", v69OfflineAccountAmount))
	for _, address := range []sdk.AccAddress{granter, grantee} {
		require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, funds))
		require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, address, funds))
	}

	spendLimit := sdk.NewCoins(sdk.NewInt64Coin("usei", v69OfflineGrantSpendable))
	require.NoError(t, testApp.AuthzKeeper.SaveGrant(ctx, grantee, granter,
		banktypes.NewSendAuthorization(spendLimit), ctx.BlockTime().AddDate(1, 0, 0)))
	authorization, _ := testApp.AuthzKeeper.GetCleanAuthorization(ctx, grantee, granter, sdk.MsgTypeURL(&banktypes.MsgSend{}))
	require.NotNil(t, authorization, "v6.8 does not serve the send authorization it saved")

	return offlineUpgradeRetainedState{
		AuthzGranter: granter.String(),
		TxSender:     grantee.String(),
		TxSenderKey:  hex.EncodeToString(granteeKey.Bytes()),
		TxRecipient:  recipient.String(),
	}
}

// requireV69OfflineUnupgradedHalt drives a copy of the pre-upgrade database
// through the v6.9 plan height.
func requireV69OfflineUnupgradedHalt(t *testing.T, root string, sourceHeight, upgradeHeight int64) {
	t.Helper()
	haltRoot := filepath.Join(root, "unupgraded-halt")
	copyOfflineUpgradeDatabase(t, root, haltRoot)

	testApp := openOfflineUpgradeApp(t, haltRoot, false)
	require.Equal(t, sourceHeight, testApp.LastBlockHeight())
	require.False(t, testApp.UpgradeKeeper.HasHandler(v69OfflineUpgradeName), "v6.8 registered a v6.9 upgrade handler")

	var panicked any
	func() {
		defer func() { panicked = recover() }()
		_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Hash: []byte("offline-upgrade-unupgraded-halt"),
			Header: &tmproto.Header{
				ChainID: offlineUpgradeChainID,
				Height:  upgradeHeight,
			},
		})
		require.NoError(t, err, "v6.8 returned from FinalizeBlock without panicking")
	}()
	require.NotNil(t, panicked, "v6.8 produced the v6.9 plan height")
	msg := fmt.Sprint(panicked)
	require.Contains(t, msg, `UPGRADE "v6.9" NEEDED`, "halt panic is missing the upgrade name: %v", panicked)
	require.Contains(t, msg, fmt.Sprintf("height: %d", upgradeHeight),
		"halt panic is missing plan height %d: %v", upgradeHeight, panicked)
	require.Equal(t, sourceHeight, testApp.LastBlockHeight(), "v6.8 committed the v6.9 plan height")
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, haltRoot, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, sourceHeight, reopened.LastBlockHeight(),
		"v6.8 left committed state behind after halting at the v6.9 plan height")
}

func copyV69OfflineUpgradeInfo(t *testing.T, root string, upgradeHeight int64) {
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
	require.Equal(t, v69OfflineUpgradeName, upgradeInfo.Name)
	require.Equal(t, upgradeHeight, upgradeInfo.Height)
	target := filepath.Join(root, "home", "data", "upgrade-info.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	copyOfflineUpgradeFile(t, source, target)
}
