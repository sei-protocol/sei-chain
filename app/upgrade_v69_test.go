//go:build upgrade_v69

package app_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/sei-protocol/sei-chain/app/retiredauthz"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
)

// v6.9 removes the authz module and stores the disabled message type URLs. The
// store loader deletes the authz store at the upgrade height, taking every
// outstanding grant with it, and the handler deletes the authz version-map
// entry. The params 2 to 3 migration stores the disabled message type URLs, the
// params-module parameter that lists the messages a Cosmos transaction may not
// carry, as an empty list. Governance refuses a change to a parameter that is
// not stored, so before v6.9 no proposal can disable a message type. No other
// module version and no other store changes. These tests cover the handler, the
// refusal of every authz message after the upgrade, the stored parameter, a
// proposal that disables MsgSend once v6.9 has run, and the upgrade on a live
// chain that holds a grant.
//
// The v6.9 binary also runs Osaka and Sei's Amsterdam subset. Both are binary
// constants rather than height-gated, so they switch on when validators restart
// on v6.9 at the upgrade height. The cross-version test runs an Osaka opcode
// (CLZ) and a Sei Amsterdam opcode (DUPN) on the same node before and after.

const (
	v69UpgradeName       = "v6.9"
	v69TxFee             = 200000
	v69AccountBalance    = 1_000_000_000
	v69KeyringPassword   = "12345678\n"
	v69GrantSpendLimit   = "1000000usei"
	v69PostUpgradeAmount = "6969usei"

	v69SendAuthorizationTypeURL = "/cosmos.bank.v1beta1.SendAuthorization"
	// v69AuthzVersionBefore is the authz consensus version v6.8 records.
	v69AuthzVersionBefore uint64 = 1
	// v69ParamsVersionBefore and v69ParamsVersion are the params consensus
	// versions on either side of the upgrade.
	v69ParamsVersionBefore uint64 = 2
	v69ParamsVersion       uint64 = 3
)

// v69ClzCode returns CLZ(1), 255, as a 32-byte word. CLZ is an Osaka opcode.
var v69ClzCode = []byte{
	0x60, 0x01, // PUSH1 1
	0x1e,                                           // CLZ
	0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3, // MSTORE at 0, RETURN 32 bytes
}

// v69DupnCode returns 42 as a 32-byte word by duplicating the 17th stack item
// with DUPN, a Sei Amsterdam opcode (EIP-8024); immediate 0x80 encodes depth 17.
var v69DupnCode = append(append(
	[]byte{0x60, 0x2a}, // PUSH1 42
	v69PushZeros(16)...),
	0xe6, 0x80, // DUPN 17
	0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3, // MSTORE at 0, RETURN 32 bytes
)

var (
	v69ClzResult  = common.LeftPadBytes([]byte{0xff}, 32)
	v69DupnResult = common.LeftPadBytes([]byte{0x2a}, 32)
)

// v69PushZeros returns n PUSH1 0 instructions.
func v69PushZeros(n int) []byte {
	code := make([]byte, 0, 2*n)
	for range n {
		code = append(code, 0x60, 0x00)
	}
	return code
}

// v69DisabledMsgTypeURLsKey is the params-store key of the disabled message type URLs.
var v69DisabledMsgTypeURLsKey = []byte(paramstypes.ModuleName + "/" + string(paramstypes.ParamStoreKeyDisabledMsgTypeURLs))

var v69PostUpgradeBankReceiver = sdk.AccAddress{
	0x69, 0x69, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x69,
}

func newV69Chain(t *testing.T) *processblock.App {
	t.Helper()
	t.Setenv("UPGRADE_VERSION_LIST", v69UpgradeName)
	app := processblock.NewTestApp(t)
	processblock.CommonPreset(app)
	app.RegisterUpgradeHandlers()
	return app
}

func applyV69(t *testing.T, app *processblock.App) {
	t.Helper()
	app.UpgradeKeeper.ApplyUpgrade(app.Ctx(), upgradetypes.Plan{
		Name:   v69UpgradeName,
		Height: app.Ctx().BlockHeight(),
	})
}

// applyV69ToCommitStore applies the v6.9 handler against the committed multistore.
func applyV69ToCommitStore(t *testing.T, a *processblock.App) {
	t.Helper()
	// Writes into the deliver context the harness hands back are dropped rather
	// than committed, so the upgrade has to reach the commit multistore directly.
	ctx, write := a.NewUncachedContext(false, a.Ctx().BlockHeader()).CacheContext()
	a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{
		Name:   v69UpgradeName,
		Height: ctx.BlockHeight(),
	})
	write()
}

// seedV68VersionMap records in the version map what every chain upgrading from
// v6.8 carries: authz and params at their v6.8 consensus versions. Like every
// write through the harness context, it must happen before the chain's first
// block.
func seedV68VersionMap(t *testing.T, a *processblock.App) {
	t.Helper()
	versionMap := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	versionMap[retiredauthz.ModuleName] = v69AuthzVersionBefore
	versionMap[paramstypes.ModuleName] = v69ParamsVersionBefore
	a.UpgradeKeeper.SetModuleVersionMap(a.Ctx(), versionMap)
}

// seedV68Params records the v6.8 version map and removes the disabled message
// type URLs from the params store, which v6.8 does not store. Like every write
// through the harness context, it must happen before the chain's first block.
func seedV68Params(t *testing.T, a *processblock.App) {
	t.Helper()
	seedV68VersionMap(t, a)
	a.Ctx().KVStore(a.GetKey(paramstypes.StoreKey)).Delete(v69DisabledMsgTypeURLsKey)
}

func v69StoreEntries(t *testing.T, store sdk.KVStore) map[string][]byte {
	t.Helper()
	iterator := store.Iterator(nil, nil)
	defer func() { require.NoError(t, iterator.Close()) }()
	entries := map[string][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		entries[string(iterator.Key())] = append([]byte(nil), iterator.Value()...)
	}
	return entries
}

func TestV69UnupgradedBinaryHaltsAtPlanHeight(t *testing.T) {
	t.Setenv("UPGRADE_VERSION_LIST", "v6.8")
	a := processblock.NewTestApp(t)
	processblock.CommonPreset(a)
	a.RegisterUpgradeHandlers()
	require.False(t, a.UpgradeKeeper.HasHandler(v69UpgradeName))
	require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(a.Ctx(), upgradetypes.Plan{
		Name: v69UpgradeName, Height: 3,
	}))
	a.RunBlock(nil)
	a.RunBlock(nil)
	require.Panics(t, func() { a.RunBlock(nil) })
}

// TestV69RunsOsakaAndSeiAmsterdamOpcodes runs CLZ and DUPN on the v6.9 binary
// after the upgrade.
func TestV69RunsOsakaAndSeiAmsterdamOpcodes(t *testing.T) {
	a := newV69Chain(t)
	clz := common.HexToAddress("0x00000000000000000000000000000000006969c1")
	dupn := common.HexToAddress("0x00000000000000000000000000000000006969d1")
	a.EvmKeeper.SetCode(a.Ctx(), clz, v69ClzCode)
	a.EvmKeeper.SetCode(a.Ctx(), dupn, v69DupnCode)
	a.RunBlock([]signing.Tx{})
	applyV69ToCommitStore(t, a)
	a.RunBlock([]signing.Tx{})

	caller := a.NewAccount()
	for name, want := range map[string]struct {
		addr   common.Address
		result []byte
	}{
		"CLZ":  {clz, v69ClzResult},
		"DUPN": {dupn, v69DupnResult},
	} {
		t.Run(name, func(t *testing.T) {
			// An undefined opcode consumes the whole gas limit, which the keeper
			// reports by panicking out of gas.
			ctx := a.Ctx().WithGasMeter(sdk.NewGasMeter(10_000_000, 1, 1))
			var ret []byte
			var err error
			require.NotPanics(t, func() {
				ret, err = a.EvmKeeper.StaticCallEVM(ctx, caller, &want.addr, nil)
			}, "v6.9 does not run %s", name)
			require.NoError(t, err, "v6.9 does not run %s", name)
			require.Equal(t, want.result, ret)
		})
	}
}

// TestV69ChangesOnlyTheAuthzAndParamsModuleVersions applies v6.9 to the version
// map of a v6.8 chain and requires the authz entry to be gone, params to move to
// consensus version 3, and every other module to keep its version.
func TestV69ChangesOnlyTheAuthzAndParamsModuleVersions(t *testing.T) {
	a := newV69Chain(t)
	seedV68VersionMap(t, a)
	a.RunBlock([]signing.Tx{})
	versionsBefore := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	require.Equal(t, v69AuthzVersionBefore, versionsBefore[retiredauthz.ModuleName])
	require.Equal(t, v69ParamsVersionBefore, versionsBefore[paramstypes.ModuleName])

	applyV69ToCommitStore(t, a)
	a.RunBlock([]signing.Tx{})

	versionsAfter := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	require.NotContains(t, versionsAfter, retiredauthz.ModuleName)
	require.Equal(t, v69ParamsVersion, versionsAfter[paramstypes.ModuleName])
	delete(versionsBefore, retiredauthz.ModuleName)
	delete(versionsBefore, paramstypes.ModuleName)
	delete(versionsAfter, paramstypes.ModuleName)
	require.Equal(t, versionsBefore, versionsAfter,
		"v6.9 changed a module version other than removing authz and moving params")
	name, _ := a.UpgradeKeeper.GetLastCompletedUpgrade(a.Ctx())
	require.Equal(t, v69UpgradeName, name)
}

// TestV69RejectsAuthzTxsWithoutCharging delivers and checks a signed
// transaction of every authz message after v6.9. Each decodes as its retired
// type, which fails validation, so it is refused before any fee is taken.
func TestV69RejectsAuthzTxsWithoutCharging(t *testing.T) {
	a := newV69Chain(t)
	signer := a.NewSignableAccount("v69/authz-signer")
	a.FundAccount(signer, v69AccountBalance)
	counterparty := a.NewAccount()
	a.RunBlock([]signing.Tx{})
	applyV69ToCommitStore(t, a)
	a.RunBlock([]signing.Tx{})

	sendTypeURL := sdk.MsgTypeURL(&banktypes.MsgSend{})
	authorization, err := codectypes.NewAnyWithValue(&retiredauthz.GenericAuthorization{Msg: sendTypeURL})
	require.NoError(t, err)
	send, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		FromAddress: counterparty.String(),
		ToAddress:   signer.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 1)),
	})
	require.NoError(t, err)
	retired := []struct {
		name string
		msg  sdk.Msg
	}{
		{"grant", &retiredauthz.MsgGrant{
			Granter: signer.String(),
			Grantee: counterparty.String(),
			Grant:   retiredauthz.Grant{Authorization: authorization, Expiration: a.Ctx().BlockTime().Add(time.Hour)},
		}},
		{"exec", &retiredauthz.MsgExec{Grantee: signer.String(), Msgs: []*codectypes.Any{send}}},
		{"revoke", &retiredauthz.MsgRevoke{Granter: signer.String(), Grantee: counterparty.String(), MsgTypeUrl: sendTypeURL}},
	}
	for _, retired := range retired {
		t.Run(retired.name, func(t *testing.T) {
			tx := a.Sign(signer, v69TxFee, retired.msg)
			balanceBefore := a.BankKeeper.GetBalance(a.Ctx(), signer, "usei")

			results := a.RunBlockDetailed([]signing.Tx{tx})
			require.Len(t, results, 1)
			require.Equal(t, retiredauthz.ErrDeprecated.ABCICode(), results[0].Code, results[0].Log)
			require.Equal(t, retiredauthz.ErrDeprecated.Codespace(), results[0].Codespace)
			require.Equal(t, balanceBefore, a.BankKeeper.GetBalance(a.Ctx(), signer, "usei"),
				"the refused authz message was charged a fee")

			txBytes, err := processblock.TxConfig.TxEncoder()(tx)
			require.NoError(t, err)
			check := a.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: txBytes})
			require.Equal(t, retiredauthz.ErrDeprecated.ABCICode(), check.Code, check.Log)
			require.Equal(t, retiredauthz.ErrDeprecated.Codespace(), check.Codespace)
		})
	}
}

// TestV69StoresTheDisabledMsgTypeURLs requires v6.9 to store the disabled
// message type URLs as an empty list in a params store that lacks them, as v6.8
// left it, and to change nothing else in the params store.
func TestV69StoresTheDisabledMsgTypeURLs(t *testing.T) {
	a := newV69Chain(t)
	seedV68Params(t, a)
	store := a.Ctx().KVStore(a.GetKey(paramstypes.StoreKey))
	want := v69StoreEntries(t, store)
	want[string(v69DisabledMsgTypeURLsKey)] = []byte("[]")

	applyV69(t, a)

	require.Equal(t, want, v69StoreEntries(t, store),
		"v6.9 changed the params store beyond storing the disabled message type URLs")
	require.Empty(t, a.ParamsKeeper.GetDisabledMsgTypeURLs(a.Ctx()))
}

// TestV69LetsGovernanceDisableMessageTypes submits a parameter change proposal
// that disables MsgSend before and after v6.9, executes the one governance
// accepts, and delivers a bank send.
func TestV69LetsGovernanceDisableMessageTypes(t *testing.T) {
	a := newV69Chain(t)
	seedV68Params(t, a)
	sender := a.NewSignableAccount("v69/sender")
	a.FundAccount(sender, v69AccountBalance)
	msgSend := sdk.MsgTypeURL(&banktypes.MsgSend{})
	disableMsgSend := proposal.NewParameterChangeProposal("Disable MsgSend", "Bank sends move to EVM transfers",
		[]proposal.ParamChange{proposal.NewParamChange(
			paramstypes.ModuleName, string(paramstypes.ParamStoreKeyDisabledMsgTypeURLs), fmt.Sprintf("[%q]", msgSend),
		)}, false)
	_, err := a.GovKeeper.SubmitProposal(a.Ctx(), disableMsgSend)
	require.ErrorContains(t, err, "not found in subspace params")

	applyV69(t, a)
	_, err = a.GovKeeper.SubmitProposal(a.Ctx(), disableMsgSend)
	require.NoError(t, err)
	require.NoError(t, params.NewParamChangeProposalHandler(a.ParamsKeeper)(a.Ctx(), disableMsgSend))
	require.Equal(t, []string{msgSend}, a.ParamsKeeper.GetDisabledMsgTypeURLs(a.Ctx()))

	results := a.RunBlockDetailed([]signing.Tx{
		a.Sign(sender, v69TxFee, banktypes.NewMsgSend(sender, a.NewAccount(), sdk.NewCoins(sdk.NewInt64Coin("usei", 1)))),
	})
	require.Len(t, results, 1)
	require.Equal(t, sdkerrors.ErrMsgTypeDisabled.ABCICode(), results[0].Code, results[0].Log)
	require.Equal(t, sdkerrors.RootCodespace, results[0].Codespace)
}

// TestV69CrossVersion runs v6.8 validators into the v6.9 upgrade with a send
// authorization granted on chain, and verifies the same chain after they
// restart on v6.9.
func TestV69CrossVersion(t *testing.T) {
	upgradetest.RunCrossVersion(t, seedV68State, verifyV69State)
}

// seedV68State records the v6.8 version map, which carries authz and params at
// consensus version 2, requires v6.8 not to store the disabled message type
// URLs, requires v6.8 to refuse CLZ and DUPN, and has the admin account grant
// node_admin a send authorization that v6.8 serves.
func seedV68State(t *testing.T, chain *upgradetest.CrossVersion) {
	require.Equal(t, v69UpgradeName, chain.UpgradeName(t))

	versions := v69ModuleVersions(t, chain)
	require.Equal(t, v69AuthzVersionBefore, versions[retiredauthz.ModuleName],
		"v6.8 module version map does not carry authz")
	require.Equal(t, v69ParamsVersionBefore, versions[paramstypes.ModuleName])
	require.Empty(t, chain.QueryStore(t, paramstypes.StoreKey, v69DisabledMsgTypeURLsKey),
		"v6.8 already stores the disabled message type URLs")
	chain.Record(t, "module_versions", versions)

	for name, code := range map[string][]byte{"CLZ": v69ClzCode, "DUPN": v69DupnCode} {
		_, callErr := v69EthCallCreate(t, chain, code)
		require.Contains(t, callErr, "invalid opcode", "v6.8 runs %s", name)
	}

	granter := chain.KeyAddress(t, "sei-node-0", "admin")
	grantee := chain.KeyAddress(t, "sei-node-0", "node_admin")
	chain.Record(t, "authz_granter", granter)
	chain.Record(t, "authz_grantee", grantee)
	chain.RequireDeliverTxSuccess(t, "v6.8 authz grant", chain.Seid(v69KeyringPassword,
		"tx", "authz", "grant", grantee, "send",
		"--spend-limit", v69GrantSpendLimit,
		"--from", "admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	))
	grants := chain.MustSeid(t, "", "q", "authz", "grants", granter, grantee, "--output", "json")
	chain.WriteDiagnostic(t, "v68-authz-grants.json", []byte(grants))
	require.Contains(t, grants, v69SendAuthorizationTypeURL, "v6.8 does not serve the send authorization")
}

// verifyV69State requires the upgrade to have applied at the planned height on
// every validator, the authz version-map entry, store, and query command to be
// gone, params to be at consensus version 3 with nothing else in the version map
// changed, the disabled message type URLs to be stored as an empty list, CLZ and
// DUPN to run, and a bank send to succeed.
func verifyV69State(t *testing.T, chain *upgradetest.CrossVersion) {
	require.Equal(t, v69UpgradeName, chain.UpgradeName(t))

	appliedHeight := v69AppliedHeight(t, chain)
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
	delete(want, retiredauthz.ModuleName)
	want[paramstypes.ModuleName] = v69ParamsVersion
	after := v69ModuleVersions(t, chain)
	chain.Record(t, "module_versions_after", after)
	require.Equal(t, want, after,
		"v6.9 changed the version map beyond removing authz and moving params to %d", v69ParamsVersion)
	require.Empty(t, chain.QueryStore(t, upgradetypes.StoreKey, v69ModuleVersionKey(retiredauthz.ModuleName)),
		"v6.9 upgrade store still carries the authz version-map entry")
	require.Equal(t, []byte("[]"), chain.QueryStore(t, paramstypes.StoreKey, v69DisabledMsgTypeURLsKey),
		"v6.9 did not store the disabled message type URLs as an empty list")

	var granter, grantee string
	chain.Replay(t, "authz_granter", &granter)
	chain.Replay(t, "authz_grantee", &grantee)
	grantsQuery := chain.Seid("", "q", "authz", "grants", granter, grantee, "--output", "json")
	chain.WriteDiagnostic(t, "v69-authz-grants.stdout", []byte(grantsQuery.Stdout))
	chain.WriteDiagnostic(t, "v69-authz-grants.stderr", []byte(grantsQuery.Stderr))
	require.Error(t, grantsQuery.Err, "v6.9 still serves the authz query command")
	require.Contains(t, grantsQuery.Combined(), `unknown command "authz"`)
	authzStore := chain.Binary("", "curl", "-s",
		"http://127.0.0.1:26657/abci_query?path=%2Fstore%2F"+retiredauthz.ModuleName+"%2Fkey")
	require.Contains(t, authzStore.Combined(), "no such store: "+retiredauthz.ModuleName)

	for name, want := range map[string]struct{ code, result []byte }{
		"CLZ":  {v69ClzCode, v69ClzResult},
		"DUPN": {v69DupnCode, v69DupnResult},
	} {
		result, callErr := v69EthCallCreate(t, chain, want.code)
		require.Empty(t, callErr, "v6.9 does not run %s", name)
		require.Equal(t, want.result, result, "%s result", name)
	}

	receiverBefore := v69UseiBalance(t, chain, v69PostUpgradeBankReceiver.String())
	chain.RequireDeliverTxSuccess(t, "v6.9 bank send", chain.Seid(v69KeyringPassword,
		"tx", "bank", "send", "admin", v69PostUpgradeBankReceiver.String(), v69PostUpgradeAmount,
		"--from", "admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	))
	sent, err := sdk.ParseCoinNormalized(v69PostUpgradeAmount)
	require.NoError(t, err)
	require.Equal(t, receiverBefore.Add(sent.Amount).String(),
		v69UseiBalance(t, chain, v69PostUpgradeBankReceiver.String()).String(),
		"the bank send after v6.9 did not credit the receiver")
}

// v69ModuleVersions returns the on-chain module version map by module name.
func v69ModuleVersions(t *testing.T, chain *upgradetest.CrossVersion) map[string]uint64 {
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

func v69ModuleVersionKey(module string) []byte {
	return append([]byte{upgradetypes.VersionMapByte}, []byte(module)...)
}

func v69AppliedHeight(t *testing.T, chain *upgradetest.CrossVersion) int64 {
	t.Helper()
	output := chain.MustSeid(t, "", "q", "upgrade", "applied", v69UpgradeName, "--output", "json")
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

func v69UseiBalance(t *testing.T, chain *upgradetest.CrossVersion, address string) sdk.Int {
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

// v69EthCallCreate simulates a contract creation with code as its initcode over
// the validator's EVM RPC. It returns the returned bytes, or the RPC error
// message when the call fails.
func v69EthCallCreate(t *testing.T, chain *upgradetest.CrossVersion, code []byte) ([]byte, string) {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_call",
		"params":  []any{map[string]string{"data": hexutil.Encode(code), "gas": "0x100000"}, "latest"},
	})
	require.NoError(t, err)
	result := chain.Binary("", "curl", "-s", "-X", "POST",
		"-H", "content-type: application/json",
		"--data", string(request),
		"http://127.0.0.1:8545")
	require.NoError(t, result.Err, result.Combined())
	var response struct {
		Result hexutil.Bytes `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Stdout), &response), result.Stdout)
	if response.Error != nil {
		return nil, response.Error.Message
	}
	return response.Result, ""
}
