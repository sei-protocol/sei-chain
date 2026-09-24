//go:build upgrade_v68

package app_test

import (
	"encoding/json"
	"testing"

	"github.com/sei-protocol/sei-chain/app/retiredoracle"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
)

const v68UpgradeName = "v6.8"

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

func TestV68UnupgradedBinaryHaltsAtPlanHeight(t *testing.T) {
	t.Setenv("UPGRADE_VERSION_LIST", "v6.7")
	a := processblock.NewTestApp(t)
	processblock.CommonPreset(a)
	a.RegisterUpgradeHandlers()
	require.False(t, a.UpgradeKeeper.HasHandler(v68UpgradeName))
	require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(a.Ctx(), upgradetypes.Plan{
		Name: v68UpgradeName, Height: 3,
	}))
	a.RunBlock(nil)
	a.RunBlock(nil)
	require.Panics(t, func() { a.RunBlock(nil) })
}

func TestV68ApplyUpgradeTwice(t *testing.T) {
	app := newV68Chain(t)
	versions := app.UpgradeKeeper.GetModuleVersionMap(app.Ctx())
	versions["oracle"] = 1
	app.UpgradeKeeper.SetModuleVersionMap(app.Ctx(), versions)
	applyV68(t, app)
	once := app.UpgradeKeeper.GetModuleVersionMap(app.Ctx())
	require.NotContains(t, once, "oracle")
	require.NotPanics(t, func() { applyV68(t, app) })
	require.Equal(t, once, app.UpgradeKeeper.GetModuleVersionMap(app.Ctx()))
}

func TestV68DeletesOracleStore(t *testing.T) {
	app := newV68Chain(t)
	require.Nil(t, app.GetKey("oracle"))
	applyV68(t, app)
	require.Nil(t, app.GetKey("oracle"))
}

// TestV68RejectsOracleTxsWithoutCharging pins that retired oracle transactions
// are refused before fees are charged.
func TestV68RejectsOracleTxsWithoutCharging(t *testing.T) {
	app := newV68Chain(t)
	applyV68(t, app)
	signers := []sdk.AccAddress{
		app.NewSignableAccount("oracle-spammer-1"),
		app.NewSignableAccount("oracle-spammer-2"),
	}
	for _, signer := range signers {
		app.FundAccount(signer, 1000000000)
	}
	before := make([]sdk.Coin, len(signers))
	txs := []signing.Tx{
		app.Sign(signers[0], 200000, retiredoracle.NewMsgAggregateExchangeRateVote(
			"1.5uatom", signers[0], sdk.ValAddress(signers[0]))),
		app.Sign(signers[1], 200000, retiredoracle.NewMsgDelegateFeedConsent(
			sdk.ValAddress(signers[1]), signers[1])),
	}
	for i, signer := range signers {
		before[i] = app.BankKeeper.GetBalance(app.Ctx(), signer, "usei")
	}

	results := app.RunBlockDetailed(txs)
	require.Len(t, results, len(txs))
	for i, result := range results {
		require.Equal(t, uint32(retiredoracle.ErrDeprecated.ABCICode()), result.Code)
		require.Equal(t, retiredoracle.ErrDeprecated.Codespace(), result.Codespace)
		require.Contains(t, result.Log, retiredoracle.ErrDeprecated.Error())
		require.Equal(t, before[i], app.BankKeeper.GetBalance(app.Ctx(), signers[i], "usei"))
	}

	for _, tx := range txs {
		txBytes, err := processblock.TxConfig.TxEncoder()(tx)
		require.NoError(t, err)
		check := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: txBytes})
		require.Equal(t, uint32(retiredoracle.ErrDeprecated.ABCICode()), check.Code)
		require.Equal(t, retiredoracle.ErrDeprecated.Codespace(), check.Codespace)
	}
}

func TestV68OracleAbsentFromExportedGenesis(t *testing.T) {
	app := newV68Chain(t)
	applyV68(t, app)
	exported, err := app.ExportAppStateAndValidators(false, nil)
	require.NoError(t, err)
	var state struct {
		AppState map[string]json.RawMessage `json:"app_state"`
	}
	require.NoError(t, json.Unmarshal(exported.AppState, &state))
	_, found := state.AppState["oracle"]
	require.False(t, found)
}

func TestV68CrossVersion(t *testing.T) {
	upgradetest.RunCrossVersion(t,
		func(t *testing.T, chain *upgradetest.CrossVersion) {
			require.Equal(t, v68UpgradeName, chain.UpgradeName(t))
			require.Contains(t, chain.ModuleVersions(t), "oracle")
			chain.Record(t, "oracle-present", true)
			receiver := chain.KeyAddress(t, "sei-node-0", "node_admin")
			chain.Record(t, "receiver", receiver)
			chain.RequireDeliverTxSuccess(t, "v6.7 bank send", chain.Seid(
				"12345678\n",
				"tx", "bank", "send", "admin", receiver, "1usei",
				"--from", "admin", "--chain-id", "sei", "--fees", "200000usei",
				"--gas", "2000000", "--broadcast-mode", "sync", "--yes", "--output", "json",
			))
		},
		func(t *testing.T, chain *upgradetest.CrossVersion) {
			require.Equal(t, v68UpgradeName, chain.UpgradeName(t))
			require.NotContains(t, chain.ModuleVersions(t), "oracle")
			var receiver string
			chain.Replay(t, "receiver", &receiver)
			chain.RequireDeliverTxSuccess(t, "v6.8 bank send", chain.Seid(
				"12345678\n",
				"tx", "bank", "send", "admin", receiver, "1usei",
				"--from", "admin", "--chain-id", "sei", "--fees", "200000usei",
				"--gas", "2000000", "--broadcast-mode", "sync", "--yes", "--output", "json",
			))
			result := chain.Seid("", "q", "oracle")
			require.Error(t, result.Err)
			require.Contains(t, result.Combined(), `unknown command "oracle"`)
			raw := chain.Binary("", "curl", "-sf",
				"http://127.0.0.1:26657/abci_query?path=%2Fstore%2Foracle%2Fkey")
			require.NotContains(t, raw.Combined(), retiredoracle.ErrDeprecated.Error())
		},
	)
}
