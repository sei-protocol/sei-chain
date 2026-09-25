//go:build upgrade_v68 && offline_upgrade && upgrade_source

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	retiredibcgov "github.com/sei-protocol/sei-chain/app/retiredibc/gov"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

var v68OfflineSourceStores = []string{"bank", "gov", "upgrade"}

// v68OfflineDeletedStores are the stores v6.8 deletes; the source phase writes
// a key into each so the target phase can prove the trees are gone.
var v68OfflineDeletedStores = []string{"capability", "ibc", "oracle", "transfer"}

const (
	v68OfflineVoucherDenom              = "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2"
	v68OfflineVoucherAmount       int64 = 1_234_567
	v68OfflineProposalTitle             = "Recover client 07-tendermint-0"
	v68OfflineProposalDescription       = "Substitute the expired client with 07-tendermint-1"
)

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
	// v6.7 dropped the IBC module versions while keeping their stores mounted;
	// only oracle still carries a version into v6.8.
	require.Contains(t, moduleVersions, "oracle")
	for _, name := range []string{"capability", "ibc", "transfer"} {
		require.NotContains(t, moduleVersions, name)
		require.NotNil(t, testApp.GetKey(name))
	}
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

// TestV68OfflineUpgradeReopen verifies that v6.7 cannot reopen a database whose
// oracle and IBC trees v6.8 deleted.
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
		"v6.7 binary reopened a database whose oracle and IBC trees were deleted")
	message := fmt.Sprint(recovered)
	namesDeletedStore := false
	for _, name := range v68OfflineDeletedStores {
		if strings.Contains(message, fmt.Sprintf("store %q", name)) {
			namesDeletedStore = true
			break
		}
	}
	require.True(t, namesDeletedStore, "panic does not name a deleted store: %s", message)
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

// seedV68OfflineUpgradeState writes a key into every store v6.8 deletes, a
// retired IBC governance proposal, an upgraded IBC client record in the upgrade
// store, and an IBC voucher balance that must survive the upgrade.
func seedV68OfflineUpgradeState(t *testing.T, testApp *App, ctx sdk.Context) offlineUpgradeRetainedState {
	t.Helper()
	for _, name := range v68OfflineDeletedStores {
		ctx.KVStore(testApp.GetKey(name)).Set([]byte("historical"), []byte("retained"))
	}
	var retained offlineUpgradeRetainedState
	seedV68IBCProposal(t, testApp, ctx, &retained)
	seedV68UpgradedIBCState(t, testApp, ctx, &retained)
	seedV68Voucher(t, testApp, ctx, &retained)
	return retained
}

func seedV68IBCProposal(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	content := &retiredibcgov.ClientUpdateProposal{
		Title:              v68OfflineProposalTitle,
		Description:        v68OfflineProposalDescription,
		SubjectClientId:    "07-tendermint-0",
		SubstituteClientId: "07-tendermint-1",
	}
	proposalID, err := testApp.GovKeeper.GetProposalID(ctx)
	require.NoError(t, err)
	proposal, err := govtypes.NewProposal(content, proposalID, ctx.BlockTime(), ctx.BlockTime().Add(time.Hour), false)
	require.NoError(t, err)
	proposal.Status = govtypes.StatusPassed
	testApp.GovKeeper.SetProposal(ctx, proposal)
	testApp.GovKeeper.SetProposalID(ctx, proposalID+1)
	stored, found := testApp.GovKeeper.GetProposal(ctx, proposalID)
	require.True(t, found)
	require.Equal(t, retiredibcgov.ClientUpdateProposalTypeURL, stored.Content.TypeUrl)
	retained.IBCProposalID = proposalID
	retained.IBCProposalTitle = v68OfflineProposalTitle
	retained.IBCProposalDescription = v68OfflineProposalDescription
}

func seedV68UpgradedIBCState(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	key := upgradetypes.UpgradedClientKey(ctx.BlockHeight() + 1000)
	ctx.KVStore(testApp.GetKey(upgradetypes.StoreKey)).Set(key, []byte("upgraded-client"))
	retained.UpgradedIBCStateKey = encodeOfflineUpgradeKey(key)
}

func seedV68Voucher(t *testing.T, testApp *App, ctx sdk.Context, retained *offlineUpgradeRetainedState) {
	t.Helper()
	holder := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	testApp.AccountKeeper.SetAccount(ctx, testApp.AccountKeeper.NewAccountWithAddress(ctx, holder))
	voucher := sdk.NewInt64Coin(v68OfflineVoucherDenom, v68OfflineVoucherAmount)
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, "transfer", sdk.NewCoins(voucher)))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "transfer", holder, sdk.NewCoins(voucher)))
	require.Equal(t, voucher, testApp.BankKeeper.GetBalance(ctx, holder, voucher.Denom))
	retained.TransferIBCDenom = voucher.Denom
	retained.VoucherHolder = holder.String()
	retained.VoucherAmount = voucher.Amount.String()
	retained.VoucherSupply = testApp.BankKeeper.GetSupply(ctx, voucher.Denom).Amount.String()
}
