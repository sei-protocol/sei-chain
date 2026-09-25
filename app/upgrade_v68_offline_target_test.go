//go:build upgrade_v68 && offline_upgrade && upgrade_target

package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	xauthsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

const v68OfflineUpgradeName = "v6.8"

const v68OfflineUpgradeBlockTimeUnix = 1_700_000_000

const (
	v68OfflineAuthVersionAfter   uint64 = 4
	v68OfflinePostUpgradeFee     int64  = 200000
	v68OfflineLegacyVestingTypes        = "/cosmos.vesting.v1beta1."
)

var v68OfflineUpgradeBlockTime = time.Unix(v68OfflineUpgradeBlockTimeUnix, 0).UTC()

func v68OfflineStoreNames(testApp *App) []string {
	keys := testApp.CommitMultiStore().StoreKeys()
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		if testApp.GetKey(key.Name()) == nil {
			continue
		}
		names = append(names, key.Name())
	}
	sort.Strings(names)
	return names
}

func TestV68OfflineUpgradeTarget(t *testing.T) {
	t.Run("fixture", testV68OfflineUpgradeTargetFixture)
	t.Run("snapshot", testV68OfflineUpgradeTargetSnapshot)
}

func testV68OfflineUpgradeTargetFixture(t *testing.T) {
	root := requireOfflineUpgradePhase(t, "target")
	artifact := readOfflineUpgradeArtifact(t, root)
	require.Equal(t, v68OfflineUpgradeName, artifact.Upgrade)
	require.NotEmpty(t, artifact.VestingAccounts, "the source phase recorded no vesting accounts")
	require.NotEmpty(t, artifact.Retained.TxSender)
	require.NotEmpty(t, artifact.Retained.TxSenderKey)
	require.NotEmpty(t, artifact.Retained.TxRecipient)
	require.Equal(t, artifact.SourceHeight+1, artifact.UpgradeHeight)

	t.Setenv("UPGRADE_VERSION_LIST", LatestUpgrade)

	cleanRoot := filepath.Join(root, offlineUpgradeMigratedDir)
	crashRoot := filepath.Join(root, "crash")
	copyOfflineUpgradeDatabase(t, root, cleanRoot)
	copyOfflineUpgradeDatabase(t, root, crashRoot)

	cleanHash := applyV68OfflineUpgradeClean(t, cleanRoot, artifact)
	crashHash := applyV68OfflineUpgradeCrashReplay(t, crashRoot, artifact)
	require.Equalf(t, cleanHash, crashHash,
		"crash-replay application hash diverged from the clean single-pass hash: clean=%x crash-replay=%x",
		cleanHash, crashHash)

	artifact.MigratedRoot = offlineUpgradeMigratedDir
	artifact.UpgradeHash = offlineUpgradeHashString(cleanHash)
	writeOfflineUpgradeArtifact(t, root, artifact)

	txRoot := filepath.Join(root, "post-upgrade-tx")
	copyOfflineUpgradeDatabase(t, cleanRoot, txRoot)
	requireV68OfflineLockedBalanceSpend(t, txRoot, artifact.Retained)
}

func applyV68OfflineUpgradeClean(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV68OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, testApp),
		"v6.8 did not reopen the v6.7 module version map")
	require.Equal(t, sortedOfflineStoreNames(artifact.Stores), v68OfflineStoreNames(testApp))
	requireV68OfflineLegacyAccountsStored(t, testApp, artifact)

	finalizeV68OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV68OfflineAppliedName(t, reopened, artifact)
	requireV68OfflineVersionMap(t, reopened, artifact.ModuleVersions)
	require.Equal(t, sortedOfflineStoreNames(artifact.Stores), v68OfflineStoreNames(reopened))
	requireV68OfflineAccountsRewritten(t, reopened, artifact)
	return committedOfflineUpgradeHash(t, reopened)
}

func applyV68OfflineUpgradeCrashReplay(t *testing.T, root string, artifact offlineUpgradeArtifact) []byte {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	requireV68OfflinePersistedPlanHasHandler(t, testApp, artifact)
	require.Equal(t, artifact.SourceHeight, testApp.LastBlockHeight())
	finalizeV68OfflineUpgrade(t, testApp, artifact.UpgradeHeight)
	closeOfflineUpgradeApp(t, testApp)

	interrupted := openOfflineUpgradeApp(t, root, false)
	require.Equal(t, artifact.SourceHeight, interrupted.LastBlockHeight(),
		"closing without commit left the crash-replay database above the pre-upgrade height")
	require.Equal(t, artifact.ModuleVersions, offlineUpgradeModuleVersions(t, interrupted),
		"closing without commit mutated the pre-upgrade version map")
	requireV68OfflineLegacyAccountsStored(t, interrupted, artifact)
	finalizeV68OfflineUpgrade(t, interrupted, artifact.UpgradeHeight)
	commitOfflineUpgradeApp(t, interrupted)
	closeOfflineUpgradeApp(t, interrupted)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, artifact.UpgradeHeight, reopened.LastBlockHeight())
	requireV68OfflineAppliedName(t, reopened, artifact)
	requireV68OfflineVersionMap(t, reopened, artifact.ModuleVersions)
	requireV68OfflineAccountsRewritten(t, reopened, artifact)
	return committedOfflineUpgradeHash(t, reopened)
}

func requireV68OfflinePersistedPlanHasHandler(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	plan, found := committedOfflineUpgradePlan(t, testApp)
	require.True(t, found, "committed upgrade plan did not survive the process boundary")
	require.Equal(t, artifact.Upgrade, plan.Name)
	require.Equal(t, artifact.UpgradeHeight, plan.Height)
	require.Equal(t, LatestUpgrade, plan.Name)
	require.True(t, testApp.UpgradeKeeper.HasHandler(plan.Name),
		"target binary has no handler for persisted plan name %q", plan.Name)
}

func requireV68OfflineAppliedName(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	lastName, lastHeight := testApp.UpgradeKeeper.GetLastCompletedUpgrade(
		offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()))
	require.Equal(t, artifact.Upgrade, lastName)
	require.Equal(t, artifact.UpgradeHeight, lastHeight)
	require.True(t, testApp.UpgradeKeeper.HasHandler(lastName),
		"target binary has no handler for applied plan name %q", lastName)
}

func finalizeV68OfflineUpgrade(t *testing.T, testApp *App, height int64) {
	t.Helper()
	_, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID,
			Height:  height,
			Time:    v68OfflineUpgradeBlockTime,
		},
	})
	require.NoError(t, err)
}

// requireV68OfflineLegacyAccountsStored requires every recorded vesting account
// to be committed under its vesting type, which the v6.8 binary reads only as
// bytes.
func requireV68OfflineLegacyAccountsStored(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	store := testApp.CommitMultiStore().GetCommitKVStore(testApp.GetKey(authtypes.StoreKey))
	for _, recorded := range artifact.VestingAccounts {
		encoded := store.Get(authtypes.AddressStoreKey(sdk.MustAccAddressFromBech32(recorded.Address)))
		require.Equal(t, recorded.TypeURL, string(v68OfflineTypeURL(encoded)),
			"the source phase did not commit %s under its vesting type", recorded.Address)
	}
}

func requireV68OfflineVersionMap(t *testing.T, testApp *App, before []string) {
	t.Helper()
	after := offlineUpgradeModuleVersions(t, testApp)
	require.Equal(t, []string{"vesting"}, offlineUpgradeDifference(before, after),
		"v6.8 removed an unexpected set of module versions")
	require.Empty(t, offlineUpgradeDifference(after, before), "v6.8 added a module version")
	require.False(t, offlineUpgradeHasModuleVersion(t, testApp, "vesting"),
		"upgrade store still has a version-map entry for vesting")
	versions := testApp.UpgradeKeeper.GetModuleVersionMap(offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()))
	require.Equal(t, v68OfflineAuthVersionAfter, versions[authtypes.ModuleName])
}

// requireV68OfflineAccountsRewritten requires every recorded vesting account to
// be stored as the base account it embedded, holding the same balance, all of
// it spendable; every other account the source phase stored to be unchanged;
// and no account to be left under a vesting type.
func requireV68OfflineAccountsRewritten(t *testing.T, testApp *App, artifact offlineUpgradeArtifact) {
	t.Helper()
	ctx := offlineUpgradeReadContext(testApp, testApp.LastBlockHeight())
	after := snapshotCommittedOfflineUpgradeStore(t, testApp, authtypes.StoreKey)

	rewritten := make(map[string]struct{}, len(artifact.VestingAccounts))
	for _, recorded := range artifact.VestingAccounts {
		address := sdk.MustAccAddressFromBech32(recorded.Address)
		want, err := testApp.AccountKeeper.MarshalAccount(v68OfflineRecordedBaseAccount(t, recorded))
		require.NoError(t, err)
		key := encodeOfflineUpgradeKey(authtypes.AddressStoreKey(address))
		require.Equal(t, base64.StdEncoding.EncodeToString(want), after[key],
			"the %s was not rewritten as the base account it embeds", recorded.TypeURL)
		rewritten[key] = struct{}{}

		balance, err := sdk.ParseCoinsNormalized(recorded.Balance)
		require.NoError(t, err)
		require.Equal(t, balance, testApp.BankKeeper.GetAllBalances(ctx, address),
			"v6.8 changed the balance of the %s", recorded.TypeURL)
		require.Equal(t, balance, testApp.BankKeeper.SpendableCoins(ctx, address),
			"the balance the %s locked is not spendable after v6.8", recorded.TypeURL)
	}

	for key, value := range artifact.Stores[authtypes.StoreKey] {
		if _, ok := rewritten[key]; ok || !v68OfflineIsAccountKey(t, key) {
			continue
		}
		require.Equal(t, value, after[key], "v6.8 changed an account that was not a vesting account")
	}
	for key, value := range after {
		if !v68OfflineIsAccountKey(t, key) {
			continue
		}
		encoded, err := base64.StdEncoding.DecodeString(value)
		require.NoError(t, err)
		require.False(t, bytes.HasPrefix(v68OfflineTypeURL(encoded), []byte(v68OfflineLegacyVestingTypes)),
			"an account is still stored under %s after v6.8", v68OfflineTypeURL(encoded))
	}
}

// v68OfflineRecordedBaseAccount returns the base account a recorded vesting
// account embedded.
func v68OfflineRecordedBaseAccount(t *testing.T, recorded offlineUpgradeVestingAccount) *authtypes.BaseAccount {
	t.Helper()
	pubKey, err := hex.DecodeString(recorded.PubKey)
	require.NoError(t, err)
	return authtypes.NewBaseAccount(sdk.MustAccAddressFromBech32(recorded.Address),
		&secp256k1.PubKey{Key: pubKey}, recorded.AccountNumber, recorded.Sequence)
}

// v68OfflineIsAccountKey reports whether an encoded account store key holds an
// account rather than the global account number.
func v68OfflineIsAccountKey(t *testing.T, encodedKey string) bool {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	require.NoError(t, err)
	return bytes.HasPrefix(key, authtypes.AddressStoreKeyPrefix)
}

// v68OfflineTypeURL returns the type URL an encoded google.protobuf.Any leads
// with, or nil when it does not lead with one.
func v68OfflineTypeURL(encoded []byte) []byte {
	num, wireType, n := protowire.ConsumeTag(encoded)
	if n < 0 || num != 1 || wireType != protowire.BytesType {
		return nil
	}
	typeURL, m := protowire.ConsumeBytes(encoded[n:])
	if m < 0 {
		return nil
	}
	return typeURL
}

// requireV68OfflineLockedBalanceSpend delivers a signed bank send of the whole
// balance the recorded sender's schedule locked before v6.8, and requires the
// committed balances and sequence to move.
func requireV68OfflineLockedBalanceSpend(t *testing.T, root string, retained offlineUpgradeRetainedState) {
	t.Helper()
	testApp := openOfflineUpgradeApp(t, root, false)
	upgradeHeight := testApp.LastBlockHeight()

	privBytes, err := hex.DecodeString(retained.TxSenderKey)
	require.NoError(t, err)
	priv := &secp256k1.PrivKey{Key: privBytes}
	sender := sdk.MustAccAddressFromBech32(retained.TxSender)
	require.Equal(t, sender, sdk.AccAddress(priv.PubKey().Address()),
		"recorded sender key does not match recorded sender address")
	recipient := sdk.MustAccAddressFromBech32(retained.TxRecipient)

	beforeCtx := offlineUpgradeReadContext(testApp, upgradeHeight)
	balance := testApp.BankKeeper.GetBalance(beforeCtx, sender, "usei")
	recipientBefore := testApp.BankKeeper.GetBalance(beforeCtx, recipient, "usei")
	sequence := testApp.AccountKeeper.GetAccount(beforeCtx, sender).GetSequence()
	amount := balance.Amount.Int64() - v68OfflinePostUpgradeFee

	txBz := signV68OfflineBankSend(t, testApp, priv, recipient, amount, v68OfflinePostUpgradeFee)
	res, err := testApp.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Hash: []byte("offline-upgrade-tx"),
		Header: &tmproto.Header{
			ChainID: offlineUpgradeChainID,
			Height:  upgradeHeight + 1,
			Time:    v68OfflineUpgradeBlockTime.Add(time.Second),
		},
		Txs: [][]byte{txBz},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 1)
	require.Equal(t, uint32(abci.CodeTypeOK), res.TxResults[0].Code, res.TxResults[0].Log)
	commitOfflineUpgradeApp(t, testApp)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeApp(t, root, false)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, upgradeHeight+1, reopened.LastBlockHeight())
	afterCtx := offlineUpgradeReadContext(reopened, reopened.LastBlockHeight())
	require.True(t, reopened.BankKeeper.GetBalance(afterCtx, sender, "usei").IsZero(),
		"the sender kept part of the balance its schedule had locked")
	require.Equal(t, recipientBefore.Add(sdk.NewInt64Coin("usei", amount)),
		reopened.BankKeeper.GetBalance(afterCtx, recipient, "usei"),
		"the bank send of the formerly locked balance did not credit the recipient")
	require.Equal(t, sequence+1, reopened.AccountKeeper.GetAccount(afterCtx, sender).GetSequence(),
		"the bank send did not advance the sender sequence")
}

func signV68OfflineBankSend(t *testing.T, testApp *App, priv *secp256k1.PrivKey, to sdk.AccAddress, amount, fee int64) []byte {
	t.Helper()
	from := sdk.AccAddress(priv.PubKey().Address())
	txConfig := testApp.GetTxConfig()
	txBuilder := txConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(banktypes.NewMsgSend(from, to, sdk.NewCoins(sdk.NewInt64Coin("usei", amount)))))
	txBuilder.SetGasLimit(1_000_000)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", fee)))

	acc := testApp.AccountKeeper.GetAccount(offlineUpgradeReadContext(testApp, testApp.LastBlockHeight()), from)
	require.NotNil(t, acc, "sender account is missing from the migrated database")
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

// testV68OfflineUpgradeTargetSnapshot applies v6.8 to a real pre-v6.8 node
// home and logs how long the account rewrite took against it.
func testV68OfflineUpgradeTargetSnapshot(t *testing.T) {
	home := requireOfflineUpgradeSnapshotHome(t)
	t.Setenv("UPGRADE_VERSION_LIST", v68OfflineUpgradeName)
	chainID := readOfflineUpgradeGenesisChainID(t, home)

	testApp := openOfflineUpgradeSnapshotApp(t, home, chainID)
	sourceHeight := testApp.LastBlockHeight()
	beforeVersions := offlineUpgradeModuleVersions(t, testApp)
	require.Contains(t, beforeVersions, "vesting",
		"%s is not a pre-v6.8 snapshot: module version map is missing vesting", home)
	legacy := v68OfflineLegacyVestingAccounts(t, testApp, offlineUpgradeContext(testApp, sourceHeight, chainID))
	balances := make(map[string]sdk.Coins, len(legacy))
	for _, address := range legacy {
		balances[address.String()] = testApp.BankKeeper.GetAllBalances(
			offlineUpgradeContext(testApp, sourceHeight, chainID), address)
	}

	require.True(t, testApp.UpgradeKeeper.HasHandler(v68OfflineUpgradeName),
		"v6.8 upgrade handler is not registered; set UPGRADE_VERSION_LIST=v6.8")
	upgradeHeight := sourceHeight + 1
	started := time.Now()
	testApp.UpgradeKeeper.ApplyUpgrade(offlineUpgradeContext(testApp, upgradeHeight, chainID), upgradetypes.Plan{
		Name:   v68OfflineUpgradeName,
		Height: upgradeHeight,
	})
	t.Logf("v6.8 rewrote %d vesting accounts of %s in %s", len(legacy), home, time.Since(started))
	testApp.CommitMultiStore().Commit(true)
	closeOfflineUpgradeApp(t, testApp)

	reopened := openOfflineUpgradeSnapshotApp(t, home, chainID)
	defer closeOfflineUpgradeApp(t, reopened)
	require.Equal(t, []string{"vesting"}, offlineUpgradeDifference(beforeVersions, offlineUpgradeModuleVersions(t, reopened)),
		"v6.8 removed an unexpected set of module versions")
	ctx := offlineUpgradeContext(reopened, reopened.LastBlockHeight(), chainID)
	require.Empty(t, v68OfflineLegacyVestingAccounts(t, reopened, ctx), "accounts are still stored under a vesting type")
	for _, address := range legacy {
		_, ok := reopened.AccountKeeper.GetAccount(ctx, address).(*authtypes.BaseAccount)
		require.True(t, ok, "%s was not rewritten as a base account", address)
		require.Equal(t, balances[address.String()], reopened.BankKeeper.GetAllBalances(ctx, address),
			"v6.8 changed the balance of %s", address)
	}
}

// v68OfflineLegacyVestingAccounts returns the address of every account stored
// under a vesting type.
func v68OfflineLegacyVestingAccounts(t *testing.T, testApp *App, ctx sdk.Context) []sdk.AccAddress {
	t.Helper()
	store := prefix.NewStore(ctx.KVStore(testApp.GetKey(authtypes.StoreKey)), authtypes.AddressStoreKeyPrefix)
	iterator := store.Iterator(nil, nil)
	defer func() {
		require.NoError(t, iterator.Close())
	}()
	var addresses []sdk.AccAddress
	for ; iterator.Valid(); iterator.Next() {
		if bytes.HasPrefix(v68OfflineTypeURL(iterator.Value()), []byte(v68OfflineLegacyVestingTypes)) {
			addresses = append(addresses, append(sdk.AccAddress(nil), iterator.Key()...))
		}
	}
	return addresses
}

func sortedOfflineStoreNames(stores map[string]map[string]string) []string {
	names := make([]string, 0, len(stores))
	for name := range stores {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
