package gov_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha1" //nolint:gosec // test names, not security
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
)

const (
	chainID      = 713715
	gasPrice     = 1_000_000_000
	votingPeriod = 100
	startTime    = 1_000
	callGasLimit = 30_000_000
)

var fundedBalance = new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e6))

type account struct {
	key  *ecdsa.PrivateKey
	addr common.Address
}

func newAccount(t testing.TB, seed byte) account {
	t.Helper()
	key, err := crypto.ToECDSA(common.LeftPadBytes([]byte{0x7f, seed}, 32))
	require.NoError(t, err)
	return account{key: key, addr: crypto.PubkeyToAddress(key.PublicKey)}
}

// fixture is a set of accounts and the genesis they vote under. Voter i has weight weights[i].
type fixture struct {
	voters   []account
	outsider account
	genesis  gov.Genesis
	upgrades gov.Upgrades
	proxies  map[byte]common.Address
}

func newFixture(t testing.TB, weights ...uint64) fixture {
	t.Helper()
	f := fixture{
		outsider: newAccount(t, 0xff),
		genesis:  gov.Genesis{Params: gov.DefaultParams()},
		proxies:  map[byte]common.Address{},
	}
	f.genesis.Params.VotingPeriod = votingPeriod
	for i, weight := range weights {
		voter := newAccount(t, byte(i+1))
		f.voters = append(f.voters, voter)
		f.genesis.Voters = append(f.genesis.Voters, gov.Voter{Address: voter.addr, Weight: weight})
	}
	for i, op := range []byte{opCall, opStaticCall, opDelegateCall} {
		f.proxies[op] = common.BigToAddress(big.NewInt(int64(0xc000 + i)))
	}
	return f
}

// chain is one executor over its own state, advancing block by block.
type chain struct {
	t        testing.TB
	executor *evmonly.Executor
	store    *evmonly.MemoryStore
	workers  int
	nonces   map[common.Address]uint64
	number   uint64
	time     uint64
	results  []*evmonly.BlockResult
}

func (f fixture) newChain(t testing.TB, workers int) *chain {
	t.Helper()
	contract, err := gov.New(f.genesis, f.upgrades)
	require.NoError(t, err)
	state := evmonly.NewMemoryState()
	for _, voter := range f.voters {
		state.SetBalance(voter.addr, fundedBalance)
	}
	state.SetBalance(f.outsider.addr, fundedBalance)
	for op, addr := range f.proxies {
		state.SetCode(addr, forwarder(op, gov.Address))
	}
	store := evmonly.NewMemoryStore(state)
	c := &chain{t: t, store: store, workers: workers, nonces: map[common.Address]uint64{}, time: startTime}
	c.executor = c.newExecutor(contract)
	t.Cleanup(func() {
		for _, result := range c.results {
			result.Release()
		}
		c.executor.Close()
	})
	return c
}

func (c *chain) newExecutor(contract *gov.Contract) *evmonly.Executor {
	return evmonly.NewExecutor(
		evmonly.Config{CustomPrecompiles: contract.Registry(), OCCWorkers: c.workers},
		evmonly.WithStore(c.store, c.store.EncodeChangeSet),
		evmonly.WithReceiptStore(evmonly.NewMemoryReceiptStore()),
	)
}

// swapBinary restarts c's executor over the same state with a governance
// precompile that plays upgrades' part, as a node restarted on another binary.
func (c *chain) swapBinary(f fixture, upgrades gov.Upgrades) {
	c.t.Helper()
	contract, err := gov.New(f.genesis, upgrades)
	require.NoError(c.t, err)
	c.executor.Close()
	c.executor = c.newExecutor(contract)
}

func (c *chain) blockContext() evmonly.BlockContext {
	return evmonly.BlockContext{
		Number:      c.number,
		Time:        c.time,
		GasLimit:    100_000_000,
		ChainID:     big.NewInt(chainID),
		BaseFee:     big.NewInt(0),
		BlobBaseFee: big.NewInt(0),
	}
}

// tx is a signed transaction from an account to an address.
type tx struct {
	from  account
	to    common.Address
	data  []byte
	value *big.Int
	gas   uint64
}

func (c *chain) sign(t tx) []byte {
	c.t.Helper()
	nonce := c.nonces[t.from.addr]
	c.nonces[t.from.addr] = nonce + 1
	value := t.value
	if value == nil {
		value = new(big.Int)
	}
	to := t.to
	gas := t.gas
	if gas == 0 {
		gas = 5_000_000
	}
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    value,
		Data:     t.data,
	}), ethtypes.LatestSignerForChainID(big.NewInt(chainID)), t.from.key)
	require.NoError(c.t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(c.t, err)
	return raw
}

// block executes txs in the next block, dt seconds after the last one.
func (c *chain) block(dt uint64, txs ...tx) *evmonly.BlockResult {
	c.t.Helper()
	result, err := c.tryBlock(dt, txs...)
	require.NoError(c.t, err)
	return result
}

// tryBlock executes txs in the next block, dt seconds after the last one, and
// returns the block's error. A failed block leaves the chain at its parent.
func (c *chain) tryBlock(dt uint64, txs ...tx) (*evmonly.BlockResult, error) {
	c.t.Helper()
	nonces := maps.Clone(c.nonces)
	c.number++
	c.time += dt
	raw := make([][]byte, len(txs))
	for i, t := range txs {
		raw[i] = c.sign(t)
	}
	result, err := c.executor.ExecuteBlock(c.t.Context(), evmonly.BlockRequest{Context: c.blockContext(), Txs: raw})
	if err != nil {
		c.nonces = nonces
		c.number--
		c.time -= dt
		return nil, err
	}
	c.results = append(c.results, result)
	return result, nil
}

// call runs data against the committed state as an eth_call from from.
func (c *chain) call(from, to common.Address, data []byte) *core.ExecutionResult {
	c.t.Helper()
	result, err := c.executor.Call(c.t.Context(), c.blockContext(), &core.Message{
		From:             from,
		To:               &to,
		GasLimit:         callGasLimit,
		GasPrice:         new(big.Int),
		GasFeeCap:        new(big.Int),
		GasTipCap:        new(big.Int),
		Value:            new(big.Int),
		Data:             data,
		SkipNonceChecks:  true,
		SkipFromEOACheck: true,
	})
	require.NoError(c.t, err)
	return result
}

// query calls a view method and unpacks its single result into out.
func (c *chain) query(out any, method string, args ...any) {
	c.t.Helper()
	result := c.call(common.Address{}, gov.Address, pack(c.t, method, args...))
	require.NoError(c.t, result.Err, "%s reverted: %s", method, revertReason(result.Revert()))
	values, err := gov.ABI.Methods[method].Outputs.Unpack(result.ReturnData)
	require.NoError(c.t, err)
	require.Len(c.t, values, 1)
	require.NoError(c.t, jsonRoundTrip(values[0], out))
}

func jsonRoundTrip(in, out any) error {
	bz, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(bz, out)
}

// queryErr calls method and returns its revert reason.
func (c *chain) queryErr(from common.Address, method string, args ...any) string {
	c.t.Helper()
	result := c.call(from, gov.Address, pack(c.t, method, args...))
	require.Error(c.t, result.Err, "%s did not revert", method)
	return revertReason(result.Revert())
}

func (c *chain) plan() (gov.Plan, bool) {
	view := c.store.OpenView()
	defer view.Close()
	return gov.ReadPlan(viewReader{view})
}

type viewReader struct {
	view interface {
		GetStorage(common.Address, common.Hash) common.Hash
	}
}

func (r viewReader) GetState(addr common.Address, key common.Hash) common.Hash {
	return r.view.GetStorage(addr, key)
}

func pack(t testing.TB, method string, args ...any) []byte {
	t.Helper()
	data, err := gov.ABI.Pack(method, args...)
	require.NoError(t, err)
	return data
}

func revertReason(data []byte) string {
	reason, err := abi.UnpackRevert(data)
	if err != nil {
		return fmt.Sprintf("<undecodable revert %x>", data)
	}
	return reason
}

// upgradeJSON is a software-upgrade proposal to commitHash(label) at height.
func upgradeJSON(label string, height int64) string {
	return fmt.Sprintf(`{"title":"upgrade","description":"move to %[1]s","type":"SoftwareUpgrade","plan":{"name":%[2]q,"height":%[3]d,"info":"info for %[1]s"}}`, label, commitHash(label), height)
}

// commitHash returns label if it is already a well-formed upgrade name, and
// otherwise a commit hash derived from it.
func commitHash(label string) string {
	if len(label) == gov.PlanNameLength && strings.Trim(label, "0123456789abcdef") == "" {
		return label
	}
	sum := sha1.Sum([]byte(label)) //nolint:gosec // test names, not security
	return hex.EncodeToString(sum[:])
}

func cancelJSON() string {
	return `{"title":"cancel","description":"cancel the upgrade","type":"CancelSoftwareUpgrade"}`
}

func (f fixture) submit(from account, proposal string) tx {
	return tx{from: from, to: gov.Address, data: must(gov.ABI.Pack("submitProposal", proposal))}
}

func (f fixture) vote(from account, id uint64, option int32) tx {
	return tx{from: from, to: gov.Address, data: must(gov.ABI.Pack("vote", id, option))}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func requireStatuses(t testing.TB, result *evmonly.BlockResult, want ...uint64) {
	t.Helper()
	got := make([]uint64, len(result.Txs))
	for i, tx := range result.Txs {
		got[i] = tx.Status
	}
	require.Equal(t, want, got)
}

// Mirrors of the ABI tuples, decoded through JSON.
type proposalData struct {
	Id               uint64 //nolint:revive // ABI field name.
	Status           int32
	FinalTallyResult tallyData
	SubmitTime       int64
	DepositEndTime   int64
	TotalDeposit     []struct {
		Amount *big.Int
		Denom  string
	}
	VotingStartTime int64
	VotingEndTime   int64
	IsExpedited     bool
	Content         []byte
}

type tallyData struct{ Yes, Abstain, No, NoWithVeto string }

type voteData struct {
	ProposalId uint64 //nolint:revive // ABI field name.
	Voter      string
	Options    []struct {
		Option int32
		Weight string
	}
}

type paramsData struct {
	VotingPeriod          uint64
	ExpeditedVotingPeriod uint64
	MaxDepositPeriod      uint64
	Quorum                string
	Threshold             string
	VetoThreshold         string
	ExpeditedQuorum       string
	ExpeditedThreshold    string
}

const (
	opCall         byte = 0xf1
	opDelegateCall byte = 0xf4
	opStaticCall   byte = 0xfa
)

// forwarder is runtime code that passes its calldata to target with op and
// returns or reverts with target's return data.
func forwarder(op byte, target common.Address) []byte {
	code := []byte{0x36, 0x5f, 0x5f, 0x37} // CALLDATACOPY(0, 0, CALLDATASIZE)
	code = append(code, 0x5f, 0x5f, 0x36, 0x5f)
	if op == opCall {
		code = append(code, 0x5f) // value
	}
	code = append(code, 0x73)
	code = append(code, target[:]...)
	code = append(code, 0x5a, op)
	code = append(code, 0x3d, 0x5f, 0x5f, 0x3e) // RETURNDATACOPY(0, 0, RETURNDATASIZE)
	dest := byte(len(code) + 6)
	code = append(code, 0x60, dest, 0x57) // JUMPI(dest, success)
	code = append(code, 0x3d, 0x5f, 0xfd) // REVERT(0, RETURNDATASIZE)
	code = append(code, 0x5b, 0x3d, 0x5f, 0xf3)
	return code
}

var workerCounts = []int{1, 8}

func forEachExecutor(t *testing.T, run func(t *testing.T, workers int)) {
	for _, workers := range workerCounts {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) { run(t, workers) })
	}
}

func TestUpgradeProposalLifecycle(t *testing.T) {
	forEachExecutor(t, func(t *testing.T, workers int) {
		f := newFixture(t, 10, 20, 30, 40)
		c := f.newChain(t, workers)
		name := strings.Repeat("ab", 20)

		var nextID uint64
		result := c.call(f.voters[0].addr, gov.Address, pack(t, "submitProposal", upgradeJSON(name, 50)))
		require.NoError(t, result.Err)
		require.NoError(t, gov.ABI.UnpackIntoInterface(&nextID, "submitProposal", result.ReturnData))
		require.Equal(t, uint64(1), nextID)

		requireStatuses(t, c.block(1, f.submit(f.voters[0], upgradeJSON(name, 50))), 1)
		submitTime := c.time
		var p proposalData
		c.query(&p, "proposal", uint64(1))
		require.Equal(t, uint64(1), p.Id)
		require.Equal(t, gov.StatusVotingPeriod, p.Status)
		require.Equal(t, int64(submitTime), p.SubmitTime)                 //nolint:gosec // test time.
		require.Equal(t, int64(submitTime), p.VotingStartTime)            //nolint:gosec // test time.
		require.Equal(t, int64(submitTime+votingPeriod), p.VotingEndTime) //nolint:gosec // test time.
		require.Empty(t, p.TotalDeposit)
		require.False(t, p.IsExpedited)
		require.Equal(t, tallyData{"0", "0", "0", "0"}, p.FinalTallyResult)
		require.JSONEq(t, fmt.Sprintf(`{
			"@type": "/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal",
			"title": "upgrade",
			"description": "move to %[1]s",
			"plan": {"name": %[1]q, "height": "50", "info": "info for %[1]s"}
		}`, name), string(p.Content))

		votes := c.block(1,
			f.vote(f.voters[0], 1, gov.OptionYes),
			f.vote(f.voters[1], 1, gov.OptionNo),
			f.vote(f.voters[2], 1, gov.OptionYes),
			f.vote(f.voters[3], 1, gov.OptionAbstain),
		)
		requireStatuses(t, votes, 1, 1, 1, 1)
		require.Zero(t, votes.OCCStats.ConflictCount, "each vote writes only its own slot")

		var live tallyData
		c.query(&live, "tallyResult", uint64(1))
		require.Equal(t, tallyData{Yes: "40", Abstain: "40", No: "20", NoWithVeto: "0"}, live)
		var v voteData
		c.query(&v, "getVote", uint64(1), f.voters[1].addr)
		require.Equal(t, uint64(1), v.ProposalId)
		require.Equal(t, f.voters[1].addr.Hex(), v.Voter)
		require.Len(t, v.Options, 1)
		require.Equal(t, gov.OptionNo, v.Options[0].Option)
		require.Equal(t, "1.000000000000000000", v.Options[0].Weight)
		require.Equal(t, gov.ErrVoteNotFound.Error(), c.queryErr(common.Address{}, "getVote", uint64(1), f.outsider.addr))

		// One second before the voting period ends, the proposal is still open and takes votes.
		requireStatuses(t, c.block(votingPeriod-2, f.vote(f.voters[1], 1, gov.OptionYes)), 1)
		c.query(&p, "proposal", uint64(1))
		require.Equal(t, gov.StatusVotingPeriod, p.Status)
		_, ok := c.plan()
		require.False(t, ok)

		// The first block at the end time refuses its votes and tallies the proposal.
		requireStatuses(t, c.block(1, f.vote(f.voters[0], 1, gov.OptionNo)), 0)
		c.query(&p, "proposal", uint64(1))
		require.Equal(t, gov.StatusPassed, p.Status)
		require.Equal(t, tallyData{Yes: "60", Abstain: "40", No: "0", NoWithVeto: "0"}, p.FinalTallyResult)
		var final tallyData
		c.query(&final, "tallyResult", uint64(1))
		require.Equal(t, p.FinalTallyResult, final)
		plan, ok := c.plan()
		require.True(t, ok)
		require.Equal(t, gov.Plan{Name: name, Height: 50, Info: "info for " + name, Proposal: 1}, plan)

		// The ended proposal takes no more votes, and the plan persists.
		require.Equal(t, gov.ErrProposalNotInVoting.Error(), c.queryErr(f.voters[0].addr, "vote", uint64(1), gov.OptionNo))
		requireStatuses(t, c.block(1, f.vote(f.voters[0], 1, gov.OptionNo)), 0)
		for range 5 {
			c.block(votingPeriod)
		}
		c.query(&final, "tallyResult", uint64(1))
		require.Equal(t, p.FinalTallyResult, final)
		after, ok := c.plan()
		require.True(t, ok)
		require.Equal(t, plan, after)
	})
}

func TestTallyOutcomes(t *testing.T) {
	// Weights 10, 20, 30, 40: total 100, quorum 33.4, veto 33.4% of votes cast, threshold 50% of non-abstain.
	for _, tc := range []struct {
		name  string
		votes map[int]int32
		want  int32
	}{
		{name: "no votes", votes: map[int]int32{}, want: gov.StatusRejected},
		{name: "below quorum", votes: map[int]int32{2: gov.OptionYes}, want: gov.StatusRejected},
		{name: "quorum by abstain alone", votes: map[int]int32{0: gov.OptionAbstain, 3: gov.OptionAbstain}, want: gov.StatusRejected},
		{name: "yes majority", votes: map[int]int32{3: gov.OptionYes}, want: gov.StatusPassed},
		{name: "tie fails", votes: map[int]int32{0: gov.OptionYes, 2: gov.OptionYes, 3: gov.OptionNo}, want: gov.StatusRejected},
		{name: "abstain excluded from threshold", votes: map[int]int32{0: gov.OptionYes, 3: gov.OptionAbstain}, want: gov.StatusPassed},
		{name: "vetoed", votes: map[int]int32{3: gov.OptionYes, 2: gov.OptionNoWithVeto}, want: gov.StatusRejected},
		{name: "veto below threshold", votes: map[int]int32{3: gov.OptionYes, 2: gov.OptionYes, 0: gov.OptionNoWithVeto}, want: gov.StatusPassed},
		{name: "no majority", votes: map[int]int32{1: gov.OptionYes, 2: gov.OptionNo}, want: gov.StatusRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 10, 20, 30, 40)
			c := f.newChain(t, 1)
			requireStatuses(t, c.block(1, f.submit(f.voters[0], upgradeJSON("target", 10_000))), 1)
			var votes []tx
			for voter, option := range tc.votes {
				votes = append(votes, f.vote(f.voters[voter], 1, option))
			}
			c.block(1, votes...)
			c.block(votingPeriod)
			var p proposalData
			c.query(&p, "proposal", uint64(1))
			require.Equal(t, tc.want, p.Status)
			_, ok := c.plan()
			require.Equal(t, tc.want == gov.StatusPassed, ok)
		})
	}
}

func TestPassingUpgradeAtOrBelowTheEndingBlockFails(t *testing.T) {
	f := newFixture(t, 1)
	c := f.newChain(t, 1)
	// The proposal ends in block 3, so a plan for height 3 is already in the past.
	requireStatuses(t, c.block(1,
		f.submit(f.voters[0], upgradeJSON("past", 3)),
		f.submit(f.voters[0], upgradeJSON("next", 4)),
		f.vote(f.voters[0], 1, gov.OptionYes),
		f.vote(f.voters[0], 2, gov.OptionYes),
	), 1, 1, 1, 1)
	c.block(1)
	c.block(votingPeriod)
	require.Equal(t, uint64(3), c.number)
	var p proposalData
	c.query(&p, "proposal", uint64(1))
	require.Equal(t, gov.StatusFailed, p.Status)
	require.Equal(t, tallyData{Yes: "1", Abstain: "0", No: "0", NoWithVeto: "0"}, p.FinalTallyResult)
	c.query(&p, "proposal", uint64(2))
	require.Equal(t, gov.StatusPassed, p.Status)
	plan, ok := c.plan()
	require.True(t, ok)
	require.Equal(t, commitHash("next"), plan.Name)
	require.Equal(t, uint64(4), plan.Height)
}

func TestLaterUpgradeReplacesThePlanAndCancelClearsIt(t *testing.T) {
	f := newFixture(t, 1)
	c := f.newChain(t, 1)
	pass := func(proposal string, id uint64) {
		t.Helper()
		requireStatuses(t, c.block(1, f.submit(f.voters[0], proposal), f.vote(f.voters[0], id, gov.OptionYes)), 1, 1)
		c.block(votingPeriod)
		var p proposalData
		c.query(&p, "proposal", id)
		require.Equal(t, gov.StatusPassed, p.Status)
	}

	// Cancelling with no plan scheduled passes and changes nothing.
	pass(cancelJSON(), 1)
	_, ok := c.plan()
	require.False(t, ok)

	pass(upgradeJSON("a", 1_000), 2)
	plan, ok := c.plan()
	require.True(t, ok)
	require.Equal(t, uint64(2), plan.Proposal)

	pass(upgradeJSON("b", 2_000), 3)
	plan, ok = c.plan()
	require.True(t, ok)
	require.Equal(t, gov.Plan{Name: commitHash("b"), Height: 2_000, Info: "info for b", Proposal: 3}, plan)

	var p proposalData
	c.query(&p, "proposal", uint64(1))
	require.JSONEq(t, `{"@type":"/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal","title":"cancel","description":"cancel the upgrade"}`, string(p.Content))

	pass(cancelJSON(), 4)
	_, ok = c.plan()
	require.False(t, ok)
}

func TestRevotingReplacesTheVote(t *testing.T) {
	f := newFixture(t, 1, 1)
	c := f.newChain(t, 1)
	requireStatuses(t, c.block(1,
		f.submit(f.voters[0], upgradeJSON("x", 1_000)),
		f.vote(f.voters[0], 1, gov.OptionYes),
		f.vote(f.voters[1], 1, gov.OptionYes),
	), 1, 1, 1)
	requireStatuses(t, c.block(1, f.vote(f.voters[1], 1, gov.OptionNoWithVeto)), 1)
	var v voteData
	c.query(&v, "getVote", uint64(1), f.voters[1].addr)
	require.Equal(t, gov.OptionNoWithVeto, v.Options[0].Option)
	c.block(votingPeriod)
	var p proposalData
	c.query(&p, "proposal", uint64(1))
	require.Equal(t, gov.StatusRejected, p.Status)
	require.Equal(t, tallyData{Yes: "1", Abstain: "0", No: "0", NoWithVeto: "1"}, p.FinalTallyResult)
}

func TestRejectedCallsRevertWithoutChangingState(t *testing.T) {
	f := newFixture(t, 1, 2)
	longString := func(n int) string { return strings.Repeat("s", n) }
	withField := func(field, value string) string {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(upgradeJSON("n", 10)), &m))
		plan := m["plan"].(map[string]any)
		switch field {
		case "title", "description", "type":
			m[field] = value
		default:
			plan[field] = value
		}
		return string(must(json.Marshal(m)))
	}
	for _, tc := range []struct {
		name   string
		voter  bool
		data   func(t testing.TB) []byte
		reason string
	}{
		{name: "outsider submits", data: func(t testing.TB) []byte { return pack(t, "submitProposal", upgradeJSON("n", 10)) }, reason: gov.ErrNotVoter.Error()},
		{name: "outsider votes", data: func(t testing.TB) []byte { return pack(t, "vote", uint64(1), gov.OptionYes) }, reason: gov.ErrNotVoter.Error()},
		{name: "malformed json", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", "{") }, reason: "invalid proposal: unexpected end of JSON input"},
		{name: "unknown type", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", withField("type", "Text")) }, reason: `unsupported proposal type: "Text"`},
		{name: "expedited", voter: true, data: func(t testing.TB) []byte {
			return pack(t, "submitProposal", `{"title":"t","description":"d","type":"SoftwareUpgrade","is_expedited":true,"plan":{"name":"n","height":10}}`)
		}, reason: "unsupported proposal type: expedited proposals"},
		{name: "empty title", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", withField("title", "")) }, reason: "invalid proposal: title must be 1 to 140 bytes"},
		{name: "long title", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", withField("title", longString(141))) }, reason: "invalid proposal: title must be 1 to 140 bytes"},
		{name: "empty description", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", withField("description", "")) }, reason: "invalid proposal: description must be 1 to 10000 bytes"},
		{name: "long description", voter: true, data: func(t testing.TB) []byte {
			return pack(t, "submitProposal", withField("description", longString(10_001)))
		}, reason: "invalid proposal: description must be 1 to 10000 bytes"},
		{name: "missing plan", voter: true, data: func(t testing.TB) []byte {
			return pack(t, "submitProposal", `{"title":"t","description":"d","type":"SoftwareUpgrade"}`)
		}, reason: "invalid proposal: upgrade plan must be specified"},
		{name: "zero height", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", upgradeJSON("n", 0)) }, reason: "invalid proposal: upgrade height must be positive"},
		{name: "negative height", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", upgradeJSON("n", -1)) }, reason: "invalid proposal: upgrade height must be positive"},
		{name: "empty name", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", withField("name", "")) }, reason: "invalid proposal: upgrade name must be a 40-character lowercase hex commit hash"},
		{name: "short hash", voter: true, data: func(t testing.TB) []byte {
			return pack(t, "submitProposal", withField("name", strings.Repeat("a", 39)))
		}, reason: "invalid proposal: upgrade name must be a 40-character lowercase hex commit hash"},
		{name: "long hash", voter: true, data: func(t testing.TB) []byte {
			return pack(t, "submitProposal", withField("name", strings.Repeat("a", 41)))
		}, reason: "invalid proposal: upgrade name must be a 40-character lowercase hex commit hash"},
		{name: "uppercase hash", voter: true, data: func(t testing.TB) []byte {
			return pack(t, "submitProposal", withField("name", strings.Repeat("A", 40)))
		}, reason: "invalid proposal: upgrade name must be a 40-character lowercase hex commit hash"},
		{name: "non-hex name", voter: true, data: func(t testing.TB) []byte {
			return pack(t, "submitProposal", withField("name", strings.Repeat("g", 40)))
		}, reason: "invalid proposal: upgrade name must be a 40-character lowercase hex commit hash"},
		{name: "tag name", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", withField("name", "v6.7.0")) }, reason: "invalid proposal: upgrade name must be a 40-character lowercase hex commit hash"},
		{name: "long info", voter: true, data: func(t testing.TB) []byte { return pack(t, "submitProposal", withField("info", longString(10_001))) }, reason: "invalid proposal: upgrade info longer than 10000 bytes"},
		{name: "oversized json", voter: true, data: func(t testing.TB) []byte {
			return pack(t, "submitProposal", upgradeJSON("n", 10)+strings.Repeat(" ", 32*1024))
		}, reason: "invalid proposal: longer than 32768 bytes"},
		{name: "unknown proposal", voter: true, data: func(t testing.TB) []byte { return pack(t, "vote", uint64(2), gov.OptionYes) }, reason: gov.ErrProposalNotFound.Error()},
		{name: "proposal zero", voter: true, data: func(t testing.TB) []byte { return pack(t, "vote", uint64(0), gov.OptionYes) }, reason: gov.ErrProposalNotFound.Error()},
		{name: "empty option", voter: true, data: func(t testing.TB) []byte { return pack(t, "vote", uint64(1), int32(0)) }, reason: gov.ErrInvalidVoteOption.Error()},
		{name: "unknown option", voter: true, data: func(t testing.TB) []byte { return pack(t, "vote", uint64(1), int32(5)) }, reason: gov.ErrInvalidVoteOption.Error()},
		{name: "negative option", voter: true, data: func(t testing.TB) []byte { return pack(t, "vote", uint64(1), int32(-1)) }, reason: gov.ErrInvalidVoteOption.Error()},
		{name: "unknown selector", voter: true, data: func(testing.TB) []byte { return []byte{1, 2, 3, 4} }, reason: gov.ErrUnknownMethod.Error()},
		{name: "short input", voter: true, data: func(testing.TB) []byte { return []byte{1} }, reason: gov.ErrUnknownMethod.Error()},
		{name: "truncated arguments", voter: true, data: func(t testing.TB) []byte { return pack(t, "vote", uint64(1), gov.OptionYes)[:20] }, reason: "invalid arguments: abi: cannot marshal in to go type: length insufficient 16 require 32"},
		{name: "missing argument", voter: true, data: func(t testing.TB) []byte { return pack(t, "vote", uint64(1), gov.OptionYes)[:4+32] }, reason: "invalid arguments: abi: cannot marshal in to go type: length insufficient 32 require 64"},
		{name: "no arguments", voter: true, data: func(t testing.TB) []byte { return pack(t, "vote", uint64(1), gov.OptionYes)[:4] }, reason: "invalid arguments: abi: attempting to unmarshal an empty string while arguments are expected"},
		{name: "trailing bytes", voter: true, data: func(t testing.TB) []byte {
			return append(pack(t, "vote", uint64(1), gov.OptionYes), make([]byte, 32)...)
		}, reason: "invalid arguments: not the canonical encoding"},
		{name: "uint64 above range", voter: true, data: func(t testing.TB) []byte {
			data := pack(t, "vote", uint64(1), gov.OptionYes)
			data[4+23] = 1
			return data
		}, reason: "invalid arguments: abi: improperly encoded uint64 value"},
		{name: "int32 above range", voter: true, data: func(t testing.TB) []byte {
			data := pack(t, "vote", uint64(1), gov.OptionYes)
			data[4+32+27] = 1
			return data
		}, reason: "invalid arguments: abi: improperly encoded int32 value"},
		{name: "dirty address padding", voter: true, data: func(t testing.TB) []byte {
			data := pack(t, "getVote", uint64(1), f.voters[1].addr)
			data[4+32] = 1
			return data
		}, reason: "invalid arguments: not the canonical encoding"},
		{name: "dirty string padding", voter: true, data: func(t testing.TB) []byte {
			data := pack(t, "submitProposal", upgradeJSON("n", 10))
			data[len(data)-1] = 1
			return data
		}, reason: "invalid arguments: not the canonical encoding"},
		{name: "non-canonical string offset", voter: true, data: func(t testing.TB) []byte {
			data := pack(t, "submitProposal", upgradeJSON("n", 10))
			moved := append(bytes.Clone(data[:4]), common.LeftPadBytes([]byte{64}, 32)...)
			moved = append(moved, make([]byte, 32)...)
			return append(moved, data[4+32:]...)
		}, reason: "invalid arguments: not the canonical encoding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := f.newChain(t, 1)
			requireStatuses(t, c.block(1, f.submit(f.voters[0], upgradeJSON("existing", 10_000))), 1)
			sender := f.outsider
			if tc.voter {
				sender = f.voters[1]
			}
			data := tc.data(t)
			result := c.call(sender.addr, gov.Address, data)
			require.Error(t, result.Err)
			require.Equal(t, tc.reason, revertReason(result.Revert()))
			require.Less(t, result.UsedGas, uint64(callGasLimit), "a revert leaves unused gas with the caller")

			block := c.block(1, tx{from: sender, to: gov.Address, data: data})
			requireStatuses(t, block, 0)
			for _, change := range block.ChangeSet.Storage {
				require.NotEqual(t, gov.Address, change.Address, "a rejected call wrote %s", change.Key)
			}
		})
	}
}

func TestCallsThroughContracts(t *testing.T) {
	f := newFixture(t, 1)
	voter := f.voters[0]
	c := f.newChain(t, 1)
	requireStatuses(t, c.block(1, f.submit(voter, upgradeJSON("x", 10_000))), 1)
	voteData := pack(t, "vote", uint64(1), gov.OptionYes)

	// A contract calling on a voter's behalf is not the voter.
	result := c.call(voter.addr, f.proxies[opCall], voteData)
	require.Equal(t, gov.ErrNotVoter.Error(), revertReason(result.Revert()))
	// A delegate call would let any contract a voter calls vote as that voter.
	result = c.call(voter.addr, f.proxies[opDelegateCall], voteData)
	require.Equal(t, gov.ErrDelegateCall.Error(), revertReason(result.Revert()))
	result = c.call(voter.addr, f.proxies[opStaticCall], voteData)
	require.Equal(t, gov.ErrReadOnly.Error(), revertReason(result.Revert()))
	result = c.call(voter.addr, f.proxies[opStaticCall], pack(t, "submitProposal", upgradeJSON("y", 10_000)))
	require.Equal(t, gov.ErrReadOnly.Error(), revertReason(result.Revert()))

	// Views work under a static call.
	result = c.call(voter.addr, f.proxies[opStaticCall], pack(t, "proposal", uint64(1)))
	require.NoError(t, result.Err)
	values, err := gov.ABI.Methods["proposal"].Outputs.Unpack(result.ReturnData)
	require.NoError(t, err)
	var p proposalData
	require.NoError(t, jsonRoundTrip(values[0], &p))
	require.Equal(t, uint64(1), p.Id)

	for op, proxy := range f.proxies {
		block := c.block(1, tx{from: voter, to: proxy, data: voteData})
		requireStatuses(t, block, 0)
		for _, change := range block.ChangeSet.Storage {
			require.NotEqual(t, gov.Address, change.Address, "op %x wrote governance storage", op)
		}
	}
}

func TestValueIsRefused(t *testing.T) {
	f := newFixture(t, 1)
	c := f.newChain(t, 1)
	for _, data := range [][]byte{
		pack(t, "submitProposal", upgradeJSON("x", 10_000)),
		pack(t, "vote", uint64(1), gov.OptionYes),
	} {
		block := c.block(1, tx{from: f.voters[0], to: gov.Address, data: data, value: big.NewInt(1)})
		requireStatuses(t, block, 0)
		for _, change := range block.ChangeSet.Balances {
			require.NotEqual(t, gov.Address, change.Address)
		}
	}
	var count uint64
	result := c.call(f.voters[0].addr, gov.Address, pack(t, "submitProposal", upgradeJSON("x", 10_000)))
	require.NoError(t, gov.ABI.UnpackIntoInterface(&count, "submitProposal", result.ReturnData))
	require.Equal(t, uint64(1), count, "no proposal was stored")
}

func TestParamsQuery(t *testing.T) {
	f := newFixture(t, 1)
	c := f.newChain(t, 1)
	var p paramsData
	c.query(&p, "params")
	require.Equal(t, paramsData{
		VotingPeriod:          votingPeriod,
		ExpeditedVotingPeriod: votingPeriod,
		Quorum:                "0.334000000000000000",
		Threshold:             "0.500000000000000000",
		VetoThreshold:         "0.334000000000000000",
		ExpeditedQuorum:       "0.334000000000000000",
		ExpeditedThreshold:    "0.500000000000000000",
	}, p)
}

func TestQueriesOfUnknownProposalsRevert(t *testing.T) {
	f := newFixture(t, 1)
	c := f.newChain(t, 1)
	for _, id := range []uint64{0, 1, ^uint64(0)} {
		require.Equal(t, gov.ErrProposalNotFound.Error(), c.queryErr(common.Address{}, "proposal", id))
		require.Equal(t, gov.ErrProposalNotFound.Error(), c.queryErr(common.Address{}, "tallyResult", id))
		require.Equal(t, gov.ErrVoteNotFound.Error(), c.queryErr(common.Address{}, "getVote", id, f.voters[0].addr))
	}
}

func TestEndBlockEndsABoundedNumberOfProposalsPerBlock(t *testing.T) {
	f := newFixture(t, 1)
	c := f.newChain(t, 1)
	const proposals = 20
	var txs []tx
	for i := range proposals {
		txs = append(txs, f.submit(f.voters[0], upgradeJSON(fmt.Sprint(i), int64(10_000+i))), f.vote(f.voters[0], uint64(i+1), gov.OptionYes))
	}
	c.block(1, txs...)
	statuses := func() []int32 {
		out := make([]int32, proposals)
		for i := range out {
			var p proposalData
			c.query(&p, "proposal", uint64(i+1))
			out[i] = p.Status
		}
		return out
	}
	c.block(votingPeriod)
	got := statuses()
	require.Equal(t, slices.Repeat([]int32{gov.StatusPassed}, 16), got[:16])
	require.Equal(t, slices.Repeat([]int32{gov.StatusVotingPeriod}, 4), got[16:])
	plan, ok := c.plan()
	require.True(t, ok)
	require.Equal(t, uint64(16), plan.Proposal)
	var open tallyData
	c.query(&open, "tallyResult", uint64(17))

	// Another block at the backlog's end time takes no more votes on it, and ends it.
	requireStatuses(t, c.block(0, f.vote(f.voters[0], 17, gov.OptionNo)), 0)
	require.Equal(t, slices.Repeat([]int32{gov.StatusPassed}, proposals), statuses())
	var final tallyData
	c.query(&final, "tallyResult", uint64(17))
	require.Equal(t, open, final)
	plan, ok = c.plan()
	require.True(t, ok)
	require.Equal(t, gov.Plan{Name: commitHash("19"), Height: 10_019, Info: "info for 19", Proposal: 20}, plan)
}

func TestProposalsEndInSubmissionOrder(t *testing.T) {
	f := newFixture(t, 1)
	c := f.newChain(t, 1)
	requireStatuses(t, c.block(1, f.submit(f.voters[0], upgradeJSON("first", 10_000)), f.vote(f.voters[0], 1, gov.OptionYes)), 1, 1)
	requireStatuses(t, c.block(10, f.submit(f.voters[0], upgradeJSON("second", 10_000)), f.vote(f.voters[0], 2, gov.OptionYes)), 1, 1)
	c.block(votingPeriod - 10)
	plan, ok := c.plan()
	require.True(t, ok)
	require.Equal(t, commitHash("first"), plan.Name)
	c.block(9)
	plan, _ = c.plan()
	require.Equal(t, commitHash("first"), plan.Name, "the second proposal is still open")
	c.block(1)
	plan, _ = c.plan()
	require.Equal(t, commitHash("second"), plan.Name)
}

// TestSequentialAndOCCAgree runs random governance traffic through a
// sequential and an OCC executor and requires identical blocks.
func TestSequentialAndOCCAgree(t *testing.T) {
	for seed := range uint64(8) {
		t.Run(fmt.Sprint("seed=", seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, 99))
			f := newFixture(t, 5, 7, 11, 13, 17, 19)
			sequential, occ := f.newChain(t, 1), f.newChain(t, 8)
			senders := append(slices.Clone(f.voters), f.outsider)
			proposals := uint64(0)
			occAttempted := false
			for range 40 {
				var txs []tx
				for range rng.IntN(24) {
					from := senders[rng.IntN(len(senders))]
					switch rng.IntN(10) {
					case 0:
						kind := upgradeJSON(fmt.Sprint("v", rng.IntN(1_000)), int64(rng.IntN(400)+1))
						if rng.IntN(4) == 0 {
							kind = cancelJSON()
						}
						txs = append(txs, f.submit(from, kind))
						proposals++
					case 1:
						txs = append(txs, tx{from: from, to: f.proxies[opCall], data: pack(t, "vote", uint64(rng.IntN(int(proposals)+1)), gov.OptionYes)})
					default:
						txs = append(txs, f.vote(from, uint64(rng.IntN(int(proposals)+2)), int32(rng.IntN(6))))
					}
				}
				dt := uint64(rng.IntN(votingPeriod / 2))
				want, got := sequential.block(dt, txs...), occ.block(dt, txs...)
				occAttempted = occAttempted || got.OCCStats.Attempted
				require.Equal(t, want.ChangeSet, got.ChangeSet, "block %d", sequential.number)
				require.Equal(t, want.GasUsed, got.GasUsed)
				require.Equal(t, len(want.Txs), len(got.Txs))
				for i := range want.Txs {
					require.Equal(t, want.Txs[i].Status, got.Txs[i].Status, "block %d tx %d", sequential.number, i)
					require.Equal(t, want.Txs[i].GasUsed, got.Txs[i].GasUsed)
				}
				wantPlan, wantOK := sequential.plan()
				gotPlan, gotOK := occ.plan()
				require.Equal(t, wantOK, gotOK)
				require.Equal(t, wantPlan, gotPlan)
			}
			require.True(t, occAttempted, "the OCC executor never ran a block under OCC")
		})
	}
}

// TestABIMatchesPrecompilesGov requires every method to keep the selector,
// mutability and types it has in precompiles/gov.
func TestABIMatchesPrecompilesGov(t *testing.T) {
	cosmosJSON, err := os.ReadFile("../../../../precompiles/gov/abi.json")
	require.NoError(t, err)
	cosmos, err := abi.JSON(strings.NewReader(string(cosmosJSON)))
	require.NoError(t, err)
	require.Len(t, gov.ABI.Methods, 6)
	for name, method := range gov.ABI.Methods {
		want, ok := cosmos.Methods[name]
		require.True(t, ok, name)
		require.Equal(t, want.ID, method.ID, name)
		require.Equal(t, want.Sig, method.Sig, name)
		require.Equal(t, want.StateMutability, method.StateMutability, name)
		require.Equal(t, fmt.Sprint(want.Outputs), fmt.Sprint(method.Outputs), name)
	}
}

func TestRequiredGas(t *testing.T) {
	f := newFixture(t, 1, 1, 1)
	contract, err := gov.New(f.genesis, gov.Upgrades{})
	require.NoError(t, err)
	small := contract.RequiredGas(pack(t, "submitProposal", upgradeJSON("x", 1)))
	large := contract.RequiredGas(pack(t, "submitProposal", upgradeJSON(strings.Repeat("x", 140), 1)))
	require.Greater(t, large, small, "submission gas grows with the stored content")
	require.Positive(t, contract.RequiredGas(nil))
	require.Positive(t, contract.RequiredGas([]byte{1, 2, 3, 4}))

	more := newFixture(t, 1, 1, 1, 1, 1, 1)
	bigger, err := gov.New(more.genesis, gov.Upgrades{})
	require.NoError(t, err)
	tally := pack(t, "tallyResult", uint64(1))
	require.Greater(t, bigger.RequiredGas(tally), contract.RequiredGas(tally), "a live tally reads every voter's slot")
}

func TestLargestProposalRoundTrips(t *testing.T) {
	f := newFixture(t, 1)
	c := f.newChain(t, 1)
	title := strings.Repeat("t", gov.MaxTitleLength)
	description := strings.Repeat("d", gov.MaxDescriptionLength)
	name := strings.Repeat("f", gov.PlanNameLength)
	info := strings.Repeat("i", gov.MaxPlanInfoLength)
	proposal := string(must(json.Marshal(map[string]any{
		"title": title, "description": description, "type": gov.ProposalTypeSoftwareUpgrade,
		"plan": map[string]any{"name": name, "height": 10_000, "info": info},
	})))
	data := pack(t, "submitProposal", proposal)
	requireStatuses(t, c.block(1, tx{from: f.voters[0], to: gov.Address, data: data, gas: 25_000_000}, f.vote(f.voters[0], 1, gov.OptionYes)), 1, 1)
	var p proposalData
	c.query(&p, "proposal", uint64(1))
	var content struct {
		Title, Description string
		Plan               struct{ Name, Height, Info string }
	}
	require.NoError(t, json.Unmarshal(p.Content, &content))
	require.Equal(t, title, content.Title)
	require.Equal(t, description, content.Description)
	require.Equal(t, name, content.Plan.Name)
	require.Equal(t, "10000", content.Plan.Height)
	require.Equal(t, info, content.Plan.Info)
	c.block(votingPeriod)
	plan, ok := c.plan()
	require.True(t, ok)
	require.Equal(t, gov.Plan{Name: name, Height: 10_000, Info: info, Proposal: 1}, plan)
}
