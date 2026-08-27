package app_test

import (
	"encoding/json"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	upgradetypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/sei-protocol/sei-chain/testutil/processblock/msgs"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

// v6.7 retires the feegrant, capability, ibc and transfer modules, and
// deprecates the oracle handlers. The tests below drive a chain through the
// shape an operator sees on upgrade day: state already written by a retired
// module, transactions still arriving for it, then the upgrade, then the same
// transactions again.

const (
	v67UpgradeName = "v6.7"
	txFee          = 200000
)

// retiredModuleTx is a transaction aimed at a module surface v6.7 retires,
// paired with the rejection every node must produce for it. The same case runs
// on both sides of the upgrade: retiring a module may not change how its
// transactions are rejected, or nodes mid-upgrade would disagree on a block.
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

// newV67Chain returns a chain whose only registered upgrade is v6.7, so that
// applying it exercises the handler under test rather than an earlier one. The
// common preset supplies the bonded validators a block needs a proposer from.
func newV67Chain(t *testing.T) *processblock.App {
	t.Helper()
	t.Setenv("UPGRADE_VERSION_LIST", v67UpgradeName)
	a := processblock.NewTestApp(t)
	processblock.CommonPreset(a)
	a.RegisterUpgradeHandlers()
	return a
}

func applyV67(t *testing.T, a *processblock.App) {
	t.Helper()
	a.UpgradeKeeper.ApplyUpgrade(a.Ctx(), upgradetypes.Plan{
		Name:   v67UpgradeName,
		Height: a.Ctx().BlockHeight(),
	})
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

// Transactions aimed at retired modules must be rejected identically on both
// sides of the upgrade. A node that has applied v6.7 and one that has not are
// briefly both live during a rollout, and a rejection that changed shape at the
// upgrade height would put them on different application hashes.
func TestV67LeavesRetiredModuleTxRejectionUnchanged(t *testing.T) {
	a := newV67Chain(t)
	beforeAccounts := fundRetiredModuleTxAccounts(a, "before-upgrade")
	afterAccounts := fundRetiredModuleTxAccounts(a, "after-upgrade")

	runRetiredModuleTxs(t, a, "before-upgrade", beforeAccounts)
	applyV67(t, a)
	runRetiredModuleTxs(t, a, "after-upgrade", afterAccounts)
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

// v6.7 drops the retired modules from the version map but deliberately leaves
// their stores mounted, so state written before the upgrade stays on disk.
// Pinning that here means adding a StoreUpgrades{Deleted} entry for one of them
// later cannot pass silently: deleting a store is a defensible choice, but it is
// an application-hash-breaking one that has to be made on purpose.
func TestV67RetainsRetiredModuleStateWrittenBeforeUpgrade(t *testing.T) {
	a := newV67Chain(t)

	// A chain reaching this upgrade carries both each module's version entry and
	// the state it wrote, so seed both. Without the version entry the version
	// map assertions below would hold on a chain that never ran these modules.
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

	for i := 0; i < 3; i++ {
		a.RunBlock([]signing.Tx{})
	}
	for _, store := range retiredStoreKeys {
		require.Contains(t, a.UpgradeKeeper.GetModuleVersionMap(a.Ctx()), store,
			"seeded %s module version did not survive to the upgrade height", store)
		require.Equal(t, seeded[store], a.Ctx().KVStore(a.GetKey(store)).Get([]byte("seeded")),
			"seeded %s state did not survive to the upgrade height", store)
	}

	applyV67(t, a)

	for _, store := range retiredStoreKeys {
		require.NotContains(t, a.UpgradeKeeper.GetModuleVersionMap(a.Ctx()), store,
			"v6.7 must drop %s from the module version map", store)
		require.Equal(t, seeded[store], a.Ctx().KVStore(a.GetKey(store)).Get([]byte("seeded")),
			"v6.7 retains the %s store; deleting it is application-hash breaking", store)
	}

	a.RunBlock([]signing.Tx{})
	for _, store := range retiredStoreKeys {
		require.Equal(t, seeded[store], a.Ctx().KVStore(a.GetKey(store)).Get([]byte("seeded")),
			"retained %s state disappeared once the chain continued past the upgrade", store)
	}
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
// export cannot emit them. A chain restarted from its own exported genesis
// therefore starts with those stores empty while the chain it was exported from
// still carries the state. Retained state is reachable by direct store access
// only, and does not survive an export and import cycle.
func TestV67RetainedStateIsAbsentFromExportedGenesis(t *testing.T) {
	a := newV67Chain(t)
	for _, store := range retiredStoreKeys {
		a.Ctx().KVStore(a.GetKey(store)).Set([]byte("seeded"), []byte("pre-upgrade"))
	}
	a.RunBlock([]signing.Tx{})

	applyV67(t, a)

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
