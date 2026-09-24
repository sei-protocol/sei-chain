//go:build upgrade_v68

package app_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtestutil "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/testutil"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/msgs"
	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

// v6.8 removes the vesting module. The module owned no store: its state is the
// accounts it wrote to the auth account store, which the auth 3 to 4 migration
// rewrites as the base accounts they embed, and its version-map entry, which
// the handler deletes. These tests cover the rewrite of every vesting account
// type, spending a balance a schedule had locked, the message the module
// served, genesis export over the migrated store, and the handler itself.

const (
	v68UpgradeName = "v6.8"
	v68TxFee       = 200000
	// v68VestingEndTime is 3000-01-01, the end time of every vesting account on
	// pacific-1, so each fixture's schedule locks its whole balance.
	v68VestingEndTime int64 = 32_503_680_000
	v68VestingBalance int64 = 1_000_000_000
	v68VestingModule        = "vesting"
	// v68AuthVersionBefore and v68AuthVersion are the auth consensus versions
	// on either side of the upgrade.
	v68AuthVersionBefore uint64 = 3
	v68AuthVersion       uint64 = 4

	v68MsgCreateVestingAccountTypeURL = "/cosmos.vesting.v1beta1.MsgCreateVestingAccount"
	v68BaseAccountTypeURL             = "/cosmos.auth.v1beta1.BaseAccount"
	v68RunningSeid                    = "/root/go/bin/seid"
	v68KeyringPassword                = "12345678\n"
	v68PostUpgradeSendAmount          = "6868usei"
)

var v68PostUpgradeBankReceiver = sdk.AccAddress{
	0x68, 0x68, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x68,
}

func newV68Chain(t *testing.T) *processblock.App {
	t.Helper()
	t.Setenv("UPGRADE_VERSION_LIST", v68UpgradeName)
	app := processblock.NewTestApp(t)
	processblock.CommonPreset(app)
	app.RegisterUpgradeHandlers()
	return app
}

func applyV68(t *testing.T, app *processblock.App) {
	t.Helper()
	app.UpgradeKeeper.ApplyUpgrade(app.Ctx(), upgradetypes.Plan{
		Name:   v68UpgradeName,
		Height: app.Ctx().BlockHeight(),
	})
}

// applyV68ToCommitStore applies the v6.8 handler against the committed multistore.
func applyV68ToCommitStore(t *testing.T, a *processblock.App) {
	t.Helper()
	// Writes into the deliver context the harness hands back are dropped rather
	// than committed, so the upgrade has to reach the commit multistore directly.
	ctx, write := a.NewUncachedContext(false, a.Ctx().BlockHeader()).CacheContext()
	a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{
		Name:   v68UpgradeName,
		Height: ctx.BlockHeight(),
	})
	write()
}

// v68VestingFixture is an account stored under a vesting module type, as v6.7
// left it, with the balance its schedule locks.
type v68VestingFixture struct {
	account authtestutil.LegacyVestingAccount
	balance sdk.Coins
}

// seedV67VersionMap records auth at its v6.7 consensus version and vesting in
// the version map, as every chain upgrading from v6.7 carries them.
func seedV67VersionMap(t *testing.T, a *processblock.App) {
	t.Helper()
	versionMap := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	versionMap[authtypes.ModuleName] = v68AuthVersionBefore
	versionMap[v68VestingModule] = 1
	a.UpgradeKeeper.SetModuleVersionMap(a.Ctx(), versionMap)
}

// seedV67VestingAccount funds base and stores it as an account of typeURL
// whose schedule locks that whole balance. Like every write through the
// harness context, it must happen before the chain's first block.
func seedV67VestingAccount(t *testing.T, a *processblock.App, typeURL string, base *authtypes.BaseAccount) v68VestingFixture {
	t.Helper()
	ctx := a.Ctx()
	balance := sdk.NewCoins(sdk.NewInt64Coin("usei", v68VestingBalance))
	account := authtestutil.LegacyVestingAccount{
		TypeURL:         typeURL,
		Base:            base,
		OriginalVesting: balance,
		EndTime:         v68VestingEndTime,
		StartTime:       ctx.BlockTime().Unix(),
	}
	if typeURL == authtestutil.LegacyPeriodicVestingAccountTypeURL {
		account.Periods = []authtestutil.LegacyVestingPeriod{
			{Length: v68VestingEndTime - account.StartTime, Amount: balance},
		}
	}
	encoded, err := account.Encode()
	require.NoError(t, err)
	ctx.KVStore(a.GetKey(authtypes.StoreKey)).Set(authtypes.AddressStoreKey(base.GetAddress()), encoded)
	a.FundAccount(base.GetAddress(), v68VestingBalance)
	return v68VestingFixture{account: account, balance: balance}
}

// seedV67VestingState writes the version map of a v6.7 chain and one account
// of every vesting type, each with a public key, account number and sequence.
func seedV67VestingState(t *testing.T, a *processblock.App) []v68VestingFixture {
	t.Helper()
	seedV67VersionMap(t, a)
	fixtures := make([]v68VestingFixture, 0, len(authtestutil.LegacyVestingAccountTypeURLs))
	for i, typeURL := range authtestutil.LegacyVestingAccountTypeURLs {
		pub := secp256k1.GenPrivKey().PubKey()
		accountNumber := a.AccountKeeper.GetNextAccountNumber(a.Ctx())
		base := authtypes.NewBaseAccount(sdk.AccAddress(pub.Address()), pub, accountNumber, uint64(i+1)) //nolint:gosec // small test index
		fixtures = append(fixtures, seedV67VestingAccount(t, a, typeURL, base))
	}
	return fixtures
}

// v68AccountEntries returns every entry under the account prefix of store.
func v68AccountEntries(t *testing.T, store sdk.KVStore) map[string][]byte {
	t.Helper()
	iterator := sdk.KVStorePrefixIterator(store, authtypes.AddressStoreKeyPrefix)
	defer func() {
		require.NoError(t, iterator.Close())
	}()
	entries := map[string][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		entries[string(iterator.Key())] = append([]byte(nil), iterator.Value()...)
	}
	return entries
}

func v68CommittedAccountEntries(t *testing.T, a *processblock.App) map[string][]byte {
	t.Helper()
	return v68AccountEntries(t, a.CommitMultiStore().GetCommitKVStore(a.GetKey(authtypes.StoreKey)))
}

func v68BankSupply(a *processblock.App) map[string]string {
	supplies := map[string]string{}
	a.BankKeeper.IterateTotalSupply(a.Ctx(), func(c sdk.Coin) bool {
		supplies[c.Denom] = c.Amount.String()
		return false
	})
	return supplies
}

func TestV68Upgrade(t *testing.T) {
	app := newV68Chain(t)
	beforeVersions := app.UpgradeKeeper.GetModuleVersionMap(app.Ctx())
	require.Contains(t, beforeVersions, "oracle")
	sender := app.NewSignableAccount("v68-sender")
	receiver := app.NewAccount()
	app.FundAccount(sender, 1_000_000)
	require.Equal(t, []uint32{0}, app.RunBlock([]signing.Tx{
		app.Sign(sender, 0, msgs.Send(sender, receiver, 1)),
	}))

	applyV68(t, app)
	require.Equal(t, beforeVersions, app.UpgradeKeeper.GetModuleVersionMap(app.Ctx()))
	require.Equal(t, []uint32{0}, app.RunBlock([]signing.Tx{
		app.Sign(sender, 0, msgs.Send(sender, receiver, 1)),
	}))
}

func TestV68UnupgradedBinaryHaltsAtPlanHeight(t *testing.T) {
	t.Setenv("UPGRADE_VERSION_LIST", "v6.7")
	app := processblock.NewTestApp(t)
	processblock.CommonPreset(app)
	app.RegisterUpgradeHandlers()
	require.False(t, app.UpgradeKeeper.HasHandler(v68UpgradeName))
	require.NoError(t, app.UpgradeKeeper.ScheduleUpgrade(app.Ctx(), upgradetypes.Plan{
		Name: v68UpgradeName, Height: 3,
	}))
	app.RunBlock(nil)
	app.RunBlock(nil)
	require.Panics(t, func() { app.RunBlock(nil) })
}

// TestV68RewritesVestingAccountsAsBaseAccounts applies v6.8 to an account of
// every vesting type and requires each to come back as the base account it
// embedded, holding the same balance, all of it now spendable, with every other
// account, the total supply, and the rest of the version map untouched.
func TestV68RewritesVestingAccountsAsBaseAccounts(t *testing.T) {
	a := newV68Chain(t)
	fixtures := seedV67VestingState(t, a)
	a.RunBlock([]signing.Tx{})
	before := v68CommittedAccountEntries(t, a)
	supplyBefore := v68BankSupply(a)
	versionsBefore := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())

	applyV68ToCommitStore(t, a)
	a.RunBlock([]signing.Tx{})

	after := v68CommittedAccountEntries(t, a)
	for _, fixture := range fixtures {
		base := fixture.account.Base
		key := string(authtypes.AddressStoreKey(base.GetAddress()))
		want, err := a.AccountKeeper.MarshalAccount(base)
		require.NoError(t, err)
		require.Equal(t, want, after[key],
			"the %s was not rewritten as the base account it embeds", fixture.account.TypeURL)
		delete(before, key)
		delete(after, key)

		got, ok := a.AccountKeeper.GetAccount(a.Ctx(), base.GetAddress()).(*authtypes.BaseAccount)
		require.True(t, ok, "the %s does not decode as a base account", fixture.account.TypeURL)
		require.Equal(t, base.GetAccountNumber(), got.GetAccountNumber())
		require.Equal(t, base.GetSequence(), got.GetSequence())
		require.True(t, base.GetPubKey().Equals(got.GetPubKey()))
		require.Equal(t, fixture.balance, a.BankKeeper.GetAllBalances(a.Ctx(), base.GetAddress()))
		require.Equal(t, fixture.balance, a.BankKeeper.SpendableCoins(a.Ctx(), base.GetAddress()),
			"the balance the %s locked is not spendable after v6.8", fixture.account.TypeURL)
	}
	require.Equal(t, before, after, "v6.8 changed an account that was not a vesting account")
	require.Equal(t, supplyBefore, v68BankSupply(a), "v6.8 moved or burned supply")

	versionsAfter := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	require.NotContains(t, versionsAfter, v68VestingModule)
	require.Equal(t, v68AuthVersion, versionsAfter[authtypes.ModuleName])
	delete(versionsBefore, v68VestingModule)
	delete(versionsBefore, authtypes.ModuleName)
	delete(versionsAfter, authtypes.ModuleName)
	require.Equal(t, versionsBefore, versionsAfter,
		"v6.8 changed a module version other than removing vesting and moving auth")
}

// TestV68ConvertedAccountSpendsTheBalanceItsScheduleLocked delivers a bank send
// of the whole balance a delayed vesting account held locked until the year
// 3000, the schedule of every pacific-1 vesting account, once v6.8 has run.
func TestV68ConvertedAccountSpendsTheBalanceItsScheduleLocked(t *testing.T) {
	a := newV68Chain(t)
	seedV67VersionMap(t, a)
	sender := a.NewSignableAccount("v68/locked-sender")
	base, ok := a.AccountKeeper.GetAccount(a.Ctx(), sender).(*authtypes.BaseAccount)
	require.True(t, ok)
	seedV67VestingAccount(t, a, authtestutil.LegacyDelayedVestingAccountTypeURL, base)
	recipient := a.NewAccount()
	a.RunBlock([]signing.Tx{})

	applyV68ToCommitStore(t, a)
	a.RunBlock([]signing.Tx{})

	amount := v68VestingBalance - v68TxFee
	results := a.RunBlockDetailed([]signing.Tx{
		a.Sign(sender, v68TxFee, msgs.Send(sender, recipient, amount)),
	})
	require.Len(t, results, 1)
	require.Equal(t, uint32(abci.CodeTypeOK), results[0].Code, results[0].Log)
	require.True(t, a.BankKeeper.GetAllBalances(a.Ctx(), sender).IsZero(),
		"the sender kept part of the balance its schedule had locked")
	require.Equal(t, sdk.NewInt64Coin("usei", amount), a.BankKeeper.GetBalance(a.Ctx(), recipient, "usei"))
	require.Equal(t, base.GetSequence()+1, a.AccountKeeper.GetAccount(a.Ctx(), sender).GetSequence())
}

// TestV68ApplyUpgradeTwice applies the v6.8 handler a second time against the
// state the first application produced.
func TestV68ApplyUpgradeTwice(t *testing.T) {
	a := newV68Chain(t)
	seedV67VestingState(t, a)
	accountStore := a.Ctx().KVStore(a.GetKey(authtypes.StoreKey))

	applyV68(t, a)
	onceAccounts := v68AccountEntries(t, accountStore)
	onceVersions := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	onceDone := a.UpgradeKeeper.GetDoneHeight(a.Ctx(), v68UpgradeName)
	onceAppVersion := a.AppVersion()
	require.Equal(t, a.Ctx().BlockHeight(), onceDone)

	require.NotPanics(t, func() { applyV68(t, a) })

	require.Equal(t, onceAccounts, v68AccountEntries(t, accountStore),
		"second ApplyUpgrade changed the account store")
	require.Equal(t, onceVersions, a.UpgradeKeeper.GetModuleVersionMap(a.Ctx()),
		"second ApplyUpgrade changed the module version map")
	require.Equal(t, onceDone, a.UpgradeKeeper.GetDoneHeight(a.Ctx(), v68UpgradeName),
		"second ApplyUpgrade changed the done height")
	require.Equal(t, onceAppVersion+1, a.AppVersion(),
		"second ApplyUpgrade is not a no-op: ApplyUpgrade increments protocol version on every call")
}

// TestV68RejectsTheVestingMessage delivers a transaction carrying
// MsgCreateVestingAccount after v6.8. The type is no longer registered, so the
// transaction does not decode: it is rejected before the ante handler and
// charges no fee, where v6.7 routed it and rejected it after taking the fee.
func TestV68RejectsTheVestingMessage(t *testing.T) {
	a := newV68Chain(t)
	signer := a.NewSignableAccount("v68/vesting-message")
	a.FundAccount(signer, v68VestingBalance)
	a.RunBlock([]signing.Tx{})
	applyV68ToCommitStore(t, a)
	a.RunBlock([]signing.Tx{})

	txBytes := v68CreateVestingAccountTx(t, signer, sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()))
	_, err := a.GetTxConfig().TxDecoder()(txBytes)
	require.ErrorContains(t, err, v68MsgCreateVestingAccountTypeURL)

	balanceBefore := a.BankKeeper.GetBalance(a.Ctx(), signer, "usei")
	res, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Txs:               [][]byte{txBytes},
		DecidedLastCommit: abci.CommitInfo{Round: 0, Votes: a.GetVotes()},
		Hash:              []byte("v68"),
		Header: &tmproto.Header{
			ChainID:         a.ChainID,
			Height:          a.Ctx().BlockHeight() + 1,
			ProposerAddress: a.GetVotes()[0].Validator.Address,
			Time:            time.Now(),
		},
	})
	require.NoError(t, err)
	require.Len(t, res.TxResults, 1)
	require.Equal(t, sdkerrors.ErrTxDecode.ABCICode(), res.TxResults[0].Code, res.TxResults[0].Log)
	require.Equal(t, sdkerrors.ErrTxDecode.Codespace(), res.TxResults[0].Codespace)
	require.Equal(t, balanceBefore,
		a.BankKeeper.GetBalance(a.GetContextForDeliverTx([]byte{}), signer, "usei"),
		"a transaction that does not decode was charged a fee")
}

// v68CreateVestingAccountTx encodes a transaction carrying the
// MsgCreateVestingAccount v6.7 clients sent, from from to to.
func v68CreateVestingAccountTx(t *testing.T, from, to sdk.AccAddress) []byte {
	t.Helper()
	amount := sdk.NewInt64Coin("usei", 1)
	coin, err := amount.Marshal()
	require.NoError(t, err)
	msg := protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), from.String())
	msg = protowire.AppendString(protowire.AppendTag(msg, 2, protowire.BytesType), to.String())
	msg = protowire.AppendBytes(protowire.AppendTag(msg, 3, protowire.BytesType), coin)
	msg = protowire.AppendVarint(protowire.AppendTag(msg, 4, protowire.VarintType), uint64(v68VestingEndTime))
	msg = protowire.AppendVarint(protowire.AppendTag(msg, 5, protowire.VarintType), 1)

	body, err := (&txtypes.TxBody{
		Messages: []*codectypes.Any{{TypeUrl: v68MsgCreateVestingAccountTypeURL, Value: msg}},
	}).Marshal()
	require.NoError(t, err)
	authInfo, err := (&txtypes.AuthInfo{
		Fee: &txtypes.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("usei", v68TxFee)), GasLimit: 1_000_000},
	}).Marshal()
	require.NoError(t, err)
	txBytes, err := (&txtypes.TxRaw{
		BodyBytes:     body,
		AuthInfoBytes: authInfo,
		Signatures:    [][]byte{make([]byte, 64)},
	}).Marshal()
	require.NoError(t, err)
	return txBytes
}

// TestV68ExportsNoVestingState exports genesis after v6.8. Export decodes every
// stored account, so it succeeds only if no account is left under a type v6.8
// no longer registers; the document has no vesting section and lists every
// converted account as a base account.
func TestV68ExportsNoVestingState(t *testing.T) {
	a := newV68Chain(t)
	fixtures := seedV67VestingState(t, a)
	a.RunBlock([]signing.Tx{})
	applyV68ToCommitStore(t, a)
	a.RunBlock([]signing.Tx{})

	exported, err := a.ExportAppStateAndValidators(false, nil)
	require.NoError(t, err)
	var genesis map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &genesis))
	require.NotContains(t, genesis, v68VestingModule)

	var auth struct {
		Accounts []struct {
			Type    string `json:"@type"`
			Address string `json:"address"`
		} `json:"accounts"`
	}
	require.NoError(t, json.Unmarshal(genesis[authtypes.ModuleName], &auth))
	exportedTypes := make(map[string]string, len(auth.Accounts))
	for _, account := range auth.Accounts {
		exportedTypes[account.Address] = account.Type
	}
	for _, fixture := range fixtures {
		require.Equal(t, v68BaseAccountTypeURL, exportedTypes[fixture.account.Base.Address],
			"the exported %s is not a base account", fixture.account.TypeURL)
	}
}

// TestV68CrossVersion runs v6.7 validators into the v6.8 upgrade and verifies
// the same chain after they restart on v6.8. The live cluster runs on a chain
// ID where v6.7 refuses to create vesting accounts, so the account rewrite is
// covered by the tests above and the offline upgrade, not here.
func TestV68CrossVersion(t *testing.T) {
	upgradetest.RunCrossVersion(t, seedV67VestingModule, verifyV68VestingRemoval)
}

// seedV67VestingModule records the v6.7 version map, which carries vesting and
// auth at consensus version 3, requires v6.7 to serve the vesting command, and
// delivers a bank send before the upgrade.
func seedV67VestingModule(t *testing.T, chain *upgradetest.CrossVersion) {
	require.Equal(t, v68UpgradeName, chain.UpgradeName(t))

	versions := v68ModuleVersions(t, chain)
	require.Equal(t, uint64(1), versions[v68VestingModule], "v6.7 module version map does not carry vesting")
	require.Equal(t, v68AuthVersionBefore, versions[authtypes.ModuleName])
	require.NotEmpty(t, chain.QueryStore(t, upgradetypes.StoreKey, v68ModuleVersionKey(v68VestingModule)),
		"v6.7 upgrade store is missing the vesting version-map entry")
	chain.Record(t, "module_versions", versions)

	vestingCommand := chain.Seid("", "tx", v68VestingModule)
	chain.WriteDiagnostic(t, "v67-tx-vesting.stdout", []byte(vestingCommand.Stdout))
	chain.WriteDiagnostic(t, "v67-tx-vesting.stderr", []byte(vestingCommand.Stderr))
	require.NoError(t, vestingCommand.Err, "v6.7 must still serve the vesting transaction command")

	chain.RequireDeliverTxSuccess(t, "v6.7 bank send", chain.Seid(v68KeyringPassword,
		"tx", "bank", "send", "admin", chain.KeyAddress(t, "sei-node-0", "node_admin"), "1usei",
		"--from", "admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	))
}

func verifyV68VestingRemoval(t *testing.T, chain *upgradetest.CrossVersion) {
	require.Equal(t, v68UpgradeName, chain.UpgradeName(t))

	appliedHeight := v68AppliedHeight(t, chain)
	require.Equal(t, chain.TargetHeight(t), appliedHeight)
	chain.Record(t, "applied_height", appliedHeight)
	// A header carries the application hash of the state its parent produced, so
	// the upgrade block's own result appears one height above the applied height.
	chain.RequireBlockAgreement(t, appliedHeight, appliedHeight+1, appliedHeight+2, appliedHeight+3)

	var before map[string]uint64
	chain.Replay(t, "module_versions", &before)
	want := make(map[string]uint64, len(before))
	for name, version := range before {
		want[name] = version
	}
	delete(want, v68VestingModule)
	want[authtypes.ModuleName] = v68AuthVersion
	after := v68ModuleVersions(t, chain)
	chain.Record(t, "module_versions_after", after)
	require.Equal(t, want, after,
		"v6.8 changed the version map beyond removing vesting and moving auth to %d", v68AuthVersion)
	require.Empty(t, chain.QueryStore(t, upgradetypes.StoreKey, v68ModuleVersionKey(v68VestingModule)),
		"v6.8 upgrade store still carries the vesting version-map entry")

	vestingCommand := chain.Seid("", "tx", v68VestingModule)
	chain.WriteDiagnostic(t, "v68-tx-vesting.stdout", []byte(vestingCommand.Stdout))
	chain.WriteDiagnostic(t, "v68-tx-vesting.stderr", []byte(vestingCommand.Stderr))
	require.Error(t, vestingCommand.Err, "v6.8 still serves the vesting transaction command")
	require.Contains(t, vestingCommand.Combined(), `unknown command "vesting"`)

	receiverBefore := v68UseiBalance(t, chain, v68PostUpgradeBankReceiver.String())
	chain.RequireDeliverTxSuccess(t, "v6.8 bank send", chain.Seid(v68KeyringPassword,
		"tx", "bank", "send", "admin", v68PostUpgradeBankReceiver.String(), v68PostUpgradeSendAmount,
		"--from", "admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	))
	sent, err := sdk.ParseCoinNormalized(v68PostUpgradeSendAmount)
	require.NoError(t, err)
	require.Equal(t, receiverBefore.Add(sent.Amount).String(),
		v68UseiBalance(t, chain, v68PostUpgradeBankReceiver.String()).String(),
		"the bank send after v6.8 did not credit the receiver")

	chain.StopNode(t)

	currentGenesis := chain.Export(t, v68RunningSeid, "v68-export")
	require.NotContains(t, currentGenesis.AppState, v68VestingModule,
		"v6.8 export still carries a vesting section")
	releaseGenesis := chain.Export(t, chain.ReleaseBinary(t), "v67-export-after-v68")
	require.Contains(t, releaseGenesis.AppState, authtypes.ModuleName,
		"v6.7 cannot export the accounts v6.8 left behind")
}

// v68ModuleVersions returns the on-chain module version map by module name.
func v68ModuleVersions(t *testing.T, chain *upgradetest.CrossVersion) map[string]uint64 {
	t.Helper()
	output := chain.MustSeid(t, "", "q", "upgrade", "module_versions", "--output", "json")
	var response struct {
		ModuleVersions []struct {
			Name    string          `json:"name"`
			Version json.RawMessage `json:"version"`
		} `json:"module_versions"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &response), output)
	versions := make(map[string]uint64, len(response.ModuleVersions))
	for _, entry := range response.ModuleVersions {
		version, err := strconv.ParseUint(strings.Trim(string(entry.Version), `"`), 10, 64)
		require.NoError(t, err, "module %s has version %s", entry.Name, entry.Version)
		versions[entry.Name] = version
	}
	return versions
}

func v68ModuleVersionKey(module string) []byte {
	return append([]byte{upgradetypes.VersionMapByte}, []byte(module)...)
}

func v68AppliedHeight(t *testing.T, chain *upgradetest.CrossVersion) int64 {
	t.Helper()
	output := chain.MustSeid(t, "", "q", "upgrade", "applied", v68UpgradeName, "--output", "json")
	var applied struct {
		Header struct {
			Height json.RawMessage `json:"height"`
		} `json:"header"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &applied), output)
	height, err := strconv.ParseInt(strings.Trim(string(applied.Header.Height), `"`), 10, 64)
	require.NoError(t, err, "applied height %s", applied.Header.Height)
	return height
}

func v68UseiBalance(t *testing.T, chain *upgradetest.CrossVersion, address string) sdk.Int {
	t.Helper()
	output := chain.MustSeid(t, "", "q", "bank", "balances", address, "--denom", "usei", "--output", "json")
	var coin struct {
		Amount json.RawMessage `json:"amount"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &coin), output)
	amount, ok := sdk.NewIntFromString(strings.Trim(string(coin.Amount), `"`))
	require.True(t, ok, "invalid usei balance %s", coin.Amount)
	return amount
}
