//go:build upgrade_v69

package app_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/retiredauthz"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
)

// v6.9 removes the authz module. The store loader deletes the authz store at
// the upgrade height, taking every outstanding grant with it, and the handler
// deletes the authz version-map entry. No other module version and no other
// store changes. These tests cover the handler, the refusal of every authz
// message after the upgrade, and the upgrade on a live chain that holds a grant.

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
)

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
// v6.8 carries beyond the modules v6.9 registers: authz at its v6.8 consensus
// version. Like every write through the harness context, it must happen before
// the chain's first block.
func seedV68VersionMap(t *testing.T, a *processblock.App) {
	t.Helper()
	versionMap := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	versionMap[retiredauthz.ModuleName] = v69AuthzVersionBefore
	a.UpgradeKeeper.SetModuleVersionMap(a.Ctx(), versionMap)
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

// TestV69RemovesOnlyTheAuthzModuleVersion applies v6.9 to the version map of a
// v6.8 chain and requires the authz entry to be gone and every other module to
// keep its version.
func TestV69RemovesOnlyTheAuthzModuleVersion(t *testing.T) {
	a := newV69Chain(t)
	seedV68VersionMap(t, a)
	a.RunBlock([]signing.Tx{})
	versionsBefore := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	require.Equal(t, v69AuthzVersionBefore, versionsBefore[retiredauthz.ModuleName])

	applyV69ToCommitStore(t, a)
	a.RunBlock([]signing.Tx{})

	versionsAfter := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	require.NotContains(t, versionsAfter, retiredauthz.ModuleName)
	delete(versionsBefore, retiredauthz.ModuleName)
	require.Equal(t, versionsBefore, versionsAfter, "v6.9 changed a module version other than removing authz")
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

// TestV69CrossVersion runs v6.8 validators into the v6.9 upgrade with a send
// authorization granted on chain, and verifies the same chain after they
// restart on v6.9.
func TestV69CrossVersion(t *testing.T) {
	upgradetest.RunCrossVersion(t, seedV68State, verifyV69State)
}

// seedV68State records the v6.8 version map, which carries authz, and has the
// admin account grant node_admin a send authorization that v6.8 serves.
func seedV68State(t *testing.T, chain *upgradetest.CrossVersion) {
	require.Equal(t, v69UpgradeName, chain.UpgradeName(t))

	versions := v69ModuleVersions(t, chain)
	require.Equal(t, v69AuthzVersionBefore, versions[retiredauthz.ModuleName],
		"v6.8 module version map does not carry authz")
	chain.Record(t, "module_versions", versions)

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
// gone with nothing else in the version map changed, and a bank send to succeed.
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
	after := v69ModuleVersions(t, chain)
	chain.Record(t, "module_versions_after", after)
	require.Equal(t, want, after, "v6.9 changed the version map beyond removing authz")
	require.Empty(t, chain.QueryStore(t, upgradetypes.StoreKey, v69ModuleVersionKey(retiredauthz.ModuleName)),
		"v6.9 upgrade store still carries the authz version-map entry")

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
