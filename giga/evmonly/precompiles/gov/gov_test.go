package gov

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

var (
	alice = common.HexToAddress("0x00000000000000000000000000000000000a11ce")
	bob   = common.HexToAddress("0x0000000000000000000000000000000000000b0b")
	carol = common.HexToAddress("0x00000000000000000000000000000000000ca401")
)

const (
	second    = uint64(time.Second)
	startTime = 1_700_000_000 * second
)

// submission is a submit method call.
type submission struct {
	method string
	args   []interface{}
}

var (
	textProposal  = textSubmission("t", "d", false)
	expedProposal = textSubmission("t", "d", true)
)

func textSubmission(title, description string, expedited bool) submission {
	return submission{SubmitTextProposalMethod, []interface{}{title, description, expedited}}
}

// weight returns percent as a 1e18-scaled weight.
func weight(percent int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(percent), big.NewInt(10_000_000_000_000_000))
}

// tally returns a tally's counts for comparison.
func tally(r TallyResultData) [4]string {
	return [4]string{r.Yes.String(), r.Abstain.String(), r.No.String(), r.NoWithVeto.String()}
}

func usei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), weiPerUsei)
}

var testParams = Params{
	VotingPeriod:          100 * second,
	ExpeditedVotingPeriod: 20 * second,
	MinDeposit:            usei(10),
	MaxDepositPeriod:      50 * second,
	MinExpeditedDeposit:   usei(40),
	Quorum:                334_000_000_000_000_000,
	Threshold:             500_000_000_000_000_000,
	VetoThreshold:         334_000_000_000_000_000,
	ExpeditedQuorum:       667_000_000_000_000_000,
	ExpeditedThreshold:    667_000_000_000_000_000,
}

type harness struct {
	t      *testing.T
	gov    Precompile
	state  *memState
	time   uint64
	number uint64
	// failures are the last EndBlock's failures.
	failures []error
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, state: newMemState(), time: startTime, number: 1}
	SetParams(h.state, testParams)
	return h
}

type callOpts struct {
	value        *big.Int
	readOnly     bool
	delegateCall bool
}

// call runs method as caller. Like the EVM, it credits value to the precompile
// before running it.
func (h *harness) call(caller common.Address, opts callOpts, method string, args ...interface{}) ([]interface{}, error) {
	input, err := ABI.Pack(method, args...)
	require.NoError(h.t, err)
	if opts.value != nil {
		h.state.AddBalance(Address, opts.value)
	}
	out, err := h.gov.Run(&precompiles.Context{
		Caller:        caller,
		Address:       Address,
		ApparentValue: opts.value,
		ReadOnly:      opts.readOnly,
		DelegateCall:  opts.delegateCall,
		Block:         h.block(),
		State:         h.state,
		Logs:          h.state,
	}, input)
	if errors.Is(err, vm.ErrExecutionReverted) {
		reason, unpackErr := abi.UnpackRevert(out)
		require.NoError(h.t, unpackErr)
		return nil, fmt.Errorf("%w: %s", err, reason)
	}
	if err != nil {
		return nil, err
	}
	values, err := ABI.Methods[method].Outputs.Unpack(out)
	require.NoError(h.t, err)
	return values, nil
}

// requireReverts checks that err is a revert whose reason carries want's message.
func requireReverts(t *testing.T, err error, want error, msgAndArgs ...interface{}) {
	t.Helper()
	require.ErrorIs(t, err, vm.ErrExecutionReverted, msgAndArgs...)
	require.ErrorContains(t, err, want.Error(), msgAndArgs...)
}

func (h *harness) block() precompiles.BlockContext {
	return precompiles.BlockContext{Number: h.number, UnixNano: h.time}
}

// endBlock ends the block, keeping its failures, then starts the next one dt
// seconds later.
func (h *harness) endBlock(dt uint64) {
	failures, err := h.gov.EndBlock(h.block(), h.state)
	require.NoError(h.t, err)
	h.failures = failures
	h.number++
	h.time += dt * second
}

func (h *harness) mustCall(caller common.Address, opts callOpts, method string, args ...interface{}) []interface{} {
	values, err := h.call(caller, opts, method, args...)
	require.NoError(h.t, err)
	return values
}

func (h *harness) submit(caller common.Address, sub submission, value *big.Int) uint64 {
	return h.mustCall(caller, callOpts{value: value}, sub.method, sub.args...)[0].(uint64)
}

func (h *harness) proposal(id uint64) ProposalData {
	var out ProposalData
	h.convert(h.mustCall(alice, callOpts{}, ProposalMethod, id)[0], &out)
	return out
}

func (h *harness) convert(v interface{}, dst interface{}) {
	abi.ConvertType(v, dst)
}

func TestSubmitBelowMinimumWaitsForDeposits(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, usei(4))
	require.Equal(t, uint64(1), id)

	got := h.proposal(id)
	require.Equal(t, [4]string{"0", "0", "0", "0"}, tally(got.FinalTallyResult))
	require.Equal(t, usei(4).String(), got.TotalDeposit.String())
	got.FinalTallyResult, got.TotalDeposit = TallyResultData{}, nil
	_, content := encodeContent(textContent{Title: "t", Description: "d"})
	require.Equal(t, ProposalData{
		Id:             1,
		Status:         StatusDepositPeriod,
		SubmitTime:     startTime,
		DepositEndTime: startTime + testParams.MaxDepositPeriod,
		ContentType:    contentText,
		Content:        content,
	}, got)

	require.Equal(t, usei(4), h.state.GetBalance(DepositEscrow))
	require.Zero(t, h.state.GetBalance(Address).Sign())
	s := newStore(h.state)
	require.True(t, s.depositQueue().contains(id, startTime+testParams.MaxDepositPeriod))
	require.Zero(t, s.votingQueue().Len())
}

func TestSubmitWithoutDeposit(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, nil)
	got := h.proposal(id)
	require.Equal(t, StatusDepositPeriod, got.Status)
	require.Zero(t, got.TotalDeposit.Sign())
	require.Zero(t, newStore(h.state).depositors(id).Len())
}

func TestDepositReachingMinimumOpensVoting(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, usei(4))
	h.time += 5 * second
	h.mustCall(bob, callOpts{value: usei(6)}, DepositMethod, id)

	got := h.proposal(id)
	require.Equal(t, StatusVotingPeriod, got.Status)
	require.Equal(t, h.time, got.VotingStartTime)
	require.Equal(t, h.time+testParams.VotingPeriod, got.VotingEndTime)
	require.Equal(t, usei(10), got.TotalDeposit)

	s := newStore(h.state)
	require.Zero(t, s.depositQueue().Len())
	require.True(t, s.votingQueue().contains(id, h.time+testParams.VotingPeriod))
	require.Equal(t, usei(10), h.state.GetBalance(DepositEscrow))

	// Deposits stay open through the voting period and accumulate per depositor.
	h.mustCall(bob, callOpts{value: usei(1)}, DepositMethod, id)
	var deposit DepositData
	h.convert(h.mustCall(carol, callOpts{}, GetDepositMethod, id, bob)[0], &deposit)
	require.Equal(t, DepositData{ProposalId: id, Depositor: bob, Amount: usei(7)}, deposit)

	values := h.mustCall(carol, callOpts{}, DepositsMethod, id, []byte{})
	var deposits []DepositData
	h.convert(values[0], &deposits)
	require.Equal(t, []DepositData{
		{ProposalId: id, Depositor: alice, Amount: usei(4)},
		{ProposalId: id, Depositor: bob, Amount: usei(7)},
	}, deposits)
	require.Empty(t, values[1])
}

func TestExpeditedProposalUsesExpeditedParams(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, expedProposal, usei(10))
	require.Equal(t, StatusDepositPeriod, h.proposal(id).Status, "the regular minimum does not open an expedited proposal")

	h.mustCall(alice, callOpts{value: usei(30)}, DepositMethod, id)
	got := h.proposal(id)
	require.True(t, got.IsExpedited)
	require.Equal(t, StatusVotingPeriod, got.Status)
	require.Equal(t, h.time+testParams.ExpeditedVotingPeriod, got.VotingEndTime)
}

func TestDepositOutsideItsPeriodIsRejected(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, nil)
	h.time += testParams.MaxDepositPeriod + second
	_, err := h.call(bob, callOpts{value: usei(10)}, DepositMethod, id)
	require.ErrorIs(t, err, errInactiveProposal)

	_, err = h.call(bob, callOpts{value: usei(1)}, DepositMethod, uint64(99))
	require.ErrorIs(t, err, errUnknownProposal)
}

func TestVoteReplacesEarlierVote(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, usei(10))
	weighted := []WeightedVoteOption{{Option: OptionYes, Weight: weight(70)}, {Option: OptionNoWithVeto, Weight: weight(30)}}
	h.mustCall(bob, callOpts{}, VoteWeightedMethod, id, weighted)

	var vote VoteData
	h.convert(h.mustCall(alice, callOpts{}, GetVoteMethod, id, bob)[0], &vote)
	require.Equal(t, VoteData{ProposalId: id, Voter: bob, Options: weighted}, vote)

	h.mustCall(bob, callOpts{}, VoteMethod, id, OptionNo)
	h.convert(h.mustCall(alice, callOpts{}, GetVoteMethod, id, bob)[0], &vote)
	require.Equal(t, []WeightedVoteOption{{Option: OptionNo, Weight: weight(100)}}, vote.Options)

	// The replaced second element is cleared, as Solidity's array assignment does.
	second := solstorage.Offset(solstorage.ArrayData(voteBase(id, bob)), 1)
	require.Equal(t, common.Hash{}, h.state.GetState(Address, second))

	h.mustCall(carol, callOpts{}, VoteMethod, id, OptionAbstain)
	values := h.mustCall(alice, callOpts{}, VotesMethod, id, []byte{})
	var votes []VoteData
	h.convert(values[0], &votes)
	require.Len(t, votes, 2)
	require.Equal(t, bob, votes[0].Voter)
	require.Equal(t, carol, votes[1].Voter)
	require.Equal(t, uint64(2), newStore(h.state).votersByProposal(id).Len(), "a revote does not re-add the voter")
}

func TestVoteRejections(t *testing.T) {
	h := newHarness(t)
	pending := h.submit(alice, textProposal, nil)
	open := h.submit(alice, textProposal, usei(10))

	tests := []struct {
		name    string
		method  string
		args    []interface{}
		advance uint64
		want    error
		reverts bool
	}{
		{"deposit period", VoteMethod, []interface{}{pending, OptionYes}, 0, errInactiveProposal, false},
		{"unknown proposal", VoteMethod, []interface{}{uint64(42), OptionYes}, 0, errUnknownProposal, false},
		{"option zero", VoteMethod, []interface{}{open, int32(0)}, 0, errInvalidVote, true},
		{"option five", VoteMethod, []interface{}{open, int32(5)}, 0, errInvalidVote, true},
		{"weights under one", VoteWeightedMethod, []interface{}{open, []WeightedVoteOption{{OptionYes, weight(50)}, {OptionNo, weight(40)}}}, 0, errInvalidVote, true},
		{"weights over one", VoteWeightedMethod, []interface{}{open, []WeightedVoteOption{{OptionYes, weight(60)}, {OptionNo, weight(50)}}}, 0, errInvalidVote, true},
		{"duplicate option", VoteWeightedMethod, []interface{}{open, []WeightedVoteOption{{OptionYes, weight(50)}, {OptionYes, weight(50)}}}, 0, errInvalidVote, true},
		{"zero weight", VoteWeightedMethod, []interface{}{open, []WeightedVoteOption{{OptionYes, weight(100)}, {OptionNo, weight(0)}}}, 0, errInvalidVote, true},
		{"weight over one", VoteWeightedMethod, []interface{}{open, []WeightedVoteOption{{OptionYes, new(big.Int).Lsh(big.NewInt(1), 255)}}}, 0, errInvalidVote, true},
		{"no options", VoteWeightedMethod, []interface{}{open, []WeightedVoteOption{}}, 0, errInvalidVote, true},
		{"too many options", VoteWeightedMethod, []interface{}{open, []WeightedVoteOption{{1, weight(20)}, {2, weight(20)}, {3, weight(20)}, {4, weight(20)}, {1, weight(20)}}}, 0, errInvalidVote, true},
		{"voting ended", VoteMethod, []interface{}{open, OptionYes}, testParams.VotingPeriod + second, errInactiveProposal, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h.time = startTime + tc.advance
			_, err := h.call(bob, callOpts{}, tc.method, tc.args...)
			if tc.reverts {
				requireReverts(t, err, tc.want)
				return
			}
			require.ErrorIs(t, err, tc.want)
		})
	}

	h.time = startTime + testParams.VotingPeriod
	_, err := h.call(bob, callOpts{}, VoteMethod, open, OptionYes)
	require.NoError(t, err, "the last second of the voting period still accepts votes")
}

func TestCallGuards(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, usei(10))

	_, err := h.call(bob, callOpts{readOnly: true}, VoteMethod, id, OptionYes)
	require.ErrorIs(t, err, vm.ErrWriteProtection)
	_, err = h.call(bob, callOpts{readOnly: true}, VoteMethod, id, int32(9))
	require.ErrorIs(t, err, vm.ErrWriteProtection, "a static call fails before its arguments are checked")
	require.NotErrorIs(t, err, vm.ErrExecutionReverted)
	_, err = h.call(bob, callOpts{readOnly: true}, ProposalMethod, id)
	require.NoError(t, err, "queries run under staticcall")

	_, err = h.call(bob, callOpts{delegateCall: true}, ProposalMethod, id)
	require.ErrorIs(t, err, errDelegateCall)

	_, err = h.call(bob, callOpts{value: usei(1)}, VoteMethod, id, OptionYes)
	require.ErrorIs(t, err, errNonPayable)

	_, err = h.call(bob, callOpts{value: big.NewInt(1)}, DepositMethod, id)
	require.ErrorIs(t, err, errWeiRemainder)
	_, err = h.call(bob, callOpts{value: big.NewInt(0)}, DepositMethod, id)
	require.ErrorIs(t, err, errZeroDeposit)

	_, err = Precompile{}.Run(&precompiles.Context{State: h.state, Address: Address}, []byte{1, 2})
	require.ErrorIs(t, err, errUnknownMethod)
}

func TestProposalsQueryFiltersAndPages(t *testing.T) {
	h := newHarness(t)
	for range pageLimit + 10 {
		h.submit(alice, textProposal, nil)
	}
	voted := h.submit(alice, textProposal, usei(10))
	h.mustCall(bob, callOpts{}, VoteMethod, voted, OptionYes)
	h.mustCall(carol, callOpts{value: usei(1)}, DepositMethod, uint64(3))

	list := func(status int32, voter, depositor common.Address, key []byte) ([]uint64, []byte) {
		values := h.mustCall(alice, callOpts{}, ProposalsMethod, status, voter, depositor, key)
		var props []ProposalData
		h.convert(values[0], &props)
		ids := make([]uint64, len(props))
		for i, p := range props {
			ids[i] = p.Id
		}
		return ids, values[1].([]byte)
	}

	first, next := list(0, common.Address{}, common.Address{}, []byte{})
	require.Len(t, first, pageLimit)
	require.Equal(t, uint64(1), first[0])
	rest, last := list(0, common.Address{}, common.Address{}, next)
	require.Len(t, rest, 11)
	require.Equal(t, voted, rest[len(rest)-1])
	require.Empty(t, last)

	all := func(status int32, voter, depositor common.Address) []uint64 {
		var out []uint64
		for ids, key := list(status, voter, depositor, nil); ; ids, key = list(status, voter, depositor, key) {
			out = append(out, ids...)
			if len(key) == 0 {
				return out
			}
		}
	}
	empty, next := list(StatusVotingPeriod, common.Address{}, common.Address{}, []byte{})
	require.Empty(t, empty, "a page covers ids 1-100, none voting")
	require.NotEmpty(t, next)
	require.Equal(t, []uint64{voted}, all(StatusVotingPeriod, common.Address{}, common.Address{}))
	require.Equal(t, []uint64{voted}, all(0, bob, common.Address{}))
	require.Equal(t, []uint64{3}, all(0, common.Address{}, carol))
	require.Empty(t, all(0, bob, carol))
	require.Len(t, all(StatusDepositPeriod, common.Address{}, common.Address{}), pageLimit+10)

	_, err := h.call(alice, callOpts{}, ProposalsMethod, int32(0), common.Address{}, common.Address{}, []byte{1})
	requireReverts(t, err, errInvalidPageKey)
	_, err = h.call(alice, callOpts{}, ProposalsMethod, int32(StatusFailed+1), common.Address{}, common.Address{}, []byte{})
	requireReverts(t, err, errInvalidStatus)
	_, err = h.call(alice, callOpts{}, ProposalsMethod, int32(-1), common.Address{}, common.Address{}, []byte{})
	requireReverts(t, err, errInvalidStatus)
}

func TestStartingProposalID(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, SetStartingProposalID(h.state, 500))

	first := h.submit(alice, textProposal, nil)
	require.Equal(t, uint64(500), first)
	require.Equal(t, uint64(501), h.submit(alice, textProposal, nil))

	_, err := h.call(alice, callOpts{}, ProposalMethod, uint64(1))
	require.ErrorIs(t, err, errUnknownProposal)
	_, err = h.call(alice, callOpts{}, ProposalMethod, uint64(499))
	require.ErrorIs(t, err, errUnknownProposal)

	values := h.mustCall(alice, callOpts{}, ProposalsMethod, int32(0), common.Address{}, common.Address{}, []byte(nil))
	var props []ProposalData
	h.convert(values[0], &props)
	require.Len(t, props, 2)
	require.Equal(t, first, props[0].Id)
	require.Empty(t, values[1].([]byte))
}

func TestProposalsListsTheLastProposalID(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, SetStartingProposalID(h.state, math.MaxUint64-1))
	h.submit(alice, textProposal, nil)
	h.submit(alice, textProposal, nil)

	values := h.mustCall(alice, callOpts{}, ProposalsMethod, int32(0), common.Address{}, common.Address{}, []byte(nil))
	var props []ProposalData
	h.convert(values[0], &props)
	require.Len(t, props, 2)
	require.Equal(t, uint64(math.MaxUint64), props[1].Id)
}

func TestStartingProposalIDIsFixedOnceProposalsExist(t *testing.T) {
	h := newHarness(t)
	h.submit(alice, textProposal, nil)
	h.submit(alice, textProposal, nil)
	require.ErrorIs(t, SetStartingProposalID(h.state, 5), errProposalsExist)
	require.Equal(t, uint64(3), h.submit(alice, textProposal, nil))
	_, err := h.call(alice, callOpts{}, ProposalMethod, uint64(1))
	require.NoError(t, err, "earlier proposals stay visible")
}

func TestParamsQuery(t *testing.T) {
	h := newHarness(t)
	var got GovParams
	h.convert(h.mustCall(alice, callOpts{}, ParamsMethod)[0], &got)
	dec := func(v uint64) *big.Int { return new(big.Int).SetUint64(v) }
	require.Equal(t, GovParams{
		VotingPeriod:          100 * second,
		ExpeditedVotingPeriod: 20 * second,
		MinDeposit:            usei(10),
		MaxDepositPeriod:      50 * second,
		MinExpeditedDeposit:   usei(40),
		Quorum:                dec(testParams.Quorum),
		Threshold:             dec(testParams.Threshold),
		VetoThreshold:         dec(testParams.VetoThreshold),
		ExpeditedQuorum:       dec(testParams.ExpeditedQuorum),
		ExpeditedThreshold:    dec(testParams.ExpeditedThreshold),
	}, got)
}

// TestABIKeepsV2Methods pins the methods gov shares with precompiles/gov to
// V2's signatures, so callers of those methods keep working.
func TestABIKeepsV2Methods(t *testing.T) {
	bz, err := os.ReadFile("../../../../precompiles/gov/abi.json")
	require.NoError(t, err)
	v2, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	for _, name := range []string{VoteMethod, DepositMethod, ProposalsMethod, GetVoteMethod, VotesMethod, GetDepositMethod, DepositsMethod, ProposalMethod, ParamsMethod, TallyResultMethod} {
		require.Equal(t, v2.Methods[name].Sig, ABI.Methods[name].Sig, name)
	}
}

func TestHasParams(t *testing.T) {
	state := newMemState()
	require.False(t, HasParams(state))
	SetParams(state, testParams)
	require.True(t, HasParams(state))
}

func TestRetiredMethodsRevertNamingTheReplacement(t *testing.T) {
	h := newHarness(t)
	for sig, args := range map[string][]interface{}{
		"submitProposal(string)":                          {`{"title":"t","description":"d","type":"Text"}`},
		"voteWeighted(uint64,(int32,string)[])":           {uint64(1), []DecimalWeightedVoteOption{{Option: OptionYes, Weight: "1"}}},
		"grantVoteAuthorization(address,int64)":           {bob, int64(1)},
		"grantProposalAuthorization(address,int64)":       {bob, int64(1)},
		"revokeVoteAuthorization(address)":                {bob},
		"revokeProposalAuthorization(address)":            {bob},
		"voteWithAuthorization(address,uint64,int32)":     {bob, uint64(1), OptionYes},
		"submitProposalWithAuthorization(address,string)": {bob, "{}"},
	} {
		method := methodBySig(t, sig)
		input, err := method.Inputs.Pack(args...)
		require.NoError(t, err, sig)
		out, err := Precompile{}.Run(&precompiles.Context{State: h.state, Address: Address, ApparentValue: usei(1)}, append(method.ID, input...))
		require.ErrorIs(t, err, vm.ErrExecutionReverted, sig)
		reason, err := abi.UnpackRevert(out)
		require.NoError(t, err, sig)
		require.Equal(t, retiredMethods[sig], reason, sig)
	}
	require.Len(t, retiredMethods, 8)
	require.Zero(t, newStore(h.state).lastProposalID())
}

// TestEveryMethodIsServedOrRetired checks that each ABI method reaches a handler
// or a retired revert, and that each served method has its unsuffixed name.
func TestEveryMethodIsServedOrRetired(t *testing.T) {
	h := newHarness(t)
	for name, method := range ABI.Methods {
		if _, retired := retiredMethods[method.Sig]; retired {
			continue
		}
		require.Equal(t, method.RawName, name, "a served overload must not be renamed by go-ethereum")
		args := make([]interface{}, len(method.Inputs))
		for i, in := range method.Inputs {
			args[i] = reflect.New(in.Type.GetType()).Elem().Interface()
		}
		input, err := method.Inputs.Pack(args...)
		require.NoError(t, err, name)
		_, err = Precompile{}.Run(&precompiles.Context{State: h.state, Logs: h.state, Address: Address, Block: h.block()}, append(method.ID, input...))
		require.NotErrorIs(t, err, errUnknownMethod, name)
	}
	require.Equal(t, "voteWeighted(uint64,(int32,uint256)[])", ABI.Methods[VoteWeightedMethod].Sig)
}

func methodBySig(t *testing.T, sig string) abi.Method {
	for _, m := range ABI.Methods {
		if m.Sig == sig {
			return m
		}
	}
	t.Fatalf("no method %s", sig)
	return abi.Method{}
}

func TestCallsEmitEvents(t *testing.T) {
	h := newHarness(t)
	pending := h.submit(alice, textProposal, usei(4))
	opened := h.submit(bob, textProposal, nil)
	h.mustCall(carol, callOpts{value: usei(10)}, DepositMethod, opened)
	weighted := []WeightedVoteOption{{Option: OptionYes, Weight: weight(60)}, {Option: OptionNo, Weight: weight(40)}}
	h.mustCall(alice, callOpts{}, VoteWeightedMethod, opened, weighted)

	type event struct {
		name    string
		indexed []common.Hash
		data    []interface{}
	}
	var got []event
	for _, l := range h.state.logs {
		require.Equal(t, Address, l.Address)
		e, err := ABI.EventByID(l.Topics[0])
		require.NoError(t, err)
		data, err := e.Inputs.NonIndexed().Unpack(l.Data)
		require.NoError(t, err)
		got = append(got, event{e.Name, l.Topics[1:], data})
	}
	var options []WeightedVoteOption
	abi.ConvertType(got[4].data[0], &options)
	got[4].data = []interface{}{options}
	require.Equal(t, []event{
		{ProposalDepositEvent, []common.Hash{idTopic(pending), addressTopic(alice)}, []interface{}{usei(4), false}},
		{SubmitProposalEvent, []common.Hash{idTopic(pending), addressTopic(alice)}, []interface{}{contentText, false}},
		{SubmitProposalEvent, []common.Hash{idTopic(opened), addressTopic(bob)}, []interface{}{contentText, false}},
		{ProposalDepositEvent, []common.Hash{idTopic(opened), addressTopic(carol)}, []interface{}{usei(10), true}},
		{ProposalVoteEvent, []common.Hash{idTopic(opened), addressTopic(alice)}, []interface{}{weighted}},
	}, got)
}

// The argument tuples of retired methods.
type (
	DecimalWeightedVoteOption struct {
		Option int32
		Weight string
	}
)

func TestProposalIDsDoNotWrap(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, SetStartingProposalID(h.state, math.MaxUint64))
	require.Equal(t, uint64(math.MaxUint64), h.submit(alice, textProposal, nil))
	_, err := h.call(alice, callOpts{}, SubmitTextProposalMethod, "t", "d", false)
	require.ErrorIs(t, err, errProposalIDsExhausted)
	require.Equal(t, uint64(math.MaxUint64), newStore(h.state).lastProposalID())
}
