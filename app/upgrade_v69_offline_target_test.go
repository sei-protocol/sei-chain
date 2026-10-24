//go:build upgrade_v69 && offline_upgrade && upgrade_target

package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/retiredauthz"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/address"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	xauthsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

const (
	v69OfflineUpgradeName          = "v6.9"
	v69OfflinePostUpgradeFee int64 = 200000
)

var v69OfflineUpgradeBlockTime = time.Unix(1_700_000_000, 0).UTC()

func TestV69OfflineUpgradeTarget(t *testing.T) {
	t.Run("fixture", testV69OfflineUpgradeTargetFixture)
	t.Run("snapshot", testV69OfflineUpgradeTargetSnapshot)
}

func testV69OfflineUpgradeTargetFixture(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, v69OfflineUpgradeName, artifact.Upgrade)
	require.NotEmpty(t, artifact.Retained.AuthzGranter)
	require.NotEmpty(t, artifact.Retained.TxSender)
	require.NotEmpty(t, artifact.Retained.TxSenderKey)
	require.NotEmpty(t, artifact.Retained.TxRecipient)
	require.Equal(t, artifact.SourceHeight+1, artifact.UpgradeHeight)

	t.Setenv("UPGRADE_VERSION_LIST", LatestUpgrade)

	cleanRoot := filepath.Join(root, offlineUpgradeMigratedDir)
	crashRoot := filepath.Join(root, "crash")
	noUpgradeInfoRoot := filepath.Join(root, "no-upgrade-info")
	copyOfflineUpgradeDatabase(t, root, cleanRoot)
	copyOfflineUpgradeDatabase(t, root, crashRoot)
	copyOfflineUpgradeDatabase(t, root, noUpgradeInfoRoot)

	requireV69OfflineRefusesRetainedStore(t, noUpgradeInfoRoot, artifact)

	cleanHash := applyV69OfflineUpgradeClean(t, cleanRoot, artifact)
	crashHash := applyV69OfflineUpgradeCrashReplay(t, crashRoot, artifact)
	require.Equalf(t, cleanHash, crashHash,
		"crash-replay application hash diverged from the clean single-pass hash: clean=%x crash-replay=%x",
		cleanHash, crashHash)
	recoveredHash := applyV69OfflineUpgradeAfterRefusal(t, noUpgradeInfoRoot, artifact)
	require.Equalf(t, cleanHash, recoveredHash,
		"the upgrade after restoring upgrade-info.json diverged from the clean hash: clean=%x recovered=%x",
		cleanHash, recoveredHash)

	artifact.MigratedRoot = offlineUpgradeMigratedDir
	artifact.UpgradeHash = offlineUpgradeHashString(cleanHash)
	writeOfflineUpgradeArtifact(t, root, artifact)

	txRoot := filepath.Join(root, "post-upgrade-tx")
	copyOfflineUpgradeDatabase(t, cleanRoot, txRoot)
	requireV69OfflineAuthzExecRefused(t, txRoot, artifact.Retained)
}

func applyV69OfflineUpgradeClean(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV69OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, testApp),
		"v6.9 did not reopen the v6.8 module version map")

	finalizeV69OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV69OfflineMigrated(t, reopened, artifact)
	hash := committedOfflineUpgradeHash(t, reopened)
	closeOfflineUpgradeApp(t, reopened)
	requireV69OfflineTreeDeleted(t, root)
	return hash
}

func applyV69OfflineUpgradeCrashReplay(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV69OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.SourceHeight, testApp.LastBlockHeight())
	finalizeV69OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	closeOfflineUpgradeApp(t, testApp)

	interrupted := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.SourceHeight, interrupted.LastBlockHeight(),
		"closing without commit left the crash-replay database above the pre-upgrade height")
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, interrupted),
		"closing without commit mutated the pre-upgrade version map")
	finalizeV69OfflineUpgrade(t, interrupted, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, interrupted)
	closeOfflineUpgradeApp(t, interrupted)

	reopened := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV69OfflineMigrated(t, reopened, artifact)
	hash := committedOfflineUpgradeHash(t, reopened)
	closeOfflineUpgradeApp(t, reopened)
	requireV69OfflineTreeDeleted(t, root)
	return hash
}

// requireV69OfflineMigrated checks a reopened migrated database: the upgrade is
// recorded, the authz version-map entry and store are gone, and nothing else in
// the retained stores changed.
func requireV69OfflineMigrated(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	requireV69OfflineAppliedName(t, testApp, artifact)
	requireV69OfflineVersionMap(t, testApp, artifact.ModuleVersions)
	requireV69OfflineStoreDeleted(t, testApp)
	requireOfflineUpgradeRetainedStoresExcept(t, testApp, artifact.Stores, v69OfflineTouchedKey)
}

// v69OfflineTouchedKey reports the retained-store keys the v6.9 upgrade block is
// specified to change: the plan, done and version-map entries of the upgrade
// store, the bank entries block rewards move every block, and the block time gov
// records every block for proposal deadlines.
func v69OfflineTouchedKey(storeName string, key []byte) bool {
	switch storeName {
	case upgradetypes.StoreKey:
		return len(key) > 0 && (key[0] == upgradetypes.PlanByte || key[0] == upgradetypes.DoneByte || key[0] == upgradetypes.VersionMapByte)
	case banktypes.StoreKey:
		return v69OfflineBlockRewardKey(key)
	case govtypes.StoreKey:
		return bytes.Equal(key, govtypes.DeadlineBoundaryBlockTimeKey)
	}
	return false
}

// v69OfflineBlockRewardKey reports bank keys that minting and distribution
// rewrite on every block: the usei supply and the balances of the module
// accounts rewards flow through.
func v69OfflineBlockRewardKey(key []byte) bool {
	if bytes.HasPrefix(key, banktypes.SupplyKey) {
		return string(key[len(banktypes.SupplyKey):]) == sdk.DefaultBondDenom
	}
	for _, prefix := range [][]byte{banktypes.BalancesPrefix, banktypes.WeiBalancesPrefix} {
		if !bytes.HasPrefix(key, prefix) {
			continue
		}
		for _, name := range []string{authtypes.FeeCollectorName, distrtypes.ModuleName, minttypes.ModuleName} {
			if bytes.HasPrefix(key[len(prefix):], address.MustLengthPrefix(authtypes.NewModuleAddress(name))) {
				return true
			}
		}
	}
	return false
}

func requireV69OfflinePersistedPlanHasHandler(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found, "committed upgrade plan did not survive the process boundary")
	require.Equal(t, artifact.Upgrade, plan.Name)
	require.Equal(t, artifact.UpgradeHeight, plan.Height)
	require.Equal(t, LatestUpgrade, plan.Name)
	require.True(t, testApp.UpgradeKeeper.HasHandler(plan.Name),
		"target binary has no handler for persisted plan name %q", plan.Name)
}

func requireV69OfflineAppliedName(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(
		offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()))
	require.Equal(t, artifact.Upgrade, lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.True(t, testApp.UpgradeKeeper.HasHandler(lastName),
		"target binary has no handler for applied plan name %q", lastName)
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

// requireV69OfflineRefusesRetainedStore requires the v6.9 handler to halt a
// node that reaches the upgrade height without upgrade-info.json, which leaves
// the authz store undeleted.
func requireV69OfflineRefusesRetainedStore(t *testing.T, root string, artifact offlineUpgradeArtifact) {
	t.Helper()
	require.NoError(t, os.Remove(filepath.Join(root, "home", "data", "upgrade-info.json")))
	testApp := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, testApp)
	require.True(t, testApp.rootStore.MayHoldStore(retiredauthz.ModuleName),
		"%s was deleted without upgrade-info.json", retiredauthz.ModuleName)

	var refusal string
	func() {
		defer func() {
			if r := recover(); r != nil {
				refusal = fmt.Sprint(r)
			}
		}()
		_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
			Hash: []byte("offline-upgrade"),
			Header: &tmproto.Header{
				ChainID: offlineUpgradeChainID, Height: artifact.UpgradeHeight, Time: v69OfflineUpgradeBlockTime,
			},
		})
		if err != nil {
			refusal = err.Error()
		}
	}()
	require.Contains(t, refusal, fmt.Sprintf("store %q was not deleted before the v6.9 upgrade handler ran", retiredauthz.ModuleName))
	require.Contains(t, refusal, v69OfflineUpgradeInfo(artifact.UpgradeHeight))
}

// applyV69OfflineUpgradeAfterRefusal writes upgrade-info.json as the refusal
// tells the operator to, then applies v6.9 and returns the application hash.
func applyV69OfflineUpgradeAfterRefusal(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	target := filepath.Join(root, "home", "data", "upgrade-info.json")
	require.NoError(t, os.WriteFile(target, []byte(v69OfflineUpgradeInfo(artifact.UpgradeHeight)), 0o600))
	return applyV69OfflineUpgradeClean(t, root, artifact)
}

// v69OfflineUpgradeInfo is the upgrade-info.json content naming v6.9 at height.
func v69OfflineUpgradeInfo(height int64) string {
	return fmt.Sprintf(`{"name":%q,"height":%d}`, v69OfflineUpgradeName, height)
}

// requireV69OfflineVersionMap requires authz to be the only module version the
// upgrade removed, with none added.
func requireV69OfflineVersionMap(t *testing.T, testApp *App, before []string) {
	t.Helper()
	after := offlineUpgradeModuleVersions(t, testApp)
	require.Equal(t, []string{retiredauthz.ModuleName}, offlineUpgradeDifference(before, after),
		"v6.9 removed an unexpected set of module versions")
	require.Empty(t, offlineUpgradeDifference(after, before), "v6.9 added a module version")
	require.False(t, offlineUpgradeHasModuleVersion(t, testApp, retiredauthz.ModuleName),
		"upgrade store still has a version-map entry for authz")
}

func requireV69OfflineStoreDeleted(t *testing.T, testApp *App) {
	t.Helper()
	require.Nil(t, testApp.GetKey(retiredauthz.ModuleName))
	for _, key := range testApp.CommitMultiStore().StoreKeys() {
		require.NotEqual(t, retiredauthz.ModuleName, key.Name())
	}
}

func requireV69OfflineTreeDeleted(t *testing.T, root string) {
	t.Helper()
	store := memiavl.NewCommitStore(filepath.Join(root, "home"), memiavl.DefaultConfig())
	defer func() {
		require.NoError(t, store.Close())
	}()
	latestVersion, err := store.GetLatestVersion()
	require.NoError(t, err)
	require.NotZero(t, latestVersion)
	_, err = store.LoadVersion(0, false)
	require.NoError(t, err)
	require.Equal(t, latestVersion, store.Version())
	require.NotNil(t, store.GetDB().TreeByName("bank"))
	require.Nil(t, store.GetDB().TreeByName(retiredauthz.ModuleName))
}

// requireV69OfflineAuthzExecRefused delivers the recorded grantee's MsgExec of a
// send from the recorded granter, which v6.8 would have executed under the
// grant, and requires it to be refused as deprecated without moving any balance
// or the grantee's sequence.
func requireV69OfflineAuthzExecRefused(t *testing.T, root string, retained offlineUpgradeRetainedState) {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	upgradeHeight := testApp.LastBlockHeight()

	privBytes, err := hex.DecodeString(retained.TxSenderKey)
	require.NoError(t, err)
	priv := &secp256k1.PrivKey{Key: privBytes}
	grantee := sdk.MustAccAddressFromBech32(retained.TxSender)
	require.Equal(t, grantee, sdk.AccAddress(priv.PubKey().Address()),
		"recorded grantee key does not match recorded grantee address")
	granter := sdk.MustAccAddressFromBech32(retained.AuthzGranter)
	recipient := sdk.MustAccAddressFromBech32(retained.TxRecipient)
	accounts := []sdk.AccAddress{granter, grantee, recipient}

	beforeCtx := offlineUpgradeReadContext(testApp, upgradeHeight)
	balances := make(map[string]sdk.Coins, len(accounts))
	for _, account := range accounts {
		balances[account.String()] = testApp.BankKeeper.GetAllBalances(beforeCtx, account)
	}
	sequence := testApp.AccountKeeper.GetAccount(beforeCtx, grantee).GetSequence()

	send, err := codectypes.NewAnyWithValue(banktypes.NewMsgSend(granter, recipient, sdk.NewCoins(sdk.NewInt64Coin("usei", 1))))
	require.NoError(t, err)
	txBz := signV69OfflineTx(t, testApp, priv, &retiredauthz.MsgExec{Grantee: grantee.String(), Msgs: []*codectypes.Any{send}})
	res, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade-tx"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID,
			Height:  upgradeHeight + 1,
			Time:    v69OfflineUpgradeBlockTime.Add(time.Second),
		},
		Txs: [][]byte{txBz},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 1)
	require.Equal(t, retiredauthz.ErrDeprecated.ABCICode(), res.TxResults[0].Code, res.TxResults[0].Log)
	require.Equal(t, retiredauthz.ErrDeprecated.Codespace(), res.TxResults[0].Codespace)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, upgradeHeight+1, reopened.LastBlockHeight())
	afterCtx := offlineUpgradeReadContext(reopened, reopened.LastBlockHeight())
	for _, account := range accounts {
		require.Equal(t, balances[account.String()], reopened.BankKeeper.GetAllBalances(afterCtx, account),
			"the refused MsgExec moved the balance of %s", account)
	}
	require.Equal(t, sequence, reopened.AccountKeeper.GetAccount(afterCtx, grantee).GetSequence(),
		"the refused MsgExec advanced the grantee's sequence")
}

func signV69OfflineTx(t *testing.T, testApp *App, priv *secp256k1.PrivKey, msg sdk.Msg) []byte {
	t.Helper()
	from := sdk.AccAddress(priv.PubKey().Address())
	txConfig := testApp.GetTxConfig()
	txBuilder := txConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msg))
	txBuilder.SetGasLimit(1_000_000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", v69OfflinePostUpgradeFee)))

	acc := testApp.AccountKeeper.GetAccount(offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()), from)
	require.NotNil(t, acc, "signer account is missing from the migrated database")
	signerData := xauthsigning.SignerData{
		ChainID:       offlineUpgradeChainID,
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}
	signMode := txConfig.SignModeHandler().DefaultMode()
	require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: signMode},
		Sequence: acc.GetSequence(),
	}))
	bytesToSign, err := txConfig.SignModeHandler().GetSignBytes(signMode, signerData, txBuilder.GetTx())
	require.NoError(t, err)
	signature, err := priv.Sign(bytesToSign)
	require.NoError(t, err)
	require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: signMode, Signature: signature},
		Sequence: acc.GetSequence(),
	}))
	bz, err := txConfig.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	return bz
}

// testV69OfflineUpgradeTargetSnapshot applies v6.9 to a real pre-v6.9 node home
// and requires the authz version-map entry, and only that entry, to be removed.
func testV69OfflineUpgradeTargetSnapshot(t *testing.T) {
	home := requireOfflineUpgradeSnapshotHome(t)
	t.Setenv("UPGRADE_VERSION_LIST", v69OfflineUpgradeName)
	chainID := readOfflineUpgradeGenesisChainID(t, home)

	// The store loader deletes the authz store only when upgrade-info.json
	// names v6.9, as it does after the previous binary halts at the plan.
	halted := openOfflineUpgradeSnapshotApp(t, home, chainID)
	sourceHeight := halted.LastBlockHeight()
	require.NoError(t, halted.UpgradeKeeper.DumpUpgradeInfoToDisk(sourceHeight+1, v69OfflineUpgradeName))
	closeOfflineUpgradeApp(t, halted)

	testApp := openOfflineUpgradeSnapshotApp(t, home, chainID)
	require.Equal(t, sourceHeight, testApp.LastBlockHeight())
	beforeVersions := offlineUpgradeModuleVersions(t, testApp)
	require.Contains(t, beforeVersions, retiredauthz.ModuleName,
		"%s is not a pre-v6.9 snapshot: module version map is missing authz", home)

	require.True(t, testApp.UpgradeKeeper.HasHandler(v69OfflineUpgradeName),
		"v6.9 upgrade handler is not registered; set UPGRADE_VERSION_LIST=v6.9")
	upgradeHeight := sourceHeight + 1
	testApp.UpgradeKeeper.ApplyUpgrade(offlineUpgradeContext(testApp, upgradeHeight, chainID), upgradetypes.Plan{
		Name:   v69OfflineUpgradeName,
		Height: upgradeHeight,
	})
	testApp.CommitMultiStore().Commit(true)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeSnapshotApp(t, home, chainID)
	defer closeOfflineUpgradeApp(t, reopened)
	requireV69OfflineVersionMap(t, reopened, beforeVersions)
}
