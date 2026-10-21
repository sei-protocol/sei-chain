package gov

import (
	"bytes"
	"math/big"
	"os"
	"sort"
	"testing"

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
	startTime     = uint64(1_700_000_000)
	textProposal  = `{"title":"t","description":"d","type":"Text"}`
	expedProposal = `{"title":"t","description":"d","type":"Text","is_expedited":true}`
)

func usei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), weiPerUsei)
}

var testParams = Params{
	VotingPeriod:          100,
	ExpeditedVotingPeriod: 20,
	MinDeposit:            usei(10),
	MaxDepositPeriod:      50,
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
	}, input)
	if err != nil {
		return nil, err
	}
	values, err := ABI.Methods[method].Outputs.Unpack(out)
	require.NoError(h.t, err)
	return values, nil
}

func (h *harness) block() precompiles.BlockContext {
	return precompiles.BlockContext{Number: h.number, Time: h.time}
}

// endBlock ends the block, then starts the next one dt seconds later.
func (h *harness) endBlock(dt uint64) {
	require.NoError(h.t, h.gov.EndBlock(h.block(), h.state))
	h.number++
	h.time += dt
}

func (h *harness) mustCall(caller common.Address, opts callOpts, method string, args ...interface{}) []interface{} {
	values, err := h.call(caller, opts, method, args...)
	require.NoError(h.t, err)
	return values
}

func (h *harness) submit(caller common.Address, content string, value *big.Int) uint64 {
	return h.mustCall(caller, callOpts{value: value}, SubmitProposalMethod, content)[0].(uint64)
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
	require.Equal(t, ProposalData{
		Id:               1,
		Status:           StatusDepositPeriod,
		FinalTallyResult: TallyResultData{Yes: "0", Abstain: "0", No: "0", NoWithVeto: "0"},
		SubmitTime:       int64(startTime),
		DepositEndTime:   int64(startTime + testParams.MaxDepositPeriod),
		TotalDeposit:     []Coin{{Amount: big.NewInt(4), Denom: "usei"}},
		Content:          []byte(textProposal),
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
	require.Empty(t, got.TotalDeposit)
	require.Zero(t, newStore(h.state).depositors(id).Len())
}

func TestDepositReachingMinimumOpensVoting(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, usei(4))
	h.time += 5
	h.mustCall(bob, callOpts{value: usei(6)}, DepositMethod, id)

	got := h.proposal(id)
	require.Equal(t, StatusVotingPeriod, got.Status)
	require.Equal(t, int64(h.time), got.VotingStartTime)
	require.Equal(t, int64(h.time+testParams.VotingPeriod), got.VotingEndTime)
	require.Equal(t, []Coin{{Amount: big.NewInt(10), Denom: "usei"}}, got.TotalDeposit)

	s := newStore(h.state)
	require.Zero(t, s.depositQueue().Len())
	require.True(t, s.votingQueue().contains(id, h.time+testParams.VotingPeriod))
	require.Equal(t, usei(10), h.state.GetBalance(DepositEscrow))

	// Deposits stay open through the voting period and accumulate per depositor.
	h.mustCall(bob, callOpts{value: usei(1)}, DepositMethod, id)
	var deposit DepositData
	h.convert(h.mustCall(carol, callOpts{}, GetDepositMethod, id, bob)[0], &deposit)
	require.Equal(t, DepositData{ProposalId: id, Depositor: bob.Hex(), Amount: []Coin{{Amount: big.NewInt(7), Denom: "usei"}}}, deposit)

	values := h.mustCall(carol, callOpts{}, DepositsMethod, id, []byte{})
	var deposits []DepositData
	h.convert(values[0], &deposits)
	require.Equal(t, []DepositData{
		{ProposalId: id, Depositor: alice.Hex(), Amount: []Coin{{Amount: big.NewInt(4), Denom: "usei"}}},
		{ProposalId: id, Depositor: bob.Hex(), Amount: []Coin{{Amount: big.NewInt(7), Denom: "usei"}}},
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
	require.Equal(t, int64(h.time+testParams.ExpeditedVotingPeriod), got.VotingEndTime)
}

func TestDepositOutsideItsPeriodIsRejected(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, nil)
	h.time += testParams.MaxDepositPeriod + 1
	_, err := h.call(bob, callOpts{value: usei(10)}, DepositMethod, id)
	require.ErrorIs(t, err, errInactiveProposal)

	_, err = h.call(bob, callOpts{value: usei(1)}, DepositMethod, uint64(99))
	require.ErrorIs(t, err, errUnknownProposal)
}

func TestVoteReplacesEarlierVote(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, usei(10))
	weighted := []WeightedVoteOptionData{{Option: OptionYes, Weight: "0.7"}, {Option: OptionNoWithVeto, Weight: ".3"}}
	h.mustCall(bob, callOpts{}, VoteWeightedMethod, id, weighted)

	var vote VoteData
	h.convert(h.mustCall(alice, callOpts{}, GetVoteMethod, id, bob)[0], &vote)
	require.Equal(t, VoteData{ProposalId: id, Voter: bob.Hex(), Options: []WeightedVoteOptionData{
		{Option: OptionYes, Weight: "0.700000000000000000"},
		{Option: OptionNoWithVeto, Weight: "0.300000000000000000"},
	}}, vote)

	h.mustCall(bob, callOpts{}, VoteMethod, id, OptionNo)
	h.convert(h.mustCall(alice, callOpts{}, GetVoteMethod, id, bob)[0], &vote)
	require.Equal(t, []WeightedVoteOptionData{{Option: OptionNo, Weight: "1.000000000000000000"}}, vote.Options)

	// The replaced second element is cleared, as Solidity's array assignment does.
	second := solstorage.Offset(solstorage.ArrayData(voteBase(id, bob)), 1)
	require.Equal(t, common.Hash{}, h.state.GetState(Address, second))

	h.mustCall(carol, callOpts{}, VoteMethod, id, OptionAbstain)
	values := h.mustCall(alice, callOpts{}, VotesMethod, id, []byte{})
	var votes []VoteData
	h.convert(values[0], &votes)
	require.Len(t, votes, 2)
	require.Equal(t, bob.Hex(), votes[0].Voter)
	require.Equal(t, carol.Hex(), votes[1].Voter)
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
	}{
		{"deposit period", VoteMethod, []interface{}{pending, OptionYes}, 0, errInactiveProposal},
		{"unknown proposal", VoteMethod, []interface{}{uint64(42), OptionYes}, 0, errUnknownProposal},
		{"option zero", VoteMethod, []interface{}{open, int32(0)}, 0, errInvalidVote},
		{"option five", VoteMethod, []interface{}{open, int32(5)}, 0, errInvalidVote},
		{"weights under one", VoteWeightedMethod, []interface{}{open, []WeightedVoteOptionData{{OptionYes, "0.5"}, {OptionNo, "0.4"}}}, 0, errInvalidVote},
		{"duplicate option", VoteWeightedMethod, []interface{}{open, []WeightedVoteOptionData{{OptionYes, "0.5"}, {OptionYes, "0.5"}}}, 0, errInvalidVote},
		{"zero weight", VoteWeightedMethod, []interface{}{open, []WeightedVoteOptionData{{OptionYes, "1"}, {OptionNo, "0"}}}, 0, errInvalidVote},
		{"no options", VoteWeightedMethod, []interface{}{open, []WeightedVoteOptionData{}}, 0, errInvalidVote},
		{"too many options", VoteWeightedMethod, []interface{}{open, []WeightedVoteOptionData{{1, "0.2"}, {2, "0.2"}, {3, "0.2"}, {4, "0.2"}, {1, "0.2"}}}, 0, errInvalidVote},
		{"voting ended", VoteMethod, []interface{}{open, OptionYes}, testParams.VotingPeriod + 1, errInactiveProposal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h.time = startTime + tc.advance
			_, err := h.call(bob, callOpts{}, tc.method, tc.args...)
			require.ErrorIs(t, err, tc.want)
		})
	}

	h.time = startTime
	_, err := h.call(bob, callOpts{}, VoteWeightedMethod, open, []WeightedVoteOptionData{{OptionYes, "1/2"}})
	require.ErrorContains(t, err, "invalid weight format")
	h.time = startTime + testParams.VotingPeriod
	_, err = h.call(bob, callOpts{}, VoteMethod, open, OptionYes)
	require.NoError(t, err, "the last second of the voting period still accepts votes")
}

func TestCallGuards(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, usei(10))

	_, err := h.call(bob, callOpts{readOnly: true}, VoteMethod, id, OptionYes)
	require.ErrorIs(t, err, vm.ErrWriteProtection)
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

	_, err = h.call(bob, callOpts{}, SubmitProposalMethod, `{"title":"t"}`)
	require.ErrorContains(t, err, "required")
	_, err = h.call(bob, callOpts{}, SubmitProposalMethod, `not json`)
	require.ErrorContains(t, err, "failed to parse proposal JSON")

	authz := abiOf(t, "grantVoteAuthorization(address,int64)")
	input, err := authz.Pack("grantVoteAuthorization", bob, int64(1))
	require.NoError(t, err)
	_, err = Precompile{}.Run(&precompiles.Context{State: h.state, Address: Address}, input)
	require.ErrorIs(t, err, errUnknownMethod)
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

	first, next := list(0, common.Address{}, common.Address{}, nil)
	require.Len(t, first, pageLimit)
	require.Equal(t, uint64(1), first[0])
	rest, last := list(0, common.Address{}, common.Address{}, next)
	require.Len(t, rest, 11)
	require.Equal(t, voted, rest[len(rest)-1])
	require.Empty(t, last)

	ids, _ := list(StatusVotingPeriod, common.Address{}, common.Address{}, nil)
	require.Equal(t, []uint64{voted}, ids)
	ids, _ = list(0, bob, common.Address{}, nil)
	require.Equal(t, []uint64{voted}, ids)
	ids, _ = list(0, common.Address{}, carol, nil)
	require.Equal(t, []uint64{3}, ids)
	ids, _ = list(0, bob, carol, nil)
	require.Empty(t, ids)
	ids, _ = list(StatusDepositPeriod, common.Address{}, common.Address{}, nil)
	require.Len(t, ids, pageLimit)

	_, err := h.call(alice, callOpts{}, ProposalsMethod, int32(0), common.Address{}, common.Address{}, []byte{1})
	require.ErrorIs(t, err, errInvalidPageKey)
}

func TestStartingProposalID(t *testing.T) {
	h := newHarness(t)
	SetStartingProposalID(h.state, 500)

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

func TestStartingProposalIDNeverReusesIDs(t *testing.T) {
	h := newHarness(t)
	h.submit(alice, textProposal, nil)
	h.submit(alice, textProposal, nil)
	SetStartingProposalID(h.state, 2)
	require.Equal(t, uint64(3), h.submit(alice, textProposal, nil))
}

func TestParamsQuery(t *testing.T) {
	h := newHarness(t)
	var got GovParams
	h.convert(h.mustCall(alice, callOpts{}, ParamsMethod)[0], &got)
	require.Equal(t, GovParams{
		VotingPeriod:          100,
		ExpeditedVotingPeriod: 20,
		MinDeposit:            []Coin{{Amount: big.NewInt(10), Denom: "usei"}},
		MaxDepositPeriod:      50,
		MinExpeditedDeposit:   []Coin{{Amount: big.NewInt(40), Denom: "usei"}},
		Quorum:                "0.334000000000000000",
		Threshold:             "0.500000000000000000",
		VetoThreshold:         "0.334000000000000000",
		ExpeditedQuorum:       "0.667000000000000000",
		ExpeditedThreshold:    "0.667000000000000000",
	}, got)
}

// TestABIMatchesV2WithoutAuthz pins the ABI to precompiles/gov's, less the
// methods sei-chain#4569 removes.
func TestABIMatchesV2WithoutAuthz(t *testing.T) {
	bz, err := os.ReadFile("../../../../precompiles/gov/abi.json")
	require.NoError(t, err)
	v2, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	removed := map[string]bool{
		"grantVoteAuthorization":          true,
		"grantProposalAuthorization":      true,
		"voteWithAuthorization":           true,
		"revokeVoteAuthorization":         true,
		"revokeProposalAuthorization":     true,
		"submitProposalWithAuthorization": true,
	}
	var want, got []string
	for name, m := range v2.Methods {
		if !removed[name] {
			want = append(want, m.Sig+" "+m.String())
		}
	}
	for _, m := range ABI.Methods {
		got = append(got, m.Sig+" "+m.String())
	}
	sort.Strings(want)
	sort.Strings(got)
	require.Equal(t, want, got)
}

func TestHasParams(t *testing.T) {
	state := newMemState()
	require.False(t, HasParams(state))
	SetParams(state, testParams)
	require.True(t, HasParams(state))
}
