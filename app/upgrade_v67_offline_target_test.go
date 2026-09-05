//go:build upgrade_v67 && offline_upgrade && upgrade_target

package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/upgradespec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	xauthsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

var v67OfflineRemovedModules = upgradespec.V67RetiredModules()

var v67OfflineUpgradeBlockTime = time.Unix(1_700_000_000, 0).UTC()

const (
	v67OfflineCrashRootEnv        = "UPGRADE_TEST_CRASH_ROOT"
	v67OfflineCrashPointEnv       = "UPGRADE_TEST_CRASH_POINT"
	v67OfflineCrashInHandler      = "handler"
	v67OfflineCrashBeforeCommit   = "before_commit"
	v67OfflineCrashMarkerFile     = "upgrade-crash.json"
	v67OfflineCrashProcessTimeout = 2 * time.Minute
)

type v67OfflineCrashMarker struct {
	Point   string `json:"point"`
	Height  int64  `json:"height"`
	AppHash string `json:"app_hash,omitempty"`
}

func TestV67OfflineUpgradeTarget(t *testing.T) {
	testV67OfflineUpgradeTargetFixture(t)
}

func TestV67OfflineUpgradeSnapshot(t *testing.T) {
	testV67OfflineUpgradeTargetSnapshot(t)
}

// TestV67OfflineUpgradeCrashProcess terminates a child process at the selected
// uncommitted point in the v6.7 upgrade block.
func TestV67OfflineUpgradeCrashProcess(t *testing.T) {
	root := os.Getenv(v67OfflineCrashRootEnv)
	if root == "" {
		t.Skipf("%s is only set by the crash-replay parent", v67OfflineCrashRootEnv)
	}
	point := os.Getenv(v67OfflineCrashPointEnv)

	artifactRoot := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, artifactRoot)
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV67OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.SourceHeight, testApp.LastBlockHeight())

	switch point {
	case v67OfflineCrashInHandler:
		// Run the registered handler's body, then die before it can return.
		testApp.UpgradeKeeper.SetUpgradeHandler(artifact.Upgrade,
			func(ctx sdk.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
				if _, upgradeErr := testApp.applyV67Upgrade(ctx, fromVM); upgradeErr != nil {
					return nil, upgradeErr
				}
				terminateV67OfflineUpgradeProcess(t, root, v67OfflineCrashMarker{
					Point:  point,
					Height: ctx.BlockHeight(),
				})
				panic("process survived Kill")
			},
		)
		finalizeV67OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
		t.Fatal("upgrade handler returned after killing its process")
	case v67OfflineCrashBeforeCommit:
		response := finalizeV67OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
		require.NotEmpty(t, response.AppHash, "FinalizeBlock returned no application hash")
		terminateV67OfflineUpgradeProcess(t, root, v67OfflineCrashMarker{
			Point:   point,
			Height:  artifact.UpgradeHeight,
			AppHash: hex.EncodeToString(response.AppHash),
		})
		t.Fatal("process survived Kill")
	default:
		t.Fatalf("%s must be %q or %q, got %q",
			v67OfflineCrashPointEnv, v67OfflineCrashInHandler, v67OfflineCrashBeforeCommit, point)
	}
}

func testV67OfflineUpgradeTargetFixture(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, "v6.7", artifact.Upgrade)
	requireV67OfflineRetainedIdentities(t, artifact.Retained)
	require.Equal(t, artifact.SourceHeight+1, artifact.UpgradeHeight)

	t.Setenv("UPGRADE_VERSION_LIST", LatestUpgrade)

	cleanRoot := filepath.Join(root, offlineUpgradeMigratedDir)
	handlerCrashRoot := filepath.Join(root, "crash-handler")
	beforeCommitCrashRoot := filepath.Join(root, "crash-before-commit")
	copyOfflineUpgradeDatabase(t, root, cleanRoot)
	copyOfflineUpgradeDatabase(t, root, handlerCrashRoot)
	copyOfflineUpgradeDatabase(t, root, beforeCommitCrashRoot)

	cleanHash := applyV67OfflineUpgradeClean(t, cleanRoot, artifact)
	handlerCrashHash := applyV67OfflineUpgradeCrashReplay(
		t, handlerCrashRoot, artifact, v67OfflineCrashInHandler,
	)
	require.Equalf(t, cleanHash, handlerCrashHash,
		"in-handler crash replay diverged from the clean application hash: clean=%x crash-replay=%x",
		cleanHash, handlerCrashHash)
	beforeCommitCrashHash := applyV67OfflineUpgradeCrashReplay(
		t, beforeCommitCrashRoot, artifact, v67OfflineCrashBeforeCommit,
	)
	require.Equalf(t, cleanHash, beforeCommitCrashHash,
		"pre-commit crash replay diverged from the clean application hash: clean=%x crash-replay=%x",
		cleanHash, beforeCommitCrashHash)

	artifact.MigratedRoot = offlineUpgradeMigratedDir
	artifact.UpgradeHash = offlineUpgradeHashString(cleanHash)
	writeOfflineUpgradeArtifact(t, root, artifact)

	txRoot := filepath.Join(root, "post-upgrade-tx")
	copyOfflineUpgradeDatabase(t, cleanRoot, txRoot)
	requireV67OfflineDeliveredBankSend(t, txRoot, artifact.Retained)
}

func applyV67OfflineUpgradeClean(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV67OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, testApp),
		"v6.7 did not reopen the v6.6 module version map")
	requireV67OfflineRetainedStores(t, testApp, artifact)
	requireV67OfflineBankState(t, testApp, artifact.Retained)

	response := finalizeV67OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV67OfflineAppliedName(t, reopened, artifact)
	requireV67OfflineVersionMap(t, reopened, artifact.ModuleVersions)
	requireV67OfflineRetainedStores(t, reopened, artifact)
	requireV67OfflineBankState(t, reopened, artifact.Retained)
	requireV67OfflineVoucherSend(t, reopened, artifact.Retained)
	hash := committedOfflineUpgradeHash(t, reopened)
	require.Equal(t, response.AppHash, hash,
		"clean commit does not match the application hash returned by FinalizeBlock")
	return hash
}

func applyV67OfflineUpgradeCrashReplay(
	t *testing.T,
	root string,
	artifact offlineUpgradeArtifact,
	point string,
) []byte {
	t.Helper()
	before := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.SourceHeight, before.LastBlockHeight())
	sourceHash := committedOfflineUpgradeHash(t, before)
	closeOfflineUpgradeApp(t, before)

	marker := crashV67OfflineUpgradeProcess(t, root, artifact, point)

	interrupted := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.SourceHeight, interrupted.LastBlockHeight(),
		"process death before commit left the crash-replay database above the pre-upgrade height")
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, interrupted),
		"process death before commit mutated the pre-upgrade version map")
	require.Equal(t, sourceHash, committedOfflineUpgradeHash(t, interrupted),
		"process death before commit changed the committed application hash")
	replayed := finalizeV67OfflineUpgrade(t, interrupted, artifact.UpgradeHeight)
	if marker.AppHash != "" {
		announcedHash, err := hex.DecodeString(marker.AppHash)
		require.NoError(t, err, "crash marker contains an invalid application hash")
		require.Equal(t, announcedHash, replayed.AppHash,
			"replayed FinalizeBlock does not match the application hash announced before the crash")
	}
	commitOfflineUpgradeApp(t, interrupted)
	closeOfflineUpgradeApp(t, interrupted)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV67OfflineAppliedName(t, reopened, artifact)
	requireV67OfflineVersionMap(t, reopened, artifact.ModuleVersions)
	hash := committedOfflineUpgradeHash(t, reopened)
	require.Equal(t, replayed.AppHash, hash,
		"replayed commit does not match the application hash returned by FinalizeBlock")
	return hash
}

func crashV67OfflineUpgradeProcess(
	t *testing.T,
	root string,
	artifact offlineUpgradeArtifact,
	point string,
) v67OfflineCrashMarker {
	t.Helper()
	markerPath := filepath.Join(root, v67OfflineCrashMarkerFile)
	require.NoError(t, os.RemoveAll(markerPath))

	ctx, cancel := context.WithTimeout(context.Background(), v67OfflineCrashProcessTimeout)
	defer cancel()
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestV67OfflineUpgradeCrashProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		v67OfflineCrashRootEnv+"="+root,
		v67OfflineCrashPointEnv+"="+point,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, ctx.Err(), "crash child did not terminate itself:\n%s", output)
	require.Error(t, err, "crash child returned successfully:\n%s", output)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "crash child did not terminate as a process failure:\n%s", output)
	require.Equal(t, -1, exitErr.ExitCode(), "crash child exited normally instead of being killed:\n%s", output)

	encodedMarker, readErr := os.ReadFile(markerPath)
	require.NoError(t, readErr, "crash child exited before reaching %s:\n%s", point, output)
	var marker v67OfflineCrashMarker
	require.NoError(t, json.Unmarshal(encodedMarker, &marker), "decode crash marker")
	require.Equal(t, point, marker.Point)
	require.Equal(t, artifact.UpgradeHeight, marker.Height,
		"crash child reached its crash point at an unexpected height")
	return marker
}

func terminateV67OfflineUpgradeProcess(t *testing.T, root string, marker v67OfflineCrashMarker) {
	t.Helper()
	encoded, err := json.Marshal(marker)
	require.NoError(t, err)
	markerPath := filepath.Join(root, v67OfflineCrashMarkerFile)
	tempPath := markerPath + ".tmp"
	require.NoError(t, os.WriteFile(tempPath, encoded, 0o600))
	require.NoError(t, os.Rename(tempPath, markerPath))
	process, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, process.Kill())
	select {}
}

func requireV67OfflinePersistedPlanHasHandler(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found, "committed upgrade plan did not survive the process boundary")
	require.Equal(t, artifact.Upgrade, plan.Name)
	require.Equal(t, artifact.UpgradeHeight, plan.Height)
	require.Equal(t, LatestUpgrade, plan.Name)
	require.True(t, testApp.UpgradeKeeper.HasHandler(plan.Name),
		"target binary has no handler for persisted plan name %q", plan.Name)
}

func requireV67OfflineAppliedName(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(
		offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()))
	require.Equal(t, artifact.Upgrade, lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.True(t, testApp.UpgradeKeeper.HasHandler(lastName),
		"target binary has no handler for applied plan name %q", lastName)
}

func finalizeV67OfflineUpgrade(t *testing.T, testApp *App, height int64) *abci.ResponseFinalizeBlock {
	t.Helper()
	response, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID,
			Height:  height,
			Time:    v67OfflineUpgradeBlockTime,
		},
	})
	require.NoError(t, err)
	return response
}

func testV67OfflineUpgradeTargetSnapshot(t *testing.T) {
	home := requireOfflineUpgradeSnapshotHome(t)
	t.Setenv("UPGRADE_VERSION_LIST", "v6.7")
	chainID := readOfflineUpgradeGenesisChainID(t, home)

	testApp := openOfflineUpgradeSnapshotApp(t, home, chainID)
	sourceHeight := testApp.LastBlockHeight()
	readCtx := offlineUpgradeContext(testApp, sourceHeight, chainID)
	beforeVersions := offlineUpgradeModuleVersions(t, testApp)
	for _, module := range v67OfflineRemovedModules {
		require.Contains(t, beforeVersions, module,
			"%s is not a pre-v6.7 snapshot: module version map is missing %s", home, module)
	}
	require.Contains(t, beforeVersions, "oracle")
	beforeStores := snapshotOfflineUpgradeStores(t, testApp, readCtx, v67OfflineRemovedModules)

	require.True(t, testApp.UpgradeKeeper.HasHandler("v6.7"),
		"v6.7 upgrade handler is not registered; set UPGRADE_VERSION_LIST=v6.7")
	upgradeHeight := sourceHeight + 1
	upgradeCtx := offlineUpgradeContext(testApp, upgradeHeight, chainID)
	testApp.UpgradeKeeper.ApplyUpgrade(upgradeCtx, upgradetypes.Plan{
		Name:   "v6.7",
		Height: upgradeHeight,
	})
	testApp.CommitMultiStore().Commit(true)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeSnapshotApp(t, home, chainID)
	defer closeOfflineUpgradeApp(t, reopened)
	requireV67OfflineVersionMap(t, reopened, beforeVersions)
	requireOfflineUpgradeRetainedStores(t, reopened, beforeStores)
}

func requireV67OfflineVersionMap(t *testing.T, testApp *App, before []string) {
	t.Helper()
	after := offlineUpgradeModuleVersions(t, testApp)
	require.Equal(t, v67OfflineRemovedModules,
		offlineUpgradeDifference(before, after),
		"v6.7 removed an unexpected set of module versions")
	require.Contains(t, after, "oracle")
	require.True(t, offlineUpgradeHasModuleVersion(t, testApp, "oracle"),
		"upgrade store dropped the oracle version-map entry")
	for _, module := range v67OfflineRemovedModules {
		require.False(t, offlineUpgradeHasModuleVersion(t, testApp, module),
			"upgrade store still has a version-map entry for %s", module)
	}
}

func requireV67OfflineRetainedStores(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	requireOfflineUpgradeRetainedStores(t, testApp, artifact.Stores)
	for storeName, want := range artifact.Stores {
		requireV67OfflineRetainedStoreKeys(t, storeName, want, artifact.Retained)
	}
}

func requireV67OfflineRetainedIdentities(t *testing.T, retained offlineUpgradeRetainedState) {
	t.Helper()
	require.NotEmpty(t, retained.FeegrantGranter)
	require.NotEmpty(t, retained.FeegrantGrantee)
	require.NotEmpty(t, retained.FeegrantKey)
	require.NotEmpty(t, retained.CapabilityName)
	require.NotZero(t, retained.CapabilityIndex)
	require.NotEmpty(t, retained.CapabilityOwnersKey)
	require.NotEmpty(t, retained.IBCClientID)
	require.NotEmpty(t, retained.IBCClientStateKey)
	require.NotEmpty(t, retained.IBCConnectionID)
	require.NotEmpty(t, retained.IBCConnectionKey)
	require.NotEmpty(t, retained.IBCPortID)
	require.NotEmpty(t, retained.IBCChannelID)
	require.NotEmpty(t, retained.IBCChannelKey)
	require.NotEmpty(t, retained.TransferDenomHash)
	require.NotEmpty(t, retained.TransferIBCDenom)
	require.NotEmpty(t, retained.TransferTraceKey)
	require.NotEmpty(t, retained.EscrowAddress)
	require.NotEmpty(t, retained.EscrowAmount)
	require.NotEmpty(t, retained.EscrowSupply)
	require.NotEmpty(t, retained.VoucherHolder)
	require.NotEmpty(t, retained.VoucherAmount)
	require.NotEmpty(t, retained.VoucherSupply)
	require.NotEmpty(t, retained.TxSender)
	require.NotEmpty(t, retained.TxSenderKey)
	require.NotEmpty(t, retained.TxRecipient)
}

// requireV67OfflineBankState asserts the recorded IBC escrow and voucher bank
// balances and total supplies.
func requireV67OfflineBankState(t *testing.T, testApp *App, retained offlineUpgradeRetainedState) {
	t.Helper()
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())

	escrowAddr, err := sdk.AccAddressFromBech32(retained.EscrowAddress)
	require.NoError(t, err)
	escrowCoin := offlineUpgradeRecordedCoin(t, "usei", retained.EscrowAmount)
	require.Equal(t, escrowCoin, testApp.BankKeeper.GetBalance(ctx, escrowAddr, escrowCoin.Denom),
		"v6.7 changed the IBC escrow balance")
	require.Equal(t, offlineUpgradeRecordedCoin(t, "usei", retained.EscrowSupply),
		testApp.BankKeeper.GetSupply(ctx, "usei"),
		"v6.7 changed usei total supply; escrowed coins must remain counted")

	holder, err := sdk.AccAddressFromBech32(retained.VoucherHolder)
	require.NoError(t, err)
	voucherCoin := offlineUpgradeRecordedCoin(t, retained.TransferIBCDenom, retained.VoucherAmount)
	require.Equal(t, voucherCoin, testApp.BankKeeper.GetBalance(ctx, holder, voucherCoin.Denom),
		"v6.7 changed the IBC voucher holder balance")
	require.Equal(t, offlineUpgradeRecordedCoin(t, retained.TransferIBCDenom, retained.VoucherSupply),
		testApp.BankKeeper.GetSupply(ctx, voucherCoin.Denom),
		"v6.7 changed IBC voucher total supply")
}

// requireV67OfflineVoucherSend sends the recorded IBC voucher from its holder to
// another account through the bank keeper.
func requireV67OfflineVoucherSend(t *testing.T, testApp *App, retained offlineUpgradeRetainedState) {
	t.Helper()
	ctx, _ := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()).CacheContext()
	holder, err := sdk.AccAddressFromBech32(retained.VoucherHolder)
	require.NoError(t, err)
	voucher := offlineUpgradeRecordedCoin(t, retained.TransferIBCDenom, retained.VoucherAmount)
	require.True(t, voucher.Amount.GT(sdk.OneInt()), "voucher amount is too small to send")

	recipient := sdk.AccAddress("v67-voucher-receiver")
	testApp.AccountKeeper.SetAccount(ctx, testApp.AccountKeeper.NewAccountWithAddress(ctx, recipient))
	send := sdk.NewCoin(voucher.Denom, sdk.OneInt())
	require.NoError(t, testApp.BankKeeper.SendCoins(ctx, holder, recipient, sdk.NewCoins(send)),
		"bank send of an IBC voucher failed after v6.7")
	require.Equal(t, send, testApp.BankKeeper.GetBalance(ctx, recipient, voucher.Denom),
		"bank send of an IBC voucher did not credit the recipient")
	require.Equal(t, voucher.Sub(send), testApp.BankKeeper.GetBalance(ctx, holder, voucher.Denom),
		"bank send of an IBC voucher did not debit the holder")
	require.Equal(t, offlineUpgradeRecordedCoin(t, voucher.Denom, retained.VoucherSupply),
		testApp.BankKeeper.GetSupply(ctx, voucher.Denom),
		"sending an IBC voucher changed its total supply")
}

func offlineUpgradeRecordedCoin(t *testing.T, denom, amount string) sdk.Coin {
	t.Helper()
	parsed, ok := sdk.NewIntFromString(amount)
	require.True(t, ok, "invalid recorded amount %q for denom %s", amount, denom)
	return sdk.NewCoin(denom, parsed)
}

func requireV67OfflineRetainedStoreKeys(
	t *testing.T,
	storeName string,
	snapshot map[string]string,
	retained offlineUpgradeRetainedState,
) {
	t.Helper()
	switch storeName {
	case "feegrant":
		requireOfflineUpgradeStoreKey(t, snapshot, "feegrant allowance", retained.FeegrantKey)
	case "capability":
		requireOfflineUpgradeStoreKey(t, snapshot, "capability owner set", retained.CapabilityOwnersKey)
	case "ibc":
		requireOfflineUpgradeStoreKey(t, snapshot, "IBC client "+retained.IBCClientID, retained.IBCClientStateKey)
		requireOfflineUpgradeStoreKey(t, snapshot, "IBC connection "+retained.IBCConnectionID, retained.IBCConnectionKey)
		requireOfflineUpgradeStoreKey(t, snapshot, "IBC channel "+retained.IBCPortID+"/"+retained.IBCChannelID, retained.IBCChannelKey)
	case "transfer":
		requireOfflineUpgradeStoreKey(t, snapshot, "transfer denom trace "+retained.TransferIBCDenom, retained.TransferTraceKey)
	}
}

const (
	v67OfflinePostUpgradeSendAmt int64 = 4242
	v67OfflinePostUpgradeFee     int64 = 200000
)

// requireV67OfflineDeliveredBankSend delivers a signed bank send against the
// migrated database and requires that the committed balances and sequence moved.
func requireV67OfflineDeliveredBankSend(t *testing.T, root string, retained offlineUpgradeRetainedState) {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	upgradeHeight := testApp.LastBlockHeight()

	privBytes, err := hex.DecodeString(retained.TxSenderKey)
	require.NoError(t, err)
	priv := &secp256k1.PrivKey{Key: privBytes}
	sender, err := sdk.AccAddressFromBech32(retained.TxSender)
	require.NoError(t, err)
	require.Equal(t, sender, sdk.AccAddress(priv.PubKey().Address()),
		"recorded sender key does not match recorded sender address")
	recipient, err := sdk.AccAddressFromBech32(retained.TxRecipient)
	require.NoError(t, err)

	beforeCtx := offlineUpgradeReadContext(testApp, upgradeHeight)
	senderBefore := testApp.BankKeeper.GetBalance(beforeCtx, sender, "usei")
	recvBefore := testApp.BankKeeper.GetBalance(beforeCtx, recipient, "usei")
	acc := testApp.AccountKeeper.GetAccount(beforeCtx, sender)
	require.NotNil(t, acc, "sender account is missing from the migrated database")
	seqBefore := acc.GetSequence()

	txBz := signV67OfflineBankSend(t, testApp, priv, recipient, v67OfflinePostUpgradeSendAmt, v67OfflinePostUpgradeFee)
	res, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade-tx"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID,
			Height:  upgradeHeight + 1,
			Time:    v67OfflineUpgradeBlockTime.Add(time.Second),
		},
		Txs: [][]byte{txBz},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 1)
	require.Equal(t, uint32(abci.CodeTypeOK), res.TxResults[0].Code, res.TxResults[0].Log)
	require.Positive(t, res.TxResults[0].GasUsed)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, upgradeHeight+1, reopened.LastBlockHeight())
	afterCtx := offlineUpgradeReadContext(reopened, reopened.LastBlockHeight())
	require.Equal(t, recvBefore.Add(sdk.NewInt64Coin("usei", v67OfflinePostUpgradeSendAmt)),
		reopened.BankKeeper.GetBalance(afterCtx, recipient, "usei"),
		"delivered bank send did not credit the recipient")
	require.Equal(t, senderBefore.Sub(sdk.NewInt64Coin("usei", v67OfflinePostUpgradeSendAmt+v67OfflinePostUpgradeFee)),
		reopened.BankKeeper.GetBalance(afterCtx, sender, "usei"),
		"delivered bank send did not debit the sender including the fee")
	require.Equal(t, seqBefore+1,
		reopened.AccountKeeper.GetAccount(afterCtx, sender).GetSequence(),
		"delivered bank send did not advance the sender sequence")
}

func signV67OfflineBankSend(t *testing.T, testApp *App, priv *secp256k1.PrivKey, to sdk.AccAddress, amount, fee int64) []byte {
	t.Helper()
	from := sdk.AccAddress(priv.PubKey().Address())
	txConfig := testApp.GetTxConfig()
	txBuilder := txConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(banktypes.NewMsgSend(from, to, sdk.NewCoins(sdk.NewInt64Coin("usei", amount)))))
	txBuilder.SetGasLimit(1_000_000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", fee)))

	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	acc := testApp.AccountKeeper.GetAccount(ctx, from)
	require.NotNil(t, acc, "sender account is missing from the migrated database")

	signerData := xauthsigning.SignerData{
		ChainID:       offlineUpgradeChainID,
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}
	sigData := signing.SingleSignatureData{
		SignMode:  txConfig.SignModeHandler().DefaultMode(),
		Signature: nil,
	}
	sig := signing.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &sigData,
		Sequence: acc.GetSequence(),
	}
	require.NoError(t, txBuilder.SetSignatures(sig))
	bytesToSign, err := txConfig.SignModeHandler().GetSignBytes(txConfig.SignModeHandler().DefaultMode(), signerData, txBuilder.GetTx())
	require.NoError(t, err)
	sigBytes, err := priv.Sign(bytesToSign)
	require.NoError(t, err)
	sigData = signing.SingleSignatureData{
		SignMode:  txConfig.SignModeHandler().DefaultMode(),
		Signature: sigBytes,
	}
	sig = signing.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &sigData,
		Sequence: acc.GetSequence(),
	}
	require.NoError(t, txBuilder.SetSignatures(sig))
	bz, err := txConfig.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	return bz
}
