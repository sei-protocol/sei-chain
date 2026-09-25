//go:build upgrade_v68

package app_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/retiredoracle"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/upgradetest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

const v68UpgradeName = "v6.8"

// v68RemovedModules are the module version map entries the v6.8 handler deletes
// along with their stores.
var v68RemovedModules = []string{"capability", "ibc", "oracle", "transfer"}

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
	for _, module := range v68RemovedModules {
		versions[module] = 1
	}
	app.UpgradeKeeper.SetModuleVersionMap(app.Ctx(), versions)
	applyV68(t, app)
	once := app.UpgradeKeeper.GetModuleVersionMap(app.Ctx())
	for _, module := range v68RemovedModules {
		require.NotContains(t, once, module)
	}
	require.Contains(t, once, "bank")
	require.NotPanics(t, func() { applyV68(t, app) })
	require.Equal(t, once, app.UpgradeKeeper.GetModuleVersionMap(app.Ctx()))
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

// TestV68RewritesRetiredIBCProposals pins that a stored proposal whose content
// type lives under an IBC protobuf package reads back as a text proposal with
// the original title and description, with its status, deposits and votes
// untouched, and that unrelated proposals are left alone.
func TestV68RewritesRetiredIBCProposals(t *testing.T) {
	app := newV68Chain(t)
	ctx := app.Ctx()
	text, err := govtypes.NewProposal(
		govtypes.NewTextProposal("keep", "unrelated", false), 41, ctx.BlockTime(), ctx.BlockTime().Add(time.Hour), false)
	require.NoError(t, err)
	app.GovKeeper.SetProposal(ctx, text)

	retired := text
	retired.ProposalId = 42
	retired.Status = govtypes.StatusPassed
	retired.Content = &codectypes.Any{
		TypeUrl: "/ibc.core.client.v1.ClientUpdateProposal",
		Value:   v68EncodeStrings("Recover client", "Substitute 07-tendermint-0", "07-tendermint-0", "07-tendermint-1"),
	}
	voter := app.NewAccount()
	store := ctx.KVStore(app.GetKey(govtypes.StoreKey))
	store.Set(govtypes.ProposalKey(retired.ProposalId), app.GovKeeper.MustMarshalProposal(retired))
	app.GovKeeper.SetVote(ctx, govtypes.NewVote(retired.ProposalId, voter, govtypes.NewNonSplitVoteOption(govtypes.OptionYes)))
	require.Panics(t, func() { app.GovKeeper.GetProposal(ctx, retired.ProposalId) },
		"retired IBC proposal content is still decodable before the upgrade")

	applyV68(t, app)

	proposal, found := app.GovKeeper.GetProposal(ctx, retired.ProposalId)
	require.True(t, found)
	require.Equal(t, govtypes.StatusPassed, proposal.Status)
	require.Equal(t, "/cosmos.gov.v1beta1.TextProposal", proposal.Content.TypeUrl)
	require.Equal(t, "Recover client", proposal.GetTitle())
	require.Equal(t, "Substitute 07-tendermint-0", proposal.GetContent().GetDescription())
	_, found = app.GovKeeper.GetVote(ctx, retired.ProposalId, voter)
	require.True(t, found)
	kept, found := app.GovKeeper.GetProposal(ctx, text.ProposalId)
	require.True(t, found)
	require.Equal(t, app.GovKeeper.MustMarshalProposal(text), app.GovKeeper.MustMarshalProposal(kept))
	require.Len(t, app.GovKeeper.GetProposals(ctx), 2)
}

// v68EncodeStrings protobuf-encodes the given values as consecutive string
// fields numbered from 1.
func v68EncodeStrings(values ...string) []byte {
	var bz []byte
	for i, value := range values {
		bz = protowire.AppendTag(bz, protowire.Number(i+1), protowire.BytesType)
		bz = protowire.AppendString(bz, value)
	}
	return bz
}

// TestV68PrunesUpgradedIBCState pins that the upgrade store's upgraded IBC
// client and consensus state records are deleted while the rest of the store
// is kept.
func TestV68PrunesUpgradedIBCState(t *testing.T) {
	app := newV68Chain(t)
	ctx := app.Ctx()
	store := ctx.KVStore(app.GetKey(upgradetypes.StoreKey))
	store.Set([]byte("upgradedIBCState/100/upgradedClient"), []byte("client"))
	store.Set([]byte("upgradedIBCState/100/upgradedConsState"), []byte("consensus"))
	store.Set([]byte("upgradedIBCStateless"), []byte("unrelated"))

	applyV68(t, app)

	iterator := sdk.KVStorePrefixIterator(store, []byte("upgradedIBCState"))
	defer iterator.Close()
	require.False(t, iterator.Valid())
	require.NotEmpty(t, app.UpgradeKeeper.GetModuleVersionMap(ctx))
	name, _ := app.UpgradeKeeper.GetLastCompletedUpgrade(ctx)
	require.Equal(t, v68UpgradeName, name)
}

func TestV68OracleAbsentFromExportedGenesis(t *testing.T) {
	app := newV68Chain(t)
	applyV68(t, app)
	exported, err := app.ExportAppStateAndValidators(false, nil)
	require.NoError(t, err)
	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &state))
	_, found := state["bank"]
	require.True(t, found)
	_, found = state["oracle"]
	require.False(t, found)
}

func TestV68CrossVersion(t *testing.T) {
	upgradetest.RunCrossVersion(t,
		func(t *testing.T, chain *upgradetest.CrossVersion) {
			require.Equal(t, v68UpgradeName, chain.UpgradeName(t))
			for _, module := range v68RemovedModules {
				require.Contains(t, chain.ModuleVersions(t), module)
			}
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
			for _, module := range v68RemovedModules {
				require.NotContains(t, chain.ModuleVersions(t), module)
			}
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
			raw := chain.Binary("", "curl", "-s",
				"http://127.0.0.1:26657/abci_query?path=%2Fstore%2Foracle%2Fkey")
			require.NotContains(t, raw.Combined(), retiredoracle.ErrDeprecated.Error())
			require.Contains(t, raw.Combined(), "no such store: oracle")
			for _, store := range []string{"ibc", "transfer", "capability"} {
				raw := chain.Binary("", "curl", "-s",
					"http://127.0.0.1:26657/abci_query?path=%2Fstore%2F"+store+"%2Fkey")
				require.Contains(t, raw.Combined(), "no such store: "+store)
			}
		},
	)
}
