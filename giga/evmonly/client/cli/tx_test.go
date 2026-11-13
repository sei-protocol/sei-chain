package cli

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	govprecompile "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
)

// wei returns usei in wei.
func wei(usei int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(usei), weiPerUsei)
}

// e18 returns a ratio of 1e18 scaled by num/den.
func e18(num, den int64) *big.Int {
	v := new(big.Int).Mul(big.NewInt(num), big.NewInt(1_000_000_000_000_000_000))
	return v.Quo(v, big.NewInt(den))
}

func TestVoteSendsVote(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	require.NoError(t, r.run("", args("tx gov vote 7 no_with_veto", txArgs(stub)...)...))

	tx := stub.sentTx()
	require.Equal(t, govprecompile.Address, *tx.To())
	require.Equal(t, big.NewInt(testChainID), tx.ChainId())
	sender, err := ethtypes.LatestSignerForChainID(tx.ChainId()).Sender(tx)
	require.NoError(t, err)
	require.Equal(t, testSender, sender)
	require.Equal(t, uint64(testEstimate*defaultGasAdjustment), tx.Gas())
	require.Equal(t, testGasPrice, tx.GasPrice())
	require.Zero(t, tx.Value().Sign())
	sig, callArgs := govCall(t, tx.Data())
	require.Equal(t, "vote(uint64,int32)", sig)
	require.Equal(t, []any{uint64(7), int32(4)}, callArgs)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(r.out.Bytes(), &resp))
	require.Equal(t, tx.Hash().Hex(), resp["txhash"])
	require.Equal(t, "42", resp["height"])
}

func TestGasFlagsOverrideTheNode(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	require.NoError(t, r.run("", args("tx gov vote 1 yes --gas 300000 --gas-fee-cap 7", txArgs(stub)...)...))
	tx := stub.sentTx()
	require.Equal(t, uint64(300_000), tx.Gas())
	require.Equal(t, big.NewInt(7), tx.GasPrice())
}

func TestWeightedVoteScalesWeights(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	require.NoError(t, r.run("", args("tx gov weighted-vote 3 yes=0.6,no=0.4", txArgs(stub)...)...))
	sig, callArgs := govCall(t, stub.sentTx().Data())
	require.Equal(t, voteWeightedSig, sig)
	options := convertTo[[]weightedOption](callArgs[1])
	require.Equal(t, []weightedOption{{Option: 1, Weight: e18(6, 10)}, {Option: 3, Weight: e18(4, 10)}}, options)
}

func TestDepositSendsWei(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	require.NoError(t, r.run("", args("tx gov deposit 3 10usei", txArgs(stub)...)...))
	tx := stub.sentTx()
	require.Equal(t, wei(10), tx.Value())
	sig, callArgs := govCall(t, tx.Data())
	require.Equal(t, "deposit(uint64)", sig)
	require.Equal(t, []any{uint64(3)}, callArgs)

	require.ErrorContains(t, r.run("", args("tx gov deposit 3 10uatom", txArgs(stub)...)...), "only usei")
}

func TestSubmitTextProposalPrintsItsID(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	stub.logs = func(*ethtypes.Transaction) []*ethtypes.Log {
		return []*ethtypes.Log{{
			Address: govprecompile.Address,
			Topics:  []common.Hash{submitProposalEvent.ID, common.BigToHash(big.NewInt(5)), common.BytesToHash(testSender.Bytes())},
			Data:    make([]byte, 64),
		}}
	}
	require.NoError(t, r.run("", args("tx gov submit-proposal --type text --title T --description D --deposit 20usei", txArgs(stub)...)...))
	tx := stub.sentTx()
	require.Equal(t, wei(20), tx.Value())
	sig, callArgs := govCall(t, tx.Data())
	require.Equal(t, "submitTextProposal(string,string,bool)", sig)
	require.Equal(t, []any{"T", "D", false}, callArgs)

	var resp struct {
		Logs []struct {
			Events []struct {
				Type       string `json:"type"`
				Attributes []struct{ Key, Value string }
			} `json:"events"`
		} `json:"logs"`
	}
	require.NoError(t, json.Unmarshal(r.out.Bytes(), &resp))
	require.Equal(t, "submit_proposal", resp.Logs[0].Events[0].Type)
	require.Equal(t, "proposal_id", resp.Logs[0].Events[0].Attributes[0].Key)
	require.Equal(t, "5", resp.Logs[0].Events[0].Attributes[0].Value)
}

func TestSubmitOtherTypeFails(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	err := r.run("", args("tx gov submit-proposal --type CommunityPoolSpend --title T --description D", txArgs(stub)...)...)
	require.ErrorContains(t, err, `proposal type "CommunityPoolSpend" is not supported on an EVM-only chain; use Text or a submit-proposal subcommand, or --node`)
	require.Zero(t, stub.requests)
}

func TestTransactionAsksBeforeBroadcasting(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	require.ErrorContains(t, r.run("n\n", args("tx gov deposit 3 10usei", askingTxArgs(stub)...)...), "cancelled transaction")
	shown := r.errOut.String()
	require.Contains(t, shown, `"to": "`+govprecompile.Address.Hex()+`"`)
	require.Contains(t, shown, `"from": "`+strings.ToLower(testSender.Hex())+`"`)
	// The prompt shows the transaction before it is signed.
	for _, sigField := range []string{`"v"`, `"r"`, `"s"`, `"hash"`} {
		require.NotContains(t, shown, sigField)
	}
	require.Empty(t, stub.sent)

	require.NoError(t, r.run("y\n", args("tx gov deposit 3 10usei", askingTxArgs(stub)...)...))
	require.Len(t, stub.sent, 1)
}

func TestFeeFlagsAreRefused(t *testing.T) {
	for _, flag := range []string{"--fees=10usei", "--gas-prices=0.1usei"} {
		r := newTestRoot(t)
		stub := newRPCStub(t)
		require.ErrorContains(t, r.run("", args("tx gov vote 1 yes "+flag, txArgs(stub)...)...), "use --gas-fee-cap to set the gas price in wei, or --node")
		require.Zero(t, stub.requests)
	}
}

func TestFailedTransactionFails(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	stub.failed = true
	err := r.run("", args("tx gov vote 1 yes", txArgs(stub)...)...)
	require.ErrorContains(t, err, "failed")
	var resp map[string]any
	require.NoError(t, json.Unmarshal(r.out.Bytes(), &resp))
	require.EqualValues(t, 1, resp["code"])
}

func TestSubmitSoftwareUpgrade(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	require.NoError(t, r.run("", args("tx gov submit-proposal software-upgrade v2.0 --upgrade-height 100 --upgrade-info info --title T --description D --is-expedited --deposit 5usei", txArgs(stub)...)...))
	sig, callArgs := govCall(t, stub.sentTx().Data())
	require.Equal(t, "submitSoftwareUpgradeProposal(string,string,bool,(string,uint64,string))", sig)
	require.Equal(t, "T", callArgs[0])
	require.Equal(t, true, callArgs[2])
	require.Equal(t, upgradePlan{Name: "v2.0", Height: 100, Info: "info"}, convertTo[upgradePlan](callArgs[3]))
	require.Empty(t, r.errOut.String())
}

func TestLongPlanNameWarns(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	name := "v6.7.0-a-name-longer-than-one-slot"
	require.NoError(t, r.run("", args("tx gov submit-proposal software-upgrade "+name+" --upgrade-height 100 --title T --description D", txArgs(stub)...)...))
	require.Contains(t, r.errOut.String(), "warning: plan name")
}

func TestCancelSoftwareUpgrade(t *testing.T) {
	scheduled := func(stub *rpcStub) {
		stub.on("currentPlan()", func([]any) ([]any, error) {
			return []any{upgradePlan{Name: "v2.0", Height: 100}}, nil
		})
	}
	cancelArgs := func(stub *rpcStub, extra ...string) []string {
		return args("tx gov submit-proposal cancel-software-upgrade --title T --description D", append(askingTxArgs(stub), extra...)...)
	}
	cancelled := func(t *testing.T, stub *rpcStub) string {
		sig, callArgs := govCall(t, stub.sentTx().Data())
		require.Equal(t, "submitCancelSoftwareUpgradeProposal(string,string,bool,string)", sig)
		return callArgs[3].(string)
	}

	t.Run("name flag", func(t *testing.T) {
		r, stub := newTestRoot(t), newRPCStub(t)
		require.NoError(t, r.run("", cancelArgs(stub, "--name", "v3.0", "--yes")...))
		require.Equal(t, "v3.0", cancelled(t, stub))
	})
	t.Run("scheduled plan with yes", func(t *testing.T) {
		r, stub := newTestRoot(t), newRPCStub(t)
		scheduled(stub)
		require.NoError(t, r.run("", cancelArgs(stub, "--yes")...))
		require.Equal(t, "v2.0", cancelled(t, stub))
	})
	t.Run("scheduled plan confirmed", func(t *testing.T) {
		r, stub := newTestRoot(t), newRPCStub(t)
		scheduled(stub)
		require.NoError(t, r.run("y\n", cancelArgs(stub)...))
		require.Contains(t, r.errOut.String(), `cancels the upgrade "v2.0" scheduled at height 100`)
		require.Equal(t, "v2.0", cancelled(t, stub))
	})
	t.Run("scheduled plan declined", func(t *testing.T) {
		r, stub := newTestRoot(t), newRPCStub(t)
		scheduled(stub)
		require.ErrorContains(t, r.run("n\n", cancelArgs(stub)...), "cancelled transaction")
		require.Empty(t, stub.sent)
	})
	t.Run("nothing scheduled", func(t *testing.T) {
		r, stub := newTestRoot(t), newRPCStub(t)
		stub.on("currentPlan()", func([]any) ([]any, error) { return []any{upgradePlan{}}, nil })
		require.ErrorContains(t, r.run("", cancelArgs(stub, "--yes")...), "no upgrade scheduled")
		require.Empty(t, stub.sent)
	})
}

func TestSubmitParamChange(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	file := filepath.Join(t.TempDir(), "proposal.json")
	require.NoError(t, os.WriteFile(file, []byte(`{
  "title": "Gov Param Change",
  "description": "Update quorum",
  "changes": [
    {"subspace": "gov", "key": "tallyparams", "value": {"quorum": "0.45", "threshold": 0.5}},
    {"subspace": "gov", "key": "votingparams", "value": {"voting_period": "10m"}},
    {"subspace": "gov", "key": "depositparams.min_deposit", "value": [{"denom": "usei", "amount": "10"}]}
  ],
  "deposit": "10000000usei",
  "is_expedited": true
}`), 0o600))
	require.NoError(t, r.run("", args("tx gov submit-proposal param-change "+file, txArgs(stub)...)...))
	tx := stub.sentTx()
	require.Equal(t, wei(10_000_000), tx.Value())
	sig, callArgs := govCall(t, tx.Data())
	require.Equal(t, "submitParamChangeProposal(string,string,bool,(string,string,bytes)[])", sig)
	require.Equal(t, true, callArgs[2])
	changes := convertTo[[]paramChange](callArgs[3])
	word := func(v *big.Int) []byte { return common.BigToHash(v).Bytes() }
	require.Equal(t, []paramChange{
		{Subspace: "gov", Key: "tallyparams.quorum", Value: word(e18(45, 100))},
		{Subspace: "gov", Key: "tallyparams.threshold", Value: word(e18(1, 2))},
		{Subspace: "gov", Key: "votingparams.voting_period", Value: word(big.NewInt(600_000_000_000))},
		{Subspace: "gov", Key: "depositparams.min_deposit", Value: word(wei(10))},
	}, changes)
}

func TestSubmitParamChangeRejectsUndeclaredParams(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	file := filepath.Join(t.TempDir(), "proposal.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"title":"T","description":"D","changes":[
    {"subspace": "baseapp", "key": "ABCIParams", "value": {"recheck_tx": true}}]}`), 0o600))
	err := r.run("", args("tx gov submit-proposal param-change "+file, txArgs(stub)...)...)
	require.ErrorContains(t, err, "baseapp/ABCIParams is not a param of this chain")
	require.ErrorContains(t, err, "gov/tallyparams.quorum")
	require.Zero(t, stub.requests)
}

func TestOfflineModesAreRefused(t *testing.T) {
	for _, flag := range []string{"--generate-only", "--dry-run", "--offline"} {
		r := newTestRoot(t)
		stub := newRPCStub(t)
		require.ErrorContains(t, r.run("", args("tx gov vote 1 yes "+flag, txArgs(stub)...)...), flag+" is not supported on an EVM-only chain; use --node")
		require.Zero(t, stub.requests)
	}
}
