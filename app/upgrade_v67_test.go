//go:build upgrade_v67

package app_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	serverconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/address"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/msgs"
	"github.com/sei-protocol/sei-chain/upgradetest"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

// v6.7 retires the feegrant, capability, ibc and transfer modules, and
// deprecates the oracle handlers. These tests cover opaque writes into the
// retired stores, transactions aimed at retired surfaces, ordinary
// transactions after the handler, bank balances left behind by IBC transfer,
// and the upgrade handler itself.

const (
	v67UpgradeName     = "v6.7"
	v67KeyringPassword = "12345678\n"
	txFee              = 200000
	// v67FeegrantAllowancePrefix is the fee-allowance key prefix.
	v67FeegrantAllowancePrefix byte = 0x00
	// DenomTrace.IBCDenom lives on the pre-upgrade branch.
	v67IBCVoucherDenomShape        = "ibc/0000000000000000000000000000000000000000000000000000000000000067"
	v67IBCVoucherShapeAmount int64 = 1_234_567
	v67EscrowStyleAmount     int64 = 7_777_777
	v67EscrowStyleSendAmount       = "1234567usei"
	v67PostUpgradeSendAmt    int64 = 4242
	v67PostUpgradeSendAmount       = "4242usei"
	v67EVMNativeSendTo             = "0x0000000000000000000000000000000000000067"
)

// GetEscrowAddress lives on the pre-upgrade branch; this is not a real escrow account.
var v67EscrowStyleAddress = sdk.AccAddress{
	0xec, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x67,
}

var v67PostUpgradeBankReceiver = sdk.AccAddress{
	0x42, 0x42, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x67,
}

// retiredModuleTx is a transaction aimed at a module surface v6.7 retires,
// paired with the rejection the v6.7 binary must produce for it.
type retiredModuleTx struct {
	name string
	// sign builds the transaction. Each case signs from an account of its own so
	// that a rejection which consumes no sequence number cannot disturb the
	// other cases sharing the block.
	sign func(a *processblock.App, signer, granter sdk.AccAddress) signing.Tx
	// code and codespace are the ABCI rejection the transaction must produce.
	code      uint32
	codespace string
	// logContains is a distinguishing fragment of the rejection message.
	logContains string
	// chargesFee records whether the sender still pays for the failed
	// transaction. Rejections raised by the message handler run after the fee
	// deduction; rejections raised by ValidateBasic run before it.
	chargesFee bool
}

func retiredModuleTxCases() []retiredModuleTx {
	return []retiredModuleTx{
		{
			name: "oracle aggregate exchange rate vote",
			sign: func(a *processblock.App, signer, _ sdk.AccAddress) signing.Tx {
				return a.Sign(signer, txFee, oracletypes.NewMsgAggregateExchangeRateVote(
					"1.5uatom", signer, sdk.ValAddress(signer)))
			},
			code:        uint32(oracletypes.ErrOracleDeprecated.ABCICode()),
			codespace:   oracletypes.ErrOracleDeprecated.Codespace(),
			logContains: "oracle module is deprecated",
			chargesFee:  true,
		},
		{
			name: "oracle delegate feed consent",
			sign: func(a *processblock.App, signer, _ sdk.AccAddress) signing.Tx {
				return a.Sign(signer, txFee, oracletypes.NewMsgDelegateFeedConsent(
					sdk.ValAddress(signer), signer))
			},
			code:        uint32(oracletypes.ErrOracleDeprecated.ABCICode()),
			codespace:   oracletypes.ErrOracleDeprecated.Codespace(),
			logContains: "oracle module is deprecated",
			chargesFee:  true,
		},
		{
			name: "transaction nominating a distinct fee granter",
			sign: func(a *processblock.App, signer, granter sdk.AccAddress) signing.Tx {
				return a.SignWithFeeGranter(signer, granter, txFee,
					oracletypes.NewMsgDelegateFeedConsent(sdk.ValAddress(signer), signer))
			},
			code:        uint32(sdkerrors.ErrInvalidRequest.ABCICode()),
			codespace:   sdkerrors.ErrInvalidRequest.Codespace(),
			logContains: "fee grants are not enabled",
			chargesFee:  false,
		},
		{
			// The granter field stays on the wire even though nothing grants
			// any more, so a client that always populates it with its own
			// address keeps working. Only a granter that differs from the payer
			// is refused.
			name: "transaction nominating itself as fee granter",
			sign: func(a *processblock.App, signer, _ sdk.AccAddress) signing.Tx {
				return a.SignWithFeeGranter(signer, signer, txFee,
					msgs.Send(signer, signer, 1))
			},
			code:       abci.CodeTypeOK,
			codespace:  "",
			chargesFee: true,
		},
	}
}

// retiredModuleTxAccounts holds one signer and one fee granter per case.
type retiredModuleTxAccounts struct {
	signers  []sdk.AccAddress
	granters []sdk.AccAddress
}

// fundRetiredModuleTxAccounts creates and funds the accounts one phase needs.
//
// Every phase's accounts must be created before the first block runs. The
// process block harness hands out the deliver context of the block it just
// committed, so an account created after that point is written into a cache
// that has already been flushed and never reaches committed state.
func fundRetiredModuleTxAccounts(a *processblock.App, phase string) retiredModuleTxAccounts {
	cases := retiredModuleTxCases()
	accounts := retiredModuleTxAccounts{
		signers:  make([]sdk.AccAddress, len(cases)),
		granters: make([]sdk.AccAddress, len(cases)),
	}
	for i, c := range cases {
		accounts.signers[i] = a.NewSignableAccount(phase + "/signer/" + c.name)
		accounts.granters[i] = a.NewSignableAccount(phase + "/granter/" + c.name)
		a.FundAccount(accounts.signers[i], 1000000000)
		a.FundAccount(accounts.granters[i], 1000000000)
	}
	return accounts
}

type ordinaryTxAccounts struct {
	bankSender   sdk.AccAddress
	bankReceiver sdk.AccAddress
	evmSender    sdk.AccAddress
}

func fundOrdinaryTxAccounts(a *processblock.App, phase string) ordinaryTxAccounts {
	accounts := ordinaryTxAccounts{
		bankSender:   a.NewSignableAccount(phase + "/bank-sender"),
		bankReceiver: a.NewAccount(),
		evmSender:    a.NewSignableAccount(phase + "/evm-sender"),
	}
	a.FundAccount(accounts.bankSender, 1000000000)
	a.FundAccount(accounts.evmSender, 1000000000)
	return accounts
}

// newV67Chain returns a chain whose only registered upgrade is v6.7, so that
// applying it exercises the handler under test rather than an earlier one. The
// common preset supplies the bonded validators a block needs a proposer from.
func newV67Chain(t *testing.T) *processblock.App {
	t.Helper()
	t.Setenv("UPGRADE_VERSION_LIST", v67UpgradeName)
	a := processblock.NewTestApp(t)
	processblock.CommonPreset(a)
	a.RegisterUpgradeHandlers()
	requireV67GoLayerExecutionConfig(t, a)
	return a
}

func applyV67(t *testing.T, a *processblock.App) {
	t.Helper()
	a.UpgradeKeeper.ApplyUpgrade(a.Ctx(), upgradetypes.Plan{
		Name:   v67UpgradeName,
		Height: a.Ctx().BlockHeight(),
	})
}

// applyV67ToCommitStore applies the v6.7 handler against the committed multistore.
func applyV67ToCommitStore(t *testing.T, a *processblock.App) {
	t.Helper()
	// Writes into the deliver context the harness hands back are dropped rather
	// than committed, so a committed-store assertion needs the upgrade applied
	// through a context that reaches the commit multistore.
	ctx, write := a.NewUncachedContext(false, a.Ctx().BlockHeader()).CacheContext()
	a.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{
		Name:   v67UpgradeName,
		Height: ctx.BlockHeight(),
	})
	write()
}

// newV67ChainWithoutHandler returns a chain whose registered upgrade list does
// not contain v6.7.
func newV67ChainWithoutHandler(t *testing.T) *processblock.App {
	t.Helper()
	t.Setenv("UPGRADE_VERSION_LIST", "v6.6")
	a := processblock.NewTestApp(t)
	processblock.CommonPreset(a)
	a.RegisterUpgradeHandlers()
	requireV67GoLayerExecutionConfig(t, a)
	return a
}

// requireV67GoLayerExecutionConfig asserts that in-process upgrade tests run
// with OCC disabled and DefaultConcurrencyWorkers. The fleet sets occ-enabled
// = true and the live harness sets concurrency-workers = 4.
func requireV67GoLayerExecutionConfig(t *testing.T, a *processblock.App) {
	t.Helper()
	require.False(t, a.OccEnabled(),
		"in-process v6.7 tests ran with BaseApp.OccEnabled()=%v, want false; this layer's application-hash determinism is not the fleet's (occ-enabled = true)",
		a.OccEnabled())
	require.Equal(t, serverconfig.DefaultConcurrencyWorkers, a.ConcurrencyWorkers(),
		"in-process v6.7 tests ran with BaseApp.ConcurrencyWorkers()=%d, want DefaultConcurrencyWorkers=%d; the live harness sets 4",
		a.ConcurrencyWorkers(), serverconfig.DefaultConcurrencyWorkers)
}

func finalizeV67Block(a *processblock.App, height int64) (*abci.ResponseFinalizeBlock, error) {
	votes := a.GetVotes()
	var proposer []byte
	if len(votes) > 0 {
		proposer = votes[0].Validator.Address
	}
	return a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		DecidedLastCommit:   abci.CommitInfo{Round: 0, Votes: votes},
		ByzantineValidators: []abci.Misbehavior{},
		Hash:                []byte("abc"),
		Header: &tmproto.Header{
			ChainID:         a.ChainID,
			Height:          height,
			ProposerAddress: proposer,
			Time:            time.Now(),
		},
	})
}

func requireV67UpgradeNeededPanic(t *testing.T, panicked any, height int64) {
	t.Helper()
	require.NotNil(t, panicked, "the v6.7 plan height was produced without a handler")
	msg := fmt.Sprint(panicked)
	require.Contains(t, msg, `UPGRADE "v6.7" NEEDED`,
		"halt panic is missing the upgrade name: %v", panicked)
	require.Contains(t, msg, fmt.Sprintf("height: %d", height),
		"halt panic is missing plan height %d: %v", height, panicked)
}

func scheduleV67Plan(t *testing.T, a *processblock.App, height int64) {
	t.Helper()
	require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(a.Ctx(), upgradetypes.Plan{
		Name:   v67UpgradeName,
		Height: height,
	}))
	plan, found := a.UpgradeKeeper.GetUpgradePlan(a.Ctx())
	require.True(t, found)
	require.Equal(t, v67UpgradeName, plan.Name)
	require.Equal(t, height, plan.Height)
}

// TestV67UnupgradedBinaryHaltsAtPlanHeight asserts that a binary without a v6.7
// handler panics at the plan height without committing, and that a binary with
// the handler runs that height.
func TestV67UnupgradedBinaryHaltsAtPlanHeight(t *testing.T) {
	t.Run("without handler", func(t *testing.T) {
		a := newV67ChainWithoutHandler(t)
		require.False(t, a.UpgradeKeeper.HasHandler(v67UpgradeName))

		const planHeight int64 = 3
		scheduleV67Plan(t, a, planHeight)
		a.RunBlock([]signing.Tx{})
		a.RunBlock([]signing.Tx{})
		require.Equal(t, planHeight-1, a.Ctx().BlockHeight())
		_, havePlan := a.UpgradeKeeper.GetUpgradePlan(a.Ctx())
		require.True(t, havePlan, "scheduled v6.7 plan was not committed")

		var panicked any
		var finalizeErr error
		func() {
			defer func() { panicked = recover() }()
			_, finalizeErr = finalizeV67Block(a, planHeight)
		}()
		require.NoError(t, finalizeErr, "FinalizeBlock returned an error instead of panicking")
		requireV67UpgradeNeededPanic(t, panicked, planHeight)
		require.Equal(t, planHeight-1, a.Ctx().BlockHeight())
		require.Equal(t, planHeight-1, a.LastBlockHeight(),
			"an un-upgraded binary committed the v6.7 plan height")
		require.Zero(t, a.UpgradeKeeper.GetDoneHeight(a.Ctx(), v67UpgradeName),
			"an un-upgraded binary applied v6.7")
		_, havePlan = a.UpgradeKeeper.GetUpgradePlan(a.Ctx())
		require.True(t, havePlan, "an un-upgraded binary cleared the v6.7 plan")
	})

	t.Run("with handler", func(t *testing.T) {
		a := newV67Chain(t)
		require.True(t, a.UpgradeKeeper.HasHandler(v67UpgradeName))

		const planHeight int64 = 1
		scheduleV67Plan(t, a, planHeight)
		require.NotPanics(t, func() { a.RunBlock([]signing.Tx{}) })
		require.Equal(t, planHeight, a.Ctx().BlockHeight())
		require.Equal(t, planHeight, a.UpgradeKeeper.GetDoneHeight(a.Ctx(), v67UpgradeName))
		_, havePlan := a.UpgradeKeeper.GetUpgradePlan(a.Ctx())
		require.False(t, havePlan)
	})
}

// TestV67ApplyUpgradeTwice applies the v6.7 handler a second time against the
// state the first application produced.
func TestV67ApplyUpgradeTwice(t *testing.T) {
	a := newV67Chain(t)
	seeded := seedRetiredStores(t, a)

	applyV67(t, a)
	onceVersions := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	onceDone := a.UpgradeKeeper.GetDoneHeight(a.Ctx(), v67UpgradeName)
	onceAppVersion := a.AppVersion()
	onceStores := snapshotDeliverRetiredStores(t, a)
	_, havePlan := a.UpgradeKeeper.GetUpgradePlan(a.Ctx())
	require.False(t, havePlan)
	require.Equal(t, a.Ctx().BlockHeight(), onceDone)
	for _, store := range retiredStoreKeys {
		require.NotContains(t, onceVersions, store)
		require.Equal(t, seeded[store], onceStores[store]["seeded"])
	}

	require.NotPanics(t, func() { applyV67(t, a) })

	require.Equal(t, onceVersions, a.UpgradeKeeper.GetModuleVersionMap(a.Ctx()),
		"second ApplyUpgrade changed the module version map")
	require.Equal(t, onceDone, a.UpgradeKeeper.GetDoneHeight(a.Ctx(), v67UpgradeName),
		"second ApplyUpgrade changed the done height")
	require.Equal(t, onceStores, snapshotDeliverRetiredStores(t, a),
		"second ApplyUpgrade changed retired store state")
	_, havePlan = a.UpgradeKeeper.GetUpgradePlan(a.Ctx())
	require.False(t, havePlan)
	require.Equal(t, onceAppVersion+1, a.AppVersion(),
		"second ApplyUpgrade is not a no-op: ApplyUpgrade increments protocol version on every call")
}

// TestV67CrossVersion creates retired-module state with the v6.6 binary and
// verifies the same chain after its validators restart on v6.7.
func TestV67CrossVersion(t *testing.T) {
	upgradetest.RunCrossVersion(t, seedV66State, verifyV67State)
}

func seedV66State(t *testing.T, chain *upgradetest.CrossVersion) {
	require.Equal(t, v67UpgradeName, chain.UpgradeName(t))
	chain.Record(t, v67LiveHarnessConfigKey, v67ReadLiveHarnessConfig(t, chain))

	granter := chain.KeyAddress(t, "sei-node-0", "admin")
	grantee := chain.KeyAddress(t, "sei-node-0", "node_admin")
	chain.Record(t, "feegrant_granter", granter)
	chain.Record(t, "feegrant_grantee", grantee)

	grant := chain.Seid(v67KeyringPassword,
		"tx", "feegrant", "grant", granter, grantee,
		"--spend-limit", "100000000usei",
		"--from", "admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	chain.RequireDeliverTxSuccess(t, "v6.6 fee allowance grant", grant)

	allowanceBefore := chain.MustSeid(t, "",
		"q", "feegrant", "grant", granter, grantee, "--output", "json")
	spendLimitBefore := v67FeegrantSpendLimit(t, allowanceBefore)
	chain.WriteDiagnostic(t, "v66-feegrant-before-spend.json", []byte(allowanceBefore))

	spend := chain.Seid(v67KeyringPassword,
		"tx", "bank", "send", "node_admin", granter, "1usei",
		"--fee-account", granter,
		"--from", "node_admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	chain.RequireDeliverTxSuccess(t, "v6.6 fee-granted bank send", spend)

	allowanceAfter := chain.MustSeid(t, "",
		"q", "feegrant", "grant", granter, grantee, "--output", "json")
	spendLimitAfter := v67FeegrantSpendLimit(t, allowanceAfter)
	require.True(t, spendLimitAfter.LT(spendLimitBefore),
		"the v6.6 transaction did not spend its fee allowance: %s to %s",
		spendLimitBefore, spendLimitAfter)
	chain.WriteDiagnostic(t, "v66-feegrant-after-spend.json", []byte(allowanceAfter))

	oracleQuery := chain.Seid("", "q", "oracle", "exchange-rates", "--output", "json")
	chain.WriteDiagnostic(t, "v66-oracle-query.stdout", []byte(oracleQuery.Stdout))
	chain.WriteDiagnostic(t, "v66-oracle-query.stderr", []byte(oracleQuery.Stderr))
	require.NoError(t, oracleQuery.Err, "v6.6 must still expose the oracle query")

	seedV66EscrowShapedBankState(t, chain)

	moduleVersions := chain.ModuleVersions(t)
	for _, module := range append(retiredStoreKeys, oracletypes.ModuleName) {
		require.Contains(t, moduleVersions, module,
			"v6.6 module version map does not contain %s", module)
		require.NotEmpty(t, chain.QueryStore(t, upgradetypes.StoreKey, v67ModuleVersionKey(module)),
			"v6.6 upgrade store is missing a version-map entry for %s", module)
	}
	chain.Record(t, "module_versions", moduleVersions)

	feegrantKey := v67FeegrantAllowanceKey(t, granter, grantee)
	feegrantValue := chain.QueryStore(t, keys.FeegrantStoreKey, feegrantKey)
	require.NotEmpty(t, feegrantValue, "v6.6 feegrant store does not serve the granted allowance")
	chain.Record(t, "feegrant_store_key", feegrantKey)
	chain.Record(t, "feegrant_store_value", feegrantValue)

	preserveV67UnupgradedHome(t, chain)
}

func verifyV67State(t *testing.T, chain *upgradetest.CrossVersion) {
	require.Equal(t, v67UpgradeName, chain.UpgradeName(t))

	var beforeConfig map[string]v67ValidatorRuntimeConfig
	chain.Replay(t, v67LiveHarnessConfigKey, &beforeConfig)
	require.Equal(t, beforeConfig, v67ReadLiveHarnessConfig(t, chain),
		"the upgrade changed a validator's SeiDB, OCC, or pruning settings; every other assertion in this run is about a configuration the binary is no longer running")

	planName := requireV67RecordedPlanName(t, chain)
	appliedOutput := chain.MustSeid(t, "",
		"q", "upgrade", "applied", planName, "--output", "json")
	var applied struct {
		Header struct {
			Height json.RawMessage `json:"height"`
		} `json:"header"`
	}
	require.NoError(t, json.Unmarshal([]byte(appliedOutput), &applied))
	appliedHeight := v67JSONInt(t, applied.Header.Height)
	require.Equal(t, chain.TargetHeight(t), appliedHeight)
	chain.Record(t, "applied_height", appliedHeight)
	// A header carries the application hash of the state its parent produced, so
	// the upgrade block's own result appears one height above the applied height.
	chain.RequireBlockAgreement(t,
		appliedHeight,
		appliedHeight+1,
		appliedHeight+2,
		appliedHeight+3,
	)

	var beforeVersions []string
	chain.Replay(t, "module_versions", &beforeVersions)
	afterVersions := chain.ModuleVersions(t)
	chain.Record(t, "module_versions_after", afterVersions)
	require.Equal(t,
		sortedStrings(retiredStoreKeys),
		stringDifference(beforeVersions, afterVersions),
		"v6.7 removed an unexpected set of module versions",
	)
	require.Contains(t, afterVersions, oracletypes.ModuleName,
		"v6.7 removed oracle even though its blocker is still registered")

	requireV67UpgradeStoreVersions(t, chain)
	require.NotEmpty(t, chain.QueryStore(t, upgradetypes.StoreKey, v67ModuleVersionKey(oracletypes.ModuleName)),
		"v6.7 removed oracle from the upgrade store even though its blocker is still registered")

	var feegrantKey []byte
	var feegrantValue []byte
	chain.Replay(t, "feegrant_store_key", &feegrantKey)
	chain.Replay(t, "feegrant_store_value", &feegrantValue)
	require.Equal(t, feegrantValue, chain.QueryStore(t, keys.FeegrantStoreKey, feegrantKey),
		"v6.7 node no longer serves the feegrant store value written by v6.6")
	chain.WaitForBlocks(t, 5)
	require.Equal(t, feegrantValue, chain.QueryStore(t, keys.FeegrantStoreKey, feegrantKey),
		"a later block changed the retained feegrant store")
	requireV67UpgradeStoreVersions(t, chain)

	var granter string
	var grantee string
	chain.Replay(t, "feegrant_granter", &granter)
	chain.Replay(t, "feegrant_grantee", &grantee)

	feegrantSpend := chain.Seid(v67KeyringPassword,
		"tx", "bank", "send", "node_admin", granter, "1usei",
		"--fee-account", granter,
		"--from", "node_admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	chain.WriteDiagnostic(t, "v67-feegrant-spend.stdout", []byte(feegrantSpend.Stdout))
	chain.WriteDiagnostic(t, "v67-feegrant-spend.stderr", []byte(feegrantSpend.Stderr))
	require.Contains(t, feegrantSpend.Combined(), "fee grants are not enabled",
		"v6.7 accepted the fee-granted transaction that v6.6 executed")

	feegrantQuery := chain.Seid("",
		"q", "feegrant", "grant", granter, grantee, "--output", "json")
	chain.WriteDiagnostic(t, "v67-feegrant-query.stdout", []byte(feegrantQuery.Stdout))
	chain.WriteDiagnostic(t, "v67-feegrant-query.stderr", []byte(feegrantQuery.Stderr))
	require.Error(t, feegrantQuery.Err, "v6.7 still exposes the retired feegrant query command")

	oracleQuery := chain.Seid("", "q", "oracle", "exchange-rates", "--output", "json")
	chain.WriteDiagnostic(t, "v67-oracle-query.stdout", []byte(oracleQuery.Stdout))
	chain.WriteDiagnostic(t, "v67-oracle-query.stderr", []byte(oracleQuery.Stderr))
	require.Contains(t, oracleQuery.Combined(), "oracle module is deprecated")

	requireV67EscrowShapedBankState(t, chain)
	requireV67IBCTransferQueriesGone(t, chain)

	requireV67PostUpgradeTxs(t, chain)

	requireV67CrashRecovery(t, chain)
	requireV67OldBinaryOnMigratedNode(t, chain)
	requireV67UnupgradedBinaryHalts(t, chain)

	chain.StopNode(t)

	currentGenesis := chain.Export(t, v67RunningSeid, "v67-export")
	for _, module := range retiredStoreKeys {
		require.NotContains(t, currentGenesis.AppState, module,
			"v6.7 export unexpectedly contains retired module %s", module)
	}

	releaseGenesis := chain.Export(t, chain.ReleaseBinary(t), "v66-export-after-v67")
	for _, module := range retiredStoreKeys {
		require.Contains(t, releaseGenesis.AppState, module,
			"v6.6 cannot read retained %s state after the v6.7 store reload", module)
	}
	require.Positive(t, v67ExportedFeegrantAllowanceCount(t, releaseGenesis),
		"the fee allowance written by v6.6 did not survive v6.7")
}

func v67FeegrantSpendLimit(t *testing.T, output string) sdk.Int {
	t.Helper()
	var value any
	require.NoError(t, json.Unmarshal([]byte(output), &value))
	amount, ok := findV67SpendLimit(value)
	require.True(t, ok, "feegrant response has no usei spend limit: %s", output)
	return amount
}

func findV67SpendLimit(value any) (sdk.Int, bool) {
	switch value := value.(type) {
	case map[string]any:
		if spendLimit, ok := value["spend_limit"].([]any); ok {
			for _, coin := range spendLimit {
				fields, ok := coin.(map[string]any)
				if !ok || fields["denom"] != "usei" {
					continue
				}
				if amount, ok := sdk.NewIntFromString(fmt.Sprint(fields["amount"])); ok {
					return amount, true
				}
			}
		}
		for _, nested := range value {
			if amount, ok := findV67SpendLimit(nested); ok {
				return amount, true
			}
		}
	case []any:
		for _, nested := range value {
			if amount, ok := findV67SpendLimit(nested); ok {
				return amount, true
			}
		}
	}
	return sdk.Int{}, false
}

func v67ExportedFeegrantAllowanceCount(t *testing.T, genesis upgradetest.ExportedGenesis) int {
	t.Helper()
	var feegrant struct {
		Allowances []json.RawMessage `json:"allowances"`
	}
	require.NoError(t, json.Unmarshal(genesis.AppState[keys.FeegrantStoreKey], &feegrant))
	return len(feegrant.Allowances)
}

func v67JSONInt(t *testing.T, encoded json.RawMessage) int64 {
	t.Helper()
	var text string
	if err := json.Unmarshal(encoded, &text); err == nil {
		value, parseErr := strconv.ParseInt(text, 10, 64)
		require.NoError(t, parseErr)
		return value
	}
	var value int64
	require.NoError(t, json.Unmarshal(encoded, &value))
	return value
}

func requireV67RecordedPlanName(t *testing.T, chain *upgradetest.CrossVersion) string {
	t.Helper()
	want := chain.UpgradeName(t)
	proposals := chain.MustSeid(t, "", "q", "gov", "proposals", "--output", "json")
	chain.WriteDiagnostic(t, "v67-gov-proposals.json", []byte(proposals))
	planName := softwareUpgradePlanName(t, proposals)
	require.Equal(t, want, planName)

	doneKey := append([]byte{upgradetypes.DoneByte}, []byte(planName)...)
	require.NotEmpty(t, chain.QueryStore(t, upgradetypes.StoreKey, doneKey),
		"upgrade store has no done entry for recorded plan name %q", planName)

	for _, miss := range upgradeNameNearMisses(planName) {
		missKey := append([]byte{upgradetypes.DoneByte}, []byte(miss)...)
		require.Empty(t, chain.QueryStore(t, upgradetypes.StoreKey, missKey),
			"upgrade store has a done entry for near-miss name %q", miss)
		applied := chain.Seid("", "q", "upgrade", "applied", miss, "--output", "json")
		require.Error(t, applied.Err, "applied query succeeded for near-miss name %q", miss)
		require.Contains(t, applied.Combined(), "no upgrade found")
	}
	return planName
}

func softwareUpgradePlanName(t *testing.T, proposalsJSON string) string {
	t.Helper()
	var envelope struct {
		Proposals []struct {
			Content struct {
				Type string `json:"@type"`
				Plan struct {
					Name string `json:"name"`
				} `json:"plan"`
			} `json:"content"`
		} `json:"proposals"`
	}
	require.NoError(t, json.Unmarshal([]byte(proposalsJSON), &envelope))
	var names []string
	for _, proposal := range envelope.Proposals {
		if !strings.HasSuffix(proposal.Content.Type, ".SoftwareUpgradeProposal") {
			continue
		}
		if strings.HasSuffix(proposal.Content.Type, ".CancelSoftwareUpgradeProposal") {
			continue
		}
		require.NotEmpty(t, proposal.Content.Plan.Name)
		names = append(names, proposal.Content.Plan.Name)
	}
	require.Len(t, names, 1, "want one software-upgrade proposal, got %q", names)
	return names[0]
}

func TestV67SoftwareUpgradePlanName(t *testing.T) {
	got := softwareUpgradePlanName(t, `{
  "proposals": [
    {"content": {"@type": "/cosmos.gov.v1beta1.TextProposal", "title": "x"}},
    {"content": {"@type": "/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal", "title": "v6.7.0"}},
    {"content": {"@type": "/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal", "title": "v6.7", "plan": {"name": "v6.7", "height": "42"}}}
  ]
}`)
	require.Equal(t, "v6.7", got)
}

func v67ModuleVersionKey(module string) []byte {
	return append([]byte{upgradetypes.VersionMapByte}, []byte(module)...)
}

func requireV67UpgradeStoreVersions(t *testing.T, chain *upgradetest.CrossVersion) {
	t.Helper()
	for _, module := range retiredStoreKeys {
		require.Empty(t, chain.QueryStore(t, upgradetypes.StoreKey, v67ModuleVersionKey(module)),
			"v6.7 upgrade store still has a version-map entry for %s", module)
	}
}

func v67FeegrantAllowanceKey(t *testing.T, granter, grantee string) []byte {
	t.Helper()
	granterAddr, err := sdk.AccAddressFromBech32(granter)
	require.NoError(t, err)
	granteeAddr, err := sdk.AccAddressFromBech32(grantee)
	require.NoError(t, err)
	// Grantee then granter, matching feegrant.FeeAllowanceKey on v6.6.
	key := []byte{v67FeegrantAllowancePrefix}
	key = append(key, address.MustLengthPrefix(granteeAddr)...)
	key = append(key, address.MustLengthPrefix(granterAddr)...)
	return key
}

type v67IBCTransferQuery struct {
	name string
	args []string
}

func v67IBCTransferQueries() []v67IBCTransferQuery {
	return []v67IBCTransferQuery{
		{name: "denom-traces", args: []string{"q", "ibc-transfer", "denom-traces", "--output", "json"}},
		{name: "params", args: []string{"q", "ibc-transfer", "params", "--output", "json"}},
		{name: "escrow-address", args: []string{"q", "ibc-transfer", "escrow-address", "transfer", "channel-0"}},
	}
}

// seedV66EscrowShapedBankState funds the v6.6 escrow address for
// transfer/channel-0 by a bank send and records that address and balance.
func seedV66EscrowShapedBankState(t *testing.T, chain *upgradetest.CrossVersion) {
	t.Helper()
	escrow := strings.TrimSpace(chain.MustSeid(t, "",
		"q", "ibc-transfer", "escrow-address", "transfer", "channel-0"))
	require.NotEmpty(t, escrow, "v6.6 escrow-address query returned no address")
	chain.Record(t, "escrow_style_address", escrow)
	chain.WriteDiagnostic(t, "v66-ibc-transfer-escrow-address.stdout", []byte(escrow+"\n"))

	send := chain.Seid(v67KeyringPassword,
		"tx", "bank", "send", "admin", escrow, v67EscrowStyleSendAmount,
		"--from", "admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	)
	chain.RequireDeliverTxSuccess(t, "v6.6 bank send to escrow-shaped address", send)

	balanceOut := chain.MustSeid(t, "",
		"q", "bank", "balances", escrow, "--denom", "usei", "--output", "json")
	chain.WriteDiagnostic(t, "v66-escrow-style-balance.json", []byte(balanceOut))
	_, balanceAmt := v67BankCoin(t, balanceOut)
	require.Equal(t, "1234567", balanceAmt,
		"the v6.6 bank send did not credit the escrow-shaped address")
	chain.Record(t, "escrow_style_balance", balanceAmt)

	supplyOut := chain.MustSeid(t, "",
		"q", "bank", "total", "--denom", "usei", "--output", "json")
	chain.WriteDiagnostic(t, "v66-usei-supply.json", []byte(supplyOut))
	_, supplyAmt := v67BankCoin(t, supplyOut)
	v67RequireSupplyCoversEscrow(t, supplyAmt, balanceAmt)

	for _, q := range v67IBCTransferQueries() {
		out := chain.MustSeid(t, "", q.args...)
		chain.WriteDiagnostic(t, "v66-ibc-transfer-"+q.name+".stdout", []byte(out))
	}
}

// requireV67EscrowShapedBankState asserts the recorded escrow-shaped address
// still holds its balance and that usei supply still covers that balance.
func requireV67EscrowShapedBankState(t *testing.T, chain *upgradetest.CrossVersion) {
	t.Helper()
	var escrow, wantBalance string
	chain.Replay(t, "escrow_style_address", &escrow)
	chain.Replay(t, "escrow_style_balance", &wantBalance)

	balanceOut := chain.MustSeid(t, "",
		"q", "bank", "balances", escrow, "--denom", "usei", "--output", "json")
	chain.WriteDiagnostic(t, "v67-escrow-style-balance.json", []byte(balanceOut))
	_, gotBalance := v67BankCoin(t, balanceOut)
	require.Equal(t, wantBalance, gotBalance,
		"v6.7 changed the escrow-shaped address balance")

	supplyOut := chain.MustSeid(t, "",
		"q", "bank", "total", "--denom", "usei", "--output", "json")
	chain.WriteDiagnostic(t, "v67-usei-supply.json", []byte(supplyOut))
	_, gotSupply := v67BankCoin(t, supplyOut)
	v67RequireSupplyCoversEscrow(t, gotSupply, wantBalance)
}

// v67RequireSupplyCoversEscrow asserts usei total supply is at least the
// escrow-shaped balance.
func v67RequireSupplyCoversEscrow(t *testing.T, supplyAmt, escrowAmt string) {
	t.Helper()
	supply, ok := sdk.NewIntFromString(supplyAmt)
	require.True(t, ok, "invalid usei supply %q", supplyAmt)
	escrow, ok := sdk.NewIntFromString(escrowAmt)
	require.True(t, ok, "invalid escrow-shaped balance %q", escrowAmt)
	require.True(t, supply.GTE(escrow),
		"usei total supply %s is below the escrow-shaped balance %s; inflation may raise supply, but the escrowed coins must still be counted",
		supplyAmt, escrowAmt)
}

// requireV67IBCTransferQueriesGone asserts that the v6.6 ibc-transfer query
// commands are no longer exposed.
func requireV67IBCTransferQueriesGone(t *testing.T, chain *upgradetest.CrossVersion) {
	t.Helper()
	for _, q := range v67IBCTransferQueries() {
		result := chain.Seid("", q.args...)
		chain.WriteDiagnostic(t, "v67-ibc-transfer-"+q.name+".stdout", []byte(result.Stdout))
		chain.WriteDiagnostic(t, "v67-ibc-transfer-"+q.name+".stderr", []byte(result.Stderr))
		require.Error(t, result.Err, "v6.7 still exposes q ibc-transfer %s", q.name)
	}
}

func v67BankCoin(t *testing.T, output string) (denom, amount string) {
	t.Helper()
	var coin struct {
		Denom  string          `json:"denom"`
		Amount json.RawMessage `json:"amount"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &coin), output)
	require.NotEmpty(t, coin.Denom, "bank coin JSON has no denom: %s", output)
	return coin.Denom, v67CoinAmount(t, coin.Amount).String()
}

// v67CoinAmount decodes a coin amount. A chain's supply reaches far beyond an
// int64, and proto JSON quotes the field as a string.
func v67CoinAmount(t *testing.T, encoded json.RawMessage) sdk.Int {
	t.Helper()
	text := strings.Trim(strings.TrimSpace(string(encoded)), `"`)
	amount, ok := sdk.NewIntFromString(text)
	require.True(t, ok, "invalid coin amount %s", encoded)
	return amount
}

// requireV67PostUpgradeTxs broadcasts a bank send and an EVM native-send after
// the upgrade and requires that each is included and changes state.
func requireV67PostUpgradeTxs(t *testing.T, chain *upgradetest.CrossVersion) {
	t.Helper()
	sender := chain.KeyAddress(t, chain.Node(), "admin")
	receiver := v67PostUpgradeBankReceiver.String()

	seqBefore := v67AccountSequence(t, chain, sender)
	recvBefore := v67UseiBalance(t, chain, receiver)

	chain.RequireDeliverTxSuccess(t, "v6.7 bank send", chain.Seid(v67KeyringPassword,
		"tx", "bank", "send", "admin", receiver, v67PostUpgradeSendAmount,
		"--from", "admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	))
	require.Equal(t, seqBefore+1, v67AccountSequence(t, chain, sender),
		"bank send did not advance the sender sequence")
	require.Equal(t, recvBefore.AddRaw(v67PostUpgradeSendAmt).String(),
		v67UseiBalance(t, chain, receiver).String(),
		"bank send did not credit the receiver")

	evmRecv := v67EVMCastSeiAddress(t, v67EVMNativeSendTo)
	evmBefore := v67UseiBalance(t, chain, evmRecv)
	chain.RequireDeliverTxSuccess(t, "v6.7 evm native-send", chain.Seid(v67KeyringPassword,
		"tx", "evm", "native-send", "admin", v67EVMNativeSendTo, v67PostUpgradeSendAmount,
		"--from", "admin",
		"--chain-id", "sei",
		"--fees", "200000usei",
		"--gas", "2000000",
		"--broadcast-mode", "sync",
		"--yes",
		"--output", "json",
	))
	require.Equal(t, seqBefore+2, v67AccountSequence(t, chain, sender),
		"evm native-send did not advance the sender sequence")
	require.Equal(t, evmBefore.AddRaw(v67PostUpgradeSendAmt).String(),
		v67UseiBalance(t, chain, evmRecv).String(),
		"evm native-send did not credit the cast Sei address")
}

func v67AccountSequence(t *testing.T, chain *upgradetest.CrossVersion, address string) int64 {
	t.Helper()
	out := chain.MustSeid(t, "", "q", "account", address, "--output", "json")
	var acc struct {
		Sequence json.RawMessage `json:"sequence"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &acc), out)
	if len(acc.Sequence) == 0 {
		return 0
	}
	return v67JSONInt(t, acc.Sequence)
}

func v67UseiBalance(t *testing.T, chain *upgradetest.CrossVersion, address string) sdk.Int {
	t.Helper()
	out := chain.MustSeid(t, "",
		"q", "bank", "balances", address, "--denom", "usei", "--output", "json")
	_, amount := v67BankCoin(t, out)
	value, ok := sdk.NewIntFromString(amount)
	require.True(t, ok, "invalid usei balance %q", amount)
	return value
}

func v67EVMCastSeiAddress(t *testing.T, evmHex string) string {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimPrefix(evmHex, "0x"))
	require.NoError(t, err, "decode EVM address %s", evmHex)
	require.Len(t, raw, 20, "EVM address %s is not 20 bytes", evmHex)
	return sdk.AccAddress(raw).String()
}

func stringDifference(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range right {
		rightSet[value] = struct{}{}
	}
	var difference []string
	for _, value := range left {
		if _, ok := rightSet[value]; !ok {
			difference = append(difference, value)
		}
	}
	sort.Strings(difference)
	return difference
}

func sortedStrings(values []string) []string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return sorted
}

// runRetiredModuleTxs submits one transaction per case in a single block and
// asserts the rejection and fee outcome each case declares.
func runRetiredModuleTxs(t *testing.T, a *processblock.App, phase string, accounts retiredModuleTxAccounts) {
	t.Helper()
	cases := retiredModuleTxCases()

	balancesBefore := make([]sdk.Coin, len(cases))
	txs := make([]signing.Tx, len(cases))
	for i, c := range cases {
		balancesBefore[i] = a.BankKeeper.GetBalance(a.Ctx(), accounts.signers[i], "usei")
		txs[i] = c.sign(a, accounts.signers[i], accounts.granters[i])
	}

	results := a.RunBlockDetailed(txs)
	require.Len(t, results, len(cases))

	for i, c := range cases {
		t.Run(phase+"/"+c.name, func(t *testing.T) {
			res := results[i]
			require.Equal(t, c.code, res.Code, "log was %q", res.Log)
			require.Equal(t, c.codespace, res.Codespace)
			require.Contains(t, res.Log, c.logContains)

			spent := balancesBefore[i].Sub(a.BankKeeper.GetBalance(a.Ctx(), accounts.signers[i], "usei"))
			if c.chargesFee {
				require.Equal(t, sdk.NewInt64Coin("usei", txFee), spent,
					"a transaction rejected by the message handler still pays its fee")
			} else {
				require.True(t, spent.IsZero(),
					"a transaction rejected before fee deduction must cost nothing, spent %s", spent)
			}
		})
	}
}

// runOrdinaryTxs submits a bank send and an EVM native send in one block and
// requires each delivered result to succeed and to move balances and sequence.
func runOrdinaryTxs(t *testing.T, a *processblock.App, phase string, accounts ordinaryTxAccounts) {
	t.Helper()
	const sendAmt int64 = 1000
	evmTo := common.HexToAddress(v67EVMNativeSendTo)
	evmRecv := sdk.AccAddress(evmTo.Bytes())

	bankSenderBefore := a.BankKeeper.GetBalance(a.Ctx(), accounts.bankSender, "usei")
	bankRecvBefore := a.BankKeeper.GetBalance(a.Ctx(), accounts.bankReceiver, "usei")
	bankSeqBefore := a.AccountKeeper.GetAccount(a.Ctx(), accounts.bankSender).GetSequence()
	evmSenderBefore := a.BankKeeper.GetBalance(a.Ctx(), accounts.evmSender, "usei")
	evmRecvBefore := a.BankKeeper.GetBalance(a.Ctx(), evmRecv, "usei")
	evmSeqBefore := a.AccountKeeper.GetAccount(a.Ctx(), accounts.evmSender).GetSequence()

	results := a.RunBlockDetailed([]signing.Tx{
		a.Sign(accounts.bankSender, txFee, msgs.Send(accounts.bankSender, accounts.bankReceiver, sendAmt)),
		a.Sign(accounts.evmSender, txFee, evmtypes.NewMsgSend(
			accounts.evmSender, evmTo, sdk.NewCoins(sdk.NewInt64Coin("usei", sendAmt)))),
	})
	require.Len(t, results, 2)

	t.Run(phase+"/bank send", func(t *testing.T) {
		require.Equal(t, uint32(abci.CodeTypeOK), results[0].Code, results[0].Log)
		require.Positive(t, results[0].GasUsed)
		require.Equal(t, sdk.NewInt64Coin("usei", sendAmt),
			a.BankKeeper.GetBalance(a.Ctx(), accounts.bankReceiver, "usei").Sub(bankRecvBefore))
		require.Equal(t, sdk.NewInt64Coin("usei", sendAmt+txFee),
			bankSenderBefore.Sub(a.BankKeeper.GetBalance(a.Ctx(), accounts.bankSender, "usei")))
		require.Equal(t, bankSeqBefore+1,
			a.AccountKeeper.GetAccount(a.Ctx(), accounts.bankSender).GetSequence())
	})
	t.Run(phase+"/evm native send", func(t *testing.T) {
		require.Equal(t, uint32(abci.CodeTypeOK), results[1].Code, results[1].Log)
		require.Positive(t, results[1].GasUsed)
		require.Equal(t, sdk.NewInt64Coin("usei", sendAmt),
			a.BankKeeper.GetBalance(a.Ctx(), evmRecv, "usei").Sub(evmRecvBefore))
		require.Equal(t, sdk.NewInt64Coin("usei", sendAmt+txFee),
			evmSenderBefore.Sub(a.BankKeeper.GetBalance(a.Ctx(), accounts.evmSender, "usei")))
		require.Equal(t, evmSeqBefore+1,
			a.AccountKeeper.GetAccount(a.Ctx(), accounts.evmSender).GetSequence())
	})
}

// Transactions aimed at retired modules must follow the v6.7 tombstone
// behavior after the upgrade handler runs. TestV67CrossVersion covers the
// behavior transition from the old binary.
func TestV67RejectsRetiredModuleTxsAfterUpgrade(t *testing.T) {
	a := newV67Chain(t)
	afterAccounts := fundRetiredModuleTxAccounts(a, "after-upgrade")

	applyV67(t, a)
	runRetiredModuleTxs(t, a, "after-upgrade", afterAccounts)
}

// TestV67AcceptsOrdinaryTxsAcrossUpgrade delivers a bank send and an EVM native
// send before and after applyV67.
func TestV67AcceptsOrdinaryTxsAcrossUpgrade(t *testing.T) {
	a := newV67Chain(t)
	before := fundOrdinaryTxAccounts(a, "before-upgrade")
	after := fundOrdinaryTxAccounts(a, "after-upgrade")

	runOrdinaryTxs(t, a, "before-upgrade", before)
	applyV67ToCommitStore(t, a)
	runOrdinaryTxs(t, a, "after-upgrade", after)
}

// Spamming a retired module is not free. Every oracle transaction is rejected,
// yet it still pays its fee, consumes a sequence number, and occupies block
// gas. This is what stops a retired handler from becoming a free denial of
// service, so it is asserted rather than left as an implementation detail.
func TestRetiredOracleTxsAreRejectedButStillCharged(t *testing.T) {
	a := newV67Chain(t)
	applyV67(t, a)

	const spamCount = 25
	spammer := a.NewSignableAccount("oracle-spammer")
	a.FundAccount(spammer, 1000000000)

	balanceBefore := a.BankKeeper.GetBalance(a.Ctx(), spammer, "usei")
	sequenceBefore := a.AccountKeeper.GetAccount(a.Ctx(), spammer).GetSequence()

	txs := make([]signing.Tx, spamCount)
	for i := range txs {
		txs[i] = a.Sign(spammer, txFee, oracletypes.NewMsgAggregateExchangeRateVote(
			"1.5uatom", spammer, sdk.ValAddress(spammer)))
	}

	for i, res := range a.RunBlockDetailed(txs) {
		require.Equal(t, uint32(oracletypes.ErrOracleDeprecated.ABCICode()), res.Code,
			"spam transaction %d: %s", i, res.Log)
		require.Positive(t, res.GasUsed, "spam transaction %d consumed no gas", i)
	}

	spent := balanceBefore.Sub(a.BankKeeper.GetBalance(a.Ctx(), spammer, "usei"))
	require.Equal(t, sdk.NewInt64Coin("usei", txFee*spamCount), spent)
	require.Equal(t, sequenceBefore+spamCount,
		a.AccountKeeper.GetAccount(a.Ctx(), spammer).GetSequence())

	// Nothing the spam carried reached oracle state.
	_, err := a.OracleKeeper.GetAggregateExchangeRateVote(a.Ctx(), sdk.ValAddress(spammer))
	require.ErrorIs(t, err, oracletypes.ErrNoAggregateVote,
		"a rejected vote must not be recorded")
}

// TestV67RetainsRetiredModuleStateWrittenBeforeUpgrade pins that v6.7 leaves
// the retired stores mounted with their committed key/value sets unchanged,
// and removes their version-map entries from the upgrade store's committed bytes.
func TestV67RetainsRetiredModuleStateWrittenBeforeUpgrade(t *testing.T) {
	a := newV67Chain(t)

	seeded := seedRetiredStores(t, a)
	a.RunBlock([]signing.Tx{})
	requireRetiredStoresMounted(t, a)
	before := snapshotRetiredStores(t, a)
	for _, store := range retiredStoreKeys {
		require.Equal(t, seeded[store], before[store]["seeded"],
			"seeded %s state is missing from the committed store", store)
		require.True(t, committedModuleVersionExists(t, a, store),
			"seeded %s module version is missing from the committed upgrade store", store)
		requireRetiredStoreInCommitment(t, a, store, []byte("seeded"), seeded[store])
	}

	applyV67ToCommitStore(t, a)
	a.RunBlock([]signing.Tx{})
	requireRetiredStoresUnchanged(t, a, before, seeded)

	for range 5 {
		a.RunBlock([]signing.Tx{})
	}
	requireRetiredStoresUnchanged(t, a, before, seeded)
}

// TestV67LeavesBankBalancesAndSupplyUntouched asserts that applying v6.7 does
// not move or burn bank balances or total supply, including an ibc/-shaped
// denom and coins at an escrow-style address.
func TestV67LeavesBankBalancesAndSupplyUntouched(t *testing.T) {
	a := newV67Chain(t)

	voucherHolder := a.NewAccount()
	a.FundAccountWithDenom(voucherHolder, v67IBCVoucherShapeAmount, v67IBCVoucherDenomShape)
	a.FundAccountWithDenom(v67EscrowStyleAddress, v67EscrowStyleAmount, "usei")

	voucherBefore := a.BankKeeper.GetBalance(a.Ctx(), voucherHolder, v67IBCVoucherDenomShape)
	escrowBefore := a.BankKeeper.GetBalance(a.Ctx(), v67EscrowStyleAddress, "usei")
	require.Equal(t, sdk.NewInt64Coin(v67IBCVoucherDenomShape, v67IBCVoucherShapeAmount), voucherBefore)
	require.Equal(t, sdk.NewInt64Coin("usei", v67EscrowStyleAmount), escrowBefore)
	require.Equal(t, voucherBefore, a.BankKeeper.GetSupply(a.Ctx(), v67IBCVoucherDenomShape))
	require.True(t, a.BankKeeper.GetSupply(a.Ctx(), "usei").IsGTE(escrowBefore))

	holderBalancesBefore := a.BankKeeper.GetAllBalances(a.Ctx(), voucherHolder)
	escrowBalancesBefore := a.BankKeeper.GetAllBalances(a.Ctx(), v67EscrowStyleAddress)
	suppliesBefore := v67BankSupplySnapshot(a)

	applyV67(t, a)

	require.Equal(t, suppliesBefore, v67BankSupplySnapshot(a),
		"v6.7 moved or burned bank supply")
	require.Equal(t, holderBalancesBefore, a.BankKeeper.GetAllBalances(a.Ctx(), voucherHolder),
		"v6.7 changed balances of an ibc/ voucher holder")
	require.Equal(t, escrowBalancesBefore, a.BankKeeper.GetAllBalances(a.Ctx(), v67EscrowStyleAddress),
		"v6.7 changed balances at an escrow-style address")
}

func v67BankSupplySnapshot(a *processblock.App) map[string]string {
	supplies := map[string]string{}
	a.BankKeeper.IterateTotalSupply(a.Ctx(), func(c sdk.Coin) bool {
		supplies[c.Denom] = c.Amount.String()
		return false
	})
	return supplies
}

func seedRetiredStores(t *testing.T, a *processblock.App) map[string][]byte {
	t.Helper()
	seeded := map[string][]byte{}
	versionMap := a.UpgradeKeeper.GetModuleVersionMap(a.Ctx())
	for _, store := range retiredStoreKeys {
		key := a.GetKey(store)
		require.NotNil(t, key, "%s store is no longer mounted", store)
		value := []byte("pre-upgrade/" + store)
		a.Ctx().KVStore(key).Set([]byte("seeded"), value)
		seeded[store] = value
		versionMap[store] = 1
	}
	a.UpgradeKeeper.SetModuleVersionMap(a.Ctx(), versionMap)
	return seeded
}

func requireRetiredStoresUnchanged(t *testing.T, a *processblock.App, before map[string]map[string][]byte, seeded map[string][]byte) {
	t.Helper()
	requireRetiredStoresMounted(t, a)
	require.Equal(t, before, snapshotRetiredStores(t, a),
		"a retired store's committed key/value set changed after v6.7")
	for _, store := range retiredStoreKeys {
		require.False(t, committedModuleVersionExists(t, a, store),
			"committed upgrade store still has a version-map entry for %s", store)
		requireRetiredStoreInCommitment(t, a, store, []byte("seeded"), seeded[store])
	}
}

const (
	v67ValidatorHome          = "/root/.sei"
	v67UnupgradedHaltNode     = "sei-node-3"
	v67UnupgradedHomeSnapshot = "/tmp/v67-unupgraded-home"
	v67UpgradedHomeSnapshot   = "/tmp/v67-upgraded-home"
	v67OldBinaryHomeSnapshot  = "/tmp/v67-pre-old-binary"
	v67RunningSeid            = "/root/go/bin/seid"
	v67LiveHarnessConfigKey   = "validator_runtime_config"
	v67LiveHarnessPruning     = "nothing"
)

const v67LiveHarnessPruningMessage = "the live upgrade harness pins pruning = %q so historical queries survive this run; mainnet validators prune, so this suite does not prove that a pruning validator still serves retained retired-store data after v6.7"

// v67ValidatorRuntimeConfig is the subset of a validator's app.toml that an
// upgrade must not silently change.
type v67ValidatorRuntimeConfig struct {
	SCEnable   bool   `json:"sc_enable"`
	SSEnable   bool   `json:"ss_enable"`
	OCCEnabled bool   `json:"occ_enabled"`
	Pruning    string `json:"pruning"`
}

// v67ReadLiveHarnessConfig reads each validator's running app.toml and requires
// SeiDB and OCC enabled and pruning = "nothing". Mainnet validators prune, so
// this cluster does not prove retained retired-store data survives on a pruning node.
func v67ReadLiveHarnessConfig(t *testing.T, chain *upgradetest.CrossVersion) map[string]v67ValidatorRuntimeConfig {
	t.Helper()
	nodes := chain.Nodes()
	configs := make(map[string]v67ValidatorRuntimeConfig, len(nodes))
	for _, node := range nodes {
		cfg := v67ReadValidatorRuntimeConfig(t, chain, node)
		v67RequireLiveHarnessSettings(t, node, cfg)
		configs[node] = cfg
	}
	return configs
}

func v67ReadValidatorRuntimeConfig(t *testing.T, chain *upgradetest.CrossVersion, node string) v67ValidatorRuntimeConfig {
	t.Helper()
	path := v67ValidatorHome + "/config/app.toml"
	result := chain.BinaryOn(node, "", "cat", path)
	chain.WriteDiagnostic(t, node+"-app.toml", []byte(result.Stdout))
	require.NoError(t, result.Err, "read %s %s: %s", node, path, result.Combined())
	require.NotEmpty(t, result.Stdout, "%s %s is empty", node, path)
	return v67ParseValidatorRuntimeConfig(t, node, result.Stdout)
}

func v67ParseValidatorRuntimeConfig(t *testing.T, node, tomlText string) v67ValidatorRuntimeConfig {
	t.Helper()
	return v67ValidatorRuntimeConfig{
		SCEnable:   v67TomlBool(t, node, tomlText, "sc-enable"),
		SSEnable:   v67TomlBool(t, node, tomlText, "ss-enable"),
		OCCEnabled: v67TomlBool(t, node, tomlText, "occ-enabled"),
		Pruning:    v67TomlScalar(t, node, tomlText, "pruning"),
	}
}

func v67RequireLiveHarnessSettings(t *testing.T, node string, cfg v67ValidatorRuntimeConfig) {
	t.Helper()
	require.True(t, cfg.SCEnable, "%s sc-enable=%v, want true", node, cfg.SCEnable)
	require.True(t, cfg.SSEnable, "%s ss-enable=%v, want true", node, cfg.SSEnable)
	require.True(t, cfg.OCCEnabled,
		"%s occ-enabled=%v, want true; the live cluster must run OCC because the fleet does",
		node, cfg.OCCEnabled)
	require.Equal(t, v67LiveHarnessPruning, cfg.Pruning,
		"%s pruning=%q, want %q; "+v67LiveHarnessPruningMessage,
		node, cfg.Pruning, v67LiveHarnessPruning, v67LiveHarnessPruning)
}

func v67TomlBool(t *testing.T, node, tomlText, key string) bool {
	t.Helper()
	raw := v67TomlScalar(t, node, tomlText, key)
	value, err := strconv.ParseBool(raw)
	require.NoError(t, err, "%s %s=%q is not a bool", node, key, raw)
	return value
}

func v67TomlScalar(t *testing.T, node, tomlText, key string) string {
	t.Helper()
	value, ok := v67LastTomlScalar(tomlText, key)
	require.True(t, ok, "%s app.toml has no %s key", node, key)
	return value
}

func v67LastTomlScalar(tomlText, key string) (string, bool) {
	found := false
	value := ""
	for _, line := range strings.Split(tomlText, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, " #"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		name, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(name) != key {
			continue
		}
		raw = strings.TrimSpace(raw)
		if unquoted, err := strconv.Unquote(raw); err == nil {
			raw = unquoted
		}
		value = raw
		found = true
	}
	return value, found
}

func TestV67TomlScalarParser(t *testing.T) {
	text := `
# occ-enabled = false
pruning = "nothing" # archive
occ-enabled = true
sc-enable = true
ss-enable = true

[giga_executor]
occ_enabled = false

[state-commit]
sc-enable = false
`
	sc, ok := v67LastTomlScalar(text, "sc-enable")
	require.True(t, ok)
	require.Equal(t, "false", sc)

	pruning, ok := v67LastTomlScalar(text, "pruning")
	require.True(t, ok)
	require.Equal(t, "nothing", pruning)

	occ, ok := v67LastTomlScalar(text, "occ-enabled")
	require.True(t, ok)
	require.Equal(t, "true", occ)

	_, ok = v67LastTomlScalar(text, "occ_enabled")
	require.True(t, ok)

	_, ok = v67LastTomlScalar(text, "concurrency-workers")
	require.False(t, ok)

	cfg := v67ParseValidatorRuntimeConfig(t, "fixture", `
pruning = "nothing"
occ-enabled = true
sc-enable = true
ss-enable = true
`)
	require.Equal(t, v67ValidatorRuntimeConfig{
		SCEnable:   true,
		SSEnable:   true,
		OCCEnabled: true,
		Pruning:    v67LiveHarnessPruning,
	}, cfg)
}

func copyV67ValidatorHome(t *testing.T, chain *upgradetest.CrossVersion, node, dst string) {
	t.Helper()
	result := chain.BinaryOn(node, "", "sh", "-c",
		fmt.Sprintf("rm -rf %s && cp -a %s %s", dst, v67ValidatorHome, dst))
	require.NoError(t, result.Err, "copy %s home to %s: %s", node, dst, result.Combined())
}

func replaceV67ValidatorHome(t *testing.T, chain *upgradetest.CrossVersion, node, src string) {
	t.Helper()
	result := chain.BinaryOn(node, "", "sh", "-c",
		fmt.Sprintf("rm -rf %s && mv %s %s", v67ValidatorHome, src, v67ValidatorHome))
	require.NoError(t, result.Err, "replace %s home from %s: %s", node, src, result.Combined())
}

func requireV67UpgradeNeededLog(t *testing.T, log string, height int64) {
	t.Helper()
	expected := fmt.Sprintf(`UPGRADE "%s" NEEDED at height: %d`, v67UpgradeName, height)
	escaped := fmt.Sprintf(`UPGRADE \"%s\" NEEDED at height: %d`, v67UpgradeName, height)
	require.True(t, strings.Contains(log, expected) || strings.Contains(log, escaped),
		"missing upgrade-needed halt at height %d\nlog:\n%s", height, log)
}

func restoreV67Validator(t *testing.T, chain *upgradetest.CrossVersion, node, snapshot string) {
	t.Helper()
	chain.StopNodeOn(t, node)
	replaceV67ValidatorHome(t, chain, node, snapshot)
	chain.StartNodeOn(t, node, v67RunningSeid)
	chain.RequireBlockAgreement(t, chain.Height(t))
}

// preserveV67UnupgradedHome copies a non-primary validator home while seid is
// stopped, then requires that validator to rejoin.
func preserveV67UnupgradedHome(t *testing.T, chain *upgradetest.CrossVersion) {
	t.Helper()
	require.NotEqual(t, chain.Node(), v67UnupgradedHaltNode)
	chain.StopNodeOn(t, v67UnupgradedHaltNode)
	copyV67ValidatorHome(t, chain, v67UnupgradedHaltNode, v67UnupgradedHomeSnapshot)
	chain.StartNodeOn(t, v67UnupgradedHaltNode, v67RunningSeid)
	chain.RequireBlockAgreement(t, chain.Height(t))
	chain.Record(t, "unupgraded_home_node", v67UnupgradedHaltNode)
}

// requireV67CrashRecovery asserts that a validator killed without a clean
// shutdown after the upgrade replays its last block on restart and agrees with
// its peers. It covers recovery near the boundary, not a crash inside the
// upgrade block, which no reliably timed test can produce.
func requireV67CrashRecovery(t *testing.T, chain *upgradetest.CrossVersion) {
	t.Helper()
	const peer = "sei-node-1"
	killedAt := chain.HeightOn(t, peer)
	chain.KillNodeOn(t, peer)
	chain.WaitForBlocks(t, 2)
	chain.StartNodeOn(t, peer, v67RunningSeid)
	chain.WaitForHeightOn(t, peer, killedAt, 3*time.Minute)
	chain.RequireBlockAgreement(t, chain.Height(t))
}

// requireV67OldBinaryOnMigratedNode starts the v6.6 binary against an upgraded
// validator database, then restores the v6.7 binary.
func requireV67OldBinaryOnMigratedNode(t *testing.T, chain *upgradetest.CrossVersion) {
	t.Helper()
	const node = "sei-node-2"
	require.NotEqual(t, chain.Node(), node)

	upgradeHeight := chain.TargetHeight(t)
	stoppedAt := chain.HeightOn(t, node)
	require.GreaterOrEqual(t, stoppedAt, upgradeHeight)

	chain.StopNodeOn(t, node)
	copyV67ValidatorHome(t, chain, node, v67OldBinaryHomeSnapshot)

	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		t.Logf("restoring v6.7 binary on %s after old-binary observation", node)
		restoreV67Validator(t, chain, node, v67OldBinaryHomeSnapshot)
	}
	defer restore()

	observed := chain.StartNodeObserving(t, node, chain.ReleaseBinary(t), 45*time.Second)
	chain.WriteDiagnostic(t, "v66-on-v67-restart.log", []byte(observed.Log))

	require.False(t, observed.Running,
		"v6.6 seid stayed up on a v6.7 database\nlog:\n%s", observed.Log)
	require.False(t, observed.Height > stoppedAt,
		"v6.6 seid advanced from height %d to %d on a v6.7 database\nlog:\n%s",
		stoppedAt, observed.Height, observed.Log)
	require.True(t,
		strings.Contains(observed.Log, "state.AppHash does not match AppHash after replay") ||
			strings.Contains(observed.Log, "upgrade handler is missing for v6.7 upgrade plan"),
		"v6.6 seid neither failed the handshake nor panicked on the missing v6.7 handler:\n%s", observed.Log)

	restore()
}

// requireV67UnupgradedBinaryHalts starts the v6.6 binary against a pre-upgrade
// validator home and requires it to halt at the v6.7 plan height.
func requireV67UnupgradedBinaryHalts(t *testing.T, chain *upgradetest.CrossVersion) {
	t.Helper()
	var node string
	chain.Replay(t, "unupgraded_home_node", &node)
	require.NotEqual(t, chain.Node(), node)
	require.Equal(t, v67UnupgradedHaltNode, node)

	upgradeHeight := chain.TargetHeight(t)
	chain.StopNodeOn(t, node)
	copyV67ValidatorHome(t, chain, node, v67UpgradedHomeSnapshot)
	replaceV67ValidatorHome(t, chain, node, v67UnupgradedHomeSnapshot)

	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		t.Logf("restoring upgraded home on %s after un-upgraded halt", node)
		restoreV67Validator(t, chain, node, v67UpgradedHomeSnapshot)
	}
	defer restore()

	observed := chain.StartNodeObserving(t, node, chain.ReleaseBinary(t), 3*time.Minute)
	chain.WriteDiagnostic(t, "v66-unupgraded-halt.log", []byte(observed.Log))

	require.False(t, observed.Running,
		"un-upgraded seid stayed up through the v6.7 plan height\nlog:\n%s", observed.Log)
	require.False(t, observed.Height >= upgradeHeight,
		"un-upgraded seid committed at or past plan height %d (last observed %d)\nlog:\n%s",
		upgradeHeight, observed.Height, observed.Log)
	requireV67UpgradeNeededLog(t, observed.Log, upgradeHeight)

	restore()
}

func snapshotDeliverRetiredStores(t *testing.T, a *processblock.App) map[string]map[string][]byte {
	t.Helper()
	stores := make(map[string]map[string][]byte, len(retiredStoreKeys))
	for _, name := range retiredStoreKeys {
		stores[name] = snapshotDeliverStore(t, a, name)
	}
	return stores
}

func snapshotDeliverStore(t *testing.T, a *processblock.App, storeName string) map[string][]byte {
	t.Helper()
	storeKey := a.GetKey(storeName)
	require.NotNil(t, storeKey, "%s store is not mounted", storeName)
	iterator := a.Ctx().KVStore(storeKey).Iterator(nil, nil)
	defer func() {
		require.NoError(t, iterator.Close())
	}()
	entries := map[string][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		entries[string(iterator.Key())] = append([]byte(nil), iterator.Value()...)
	}
	return entries
}

func requireRetiredStoresMounted(t *testing.T, a *processblock.App) {
	t.Helper()
	cms := a.CommitMultiStore()
	mounted := map[string]struct{}{}
	for _, key := range cms.StoreKeys() {
		mounted[key.Name()] = struct{}{}
	}
	for _, name := range retiredStoreKeys {
		_, ok := mounted[name]
		require.True(t, ok, "%s is not present in the commit multistore", name)
		key := a.GetKey(name)
		require.NotNil(t, key, "%s store is no longer mounted", name)
		require.NotNil(t, cms.GetCommitKVStore(key), "%s is not in the commit multistore", name)
	}
}

func snapshotRetiredStores(t *testing.T, a *processblock.App) map[string]map[string][]byte {
	t.Helper()
	stores := make(map[string]map[string][]byte, len(retiredStoreKeys))
	for _, name := range retiredStoreKeys {
		stores[name] = snapshotCommittedStore(t, a, name)
	}
	return stores
}

func snapshotCommittedStore(t *testing.T, a *processblock.App, storeName string) map[string][]byte {
	t.Helper()
	storeKey := a.GetKey(storeName)
	require.NotNil(t, storeKey, "%s store is not mounted", storeName)
	store := a.CommitMultiStore().GetCommitKVStore(storeKey)
	require.NotNil(t, store, "%s is not in the commit multistore", storeName)
	iterator := store.Iterator(nil, nil)
	defer func() {
		require.NoError(t, iterator.Close())
	}()
	entries := map[string][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		entries[string(iterator.Key())] = append([]byte(nil), iterator.Value()...)
	}
	return entries
}

func requireRetiredStoreInCommitment(t *testing.T, a *processblock.App, storeName string, key, want []byte) {
	t.Helper()
	queryable, ok := a.CommitMultiStore().(sdk.Queryable)
	require.True(t, ok, "commit multistore does not support queries")
	resp := queryable.Query(context.Background(), abci.RequestQuery{
		Path:  "/" + storeName + "/key",
		Data:  key,
		Prove: true,
	})
	require.Equal(t, uint32(0), resp.Code, "query /%s/key: %s", storeName, resp.Log)
	require.Equal(t, want, resp.Value, "query /%s/key returned a different value", storeName)
	require.NotNil(t, resp.ProofOps, "%s is missing from the commitment set", storeName)
	require.NotEmpty(t, resp.ProofOps.Ops, "%s is missing from the commitment set", storeName)
}

func committedModuleVersionExists(t *testing.T, a *processblock.App, module string) bool {
	t.Helper()
	key := a.GetKey(upgradetypes.StoreKey)
	require.NotNil(t, key, "upgrade store is not mounted")
	store := a.CommitMultiStore().GetCommitKVStore(key)
	require.NotNil(t, store, "upgrade store is not in the commit multistore")
	return store.Has(append([]byte{upgradetypes.VersionMapByte}, []byte(module)...))
}

// Deprecating the oracle handlers stopped transactions from reaching oracle
// state, but the module is still in the manager and its mid blocker still runs
// every vote period. With no votes to tally it marks every bonded validator
// absent, so the store keeps being written after the upgrade even though no
// client can put anything in it.
//
// This is characterization, not a defect: it records the write that a later
// oracle removal has to stop before it can drop the store, because a module
// still writing at the height its store is deleted is how an upgrade halts a
// chain. If oracle stops writing, this test should be deleted along with the
// blocker, not adjusted to keep passing.
func TestOracleKeepsWritingStateAfterV67(t *testing.T) {
	a := newV67Chain(t)
	applyV67(t, a)

	validator := a.GetAllValidators()[0]
	operator, err := sdk.ValAddressFromBech32(validator.OperatorAddress)
	require.NoError(t, err)

	require.Zero(t, a.OracleKeeper.GetVotePenaltyCounter(a.Ctx(), operator).AbstainCount)

	// A vote period is two blocks by default, so this spans several of them.
	for i := 0; i < 8; i++ {
		a.RunBlock([]signing.Tx{})
	}

	require.Positive(t, a.OracleKeeper.GetVotePenaltyCounter(a.Ctx(), operator).AbstainCount,
		"oracle no longer records abstentions after v6.7; if its blocker was removed, "+
			"remove this test with it")
	_, err = a.OracleKeeper.GetAggregateExchangeRateVote(a.Ctx(), operator)
	require.ErrorIs(t, err, oracletypes.ErrNoAggregateVote,
		"the abstentions must come from the blocker, not from a vote that got through")
}

// The retired stores survive the upgrade, but no module claims them, so genesis
// export cannot emit them. An opaque write made before the upgrade is still in
// the store after export, and is absent from the exported genesis document.
func TestV67RetainedStateIsAbsentFromExportedGenesis(t *testing.T) {
	a := newV67Chain(t)
	for _, store := range retiredStoreKeys {
		a.Ctx().KVStore(a.GetKey(store)).Set([]byte("seeded"), []byte("pre-upgrade"))
	}

	applyV67(t, a)
	a.RunBlock([]signing.Tx{})

	exported, err := a.ExportAppStateAndValidators(false, nil)
	require.NoError(t, err)

	var genesis map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &genesis))
	for _, store := range retiredStoreKeys {
		require.Equal(t, []byte("pre-upgrade"), a.Ctx().KVStore(a.GetKey(store)).Get([]byte("seeded")),
			"the %s state must still be in the store, or its absence from the export proves nothing", store)
		require.NotContains(t, genesis, store,
			"an exported genesis with a %s section would need a module to import it", store)
	}
}

// retiredStoreKeys are the stores whose modules v6.7 removes while keeping the
// store mounted. Declared here in terms of the sei-db key constants so this
// external test package does not need the unexported names in package app.
var retiredStoreKeys = []string{
	keys.FeegrantStoreKey,
	keys.CapabilityStoreKey,
	keys.IBCStoreKey,
	keys.IBCTransferStoreKey,
}
