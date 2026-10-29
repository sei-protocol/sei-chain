package gov

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// equalElectorate gives alice, bob and carol 100 tokens each.
var equalElectorate = GenesisElectorate{alice: big.NewInt(100), bob: big.NewInt(100), carol: big.NewInt(100)}

func newElectedHarness(t *testing.T, e Electorate) *harness {
	h := newHarness(t)
	h.gov = Precompile{Electorate: e}
	return h
}

// open submits content with the minimum deposit, putting it straight to a vote.
func (h *harness) open(content submission, deposit *big.Int) uint64 {
	return h.submit(alice, content, deposit)
}

func (h *harness) vote(voter common.Address, id uint64, option int32) {
	h.mustCall(voter, callOpts{}, VoteMethod, id, option)
}

func (h *harness) voteWeighted(voter common.Address, id uint64, percents map[int32]int64) {
	var args []WeightedVoteOption
	for _, o := range []int32{OptionYes, OptionAbstain, OptionNo, OptionNoWithVeto} {
		if p, ok := percents[o]; ok {
			args = append(args, WeightedVoteOption{Option: o, Weight: weight(p)})
		}
	}
	h.mustCall(voter, callOpts{}, VoteWeightedMethod, id, args)
}

func (h *harness) status(id uint64) int32 {
	return h.proposal(id).Status
}

func TestEndBlockDropsProposalShortOfDeposit(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.submit(alice, textProposal, usei(4))
	h.mustCall(bob, callOpts{value: usei(3)}, DepositMethod, id)

	h.time = startTime + testParams.MaxDepositPeriod - second
	h.endBlock(1)
	require.Equal(t, StatusDepositPeriod, h.status(id), "still open before its deposit end time")

	h.endBlock(1) // the block at exactly the deposit end time drops it
	_, err := h.call(alice, callOpts{}, ProposalMethod, id)
	require.ErrorIs(t, err, errUnknownProposal)
	require.Zero(t, h.state.GetBalance(DepositEscrow).Sign(), "deposits burn")
	require.Zero(t, h.state.GetBalance(alice).Sign())
	require.Zero(t, h.state.GetBalance(bob).Sign())
	require.Zero(t, newStore(h.state).depositQueue().Len())
	require.Zero(t, newStore(h.state).depositors(id).Len())
	require.Empty(t, h.state.storage[Address][proposalBase(id)], "proposal storage is zeroed")
}

func TestEndBlockPassesAndRefunds(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.open(textProposal, usei(10))
	h.vote(alice, id, OptionYes)
	h.vote(bob, id, OptionYes)
	h.vote(carol, id, OptionNo)

	h.time = startTime + testParams.VotingPeriod
	h.endBlock(1)

	got := h.proposal(id)
	require.Equal(t, StatusPassed, got.Status)
	require.Equal(t, [4]string{"200", "0", "100", "0"}, tally(got.FinalTallyResult))
	require.Equal(t, usei(10), h.state.GetBalance(alice), "deposit refunded")
	require.Zero(t, h.state.GetBalance(DepositEscrow).Sign())
	require.Zero(t, newStore(h.state).votingQueue().Len())

	_, err := h.call(bob, callOpts{}, VoteMethod, id, OptionNo)
	require.ErrorIs(t, err, errInactiveProposal)
}

func TestVotingEndsAtTheNanosecond(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	params := testParams
	params.VotingPeriod = 100*second + 1
	require.NoError(t, SetParams(h.state, params))
	id := h.open(textProposal, usei(10))
	h.vote(alice, id, OptionYes)
	h.vote(bob, id, OptionYes)

	h.time = startTime + 100*second
	h.endBlock(0)
	require.Equal(t, StatusVotingPeriod, h.status(id), "one nanosecond short of the end")
	h.time++
	h.endBlock(0)
	require.Equal(t, StatusPassed, h.status(id))
}

func TestEndBlockWaitsForVotingEnd(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.open(textProposal, usei(10))
	h.time = startTime + testParams.VotingPeriod - second
	h.endBlock(1)
	require.Equal(t, StatusVotingPeriod, h.status(id))

	h.vote(alice, id, OptionYes) // a vote in the ending block counts
	h.vote(bob, id, OptionYes)
	h.endBlock(1)
	require.Equal(t, StatusPassed, h.status(id))
}

func TestEndBlockTallyOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		votes    map[common.Address]int32
		status   int32
		refunded bool
	}{
		{"no quorum burns", map[common.Address]int32{alice: OptionYes}, StatusRejected, false},
		{"veto burns", map[common.Address]int32{alice: OptionNoWithVeto, bob: OptionNoWithVeto, carol: OptionYes}, StatusRejected, false},
		{"all abstain refunds", map[common.Address]int32{alice: OptionAbstain, bob: OptionAbstain}, StatusRejected, true},
		{"tie fails threshold", map[common.Address]int32{alice: OptionYes, bob: OptionNo}, StatusRejected, true},
		{"abstain is left out of threshold", map[common.Address]int32{alice: OptionYes, bob: OptionAbstain, carol: OptionAbstain}, StatusPassed, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newElectedHarness(t, equalElectorate)
			id := h.open(textProposal, usei(10))
			for voter, option := range tc.votes {
				h.vote(voter, id, option)
			}
			h.time = startTime + testParams.VotingPeriod
			h.endBlock(1)
			require.Equal(t, tc.status, h.status(id))
			require.Zero(t, h.state.GetBalance(DepositEscrow).Sign())
			if tc.refunded {
				require.Equal(t, usei(10), h.state.GetBalance(alice))
			} else {
				require.Zero(t, h.state.GetBalance(alice).Sign())
			}
		})
	}
}

func TestEndBlockWithoutElectorateRejectsAndRefunds(t *testing.T) {
	h := newHarness(t)
	id := h.open(textProposal, usei(10))
	h.vote(alice, id, OptionYes)
	h.time = startTime + testParams.VotingPeriod
	h.endBlock(1)
	require.Equal(t, StatusRejected, h.status(id))
	require.Equal(t, usei(10), h.state.GetBalance(alice))
}

func TestEndBlockWeightedVotes(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.open(textProposal, usei(10))
	h.voteWeighted(alice, id, map[int32]int64{OptionYes: 75, OptionNo: 25})
	h.vote(bob, id, OptionNo)
	h.time = startTime + testParams.VotingPeriod
	h.endBlock(1)
	got := h.proposal(id)
	require.Equal(t, StatusRejected, got.Status, "75 yes of 200")
	require.Equal(t, [4]string{"75", "0", "125", "0"}, tally(got.FinalTallyResult))
}

func TestEndBlockConvertsFailedExpedited(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.open(expedProposal, usei(40))
	h.vote(alice, id, OptionYes)
	h.vote(bob, id, OptionYes) // 2/3 turnout misses the expedited quorum

	h.time = startTime + testParams.ExpeditedVotingPeriod
	h.endBlock(1)
	got := h.proposal(id)
	require.Equal(t, StatusVotingPeriod, got.Status)
	require.Equal(t, startTime+testParams.VotingPeriod, got.VotingEndTime)
	require.Equal(t, usei(40), h.state.GetBalance(DepositEscrow), "deposits stay for the regular period")
	_, err := h.call(alice, callOpts{}, GetVoteMethod, id, alice)
	require.ErrorIs(t, err, errVoteNotFound, "the regular period starts a fresh round of votes")

	h.vote(alice, id, OptionYes)
	h.vote(bob, id, OptionYes)
	h.time = startTime + testParams.VotingPeriod
	h.endBlock(1)
	require.Equal(t, StatusPassed, h.status(id))
	require.Equal(t, usei(40), h.state.GetBalance(alice))
}

func TestEndBlockPassesExpedited(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.open(expedProposal, usei(40))
	for _, voter := range []common.Address{alice, bob, carol} {
		h.vote(voter, id, OptionYes)
	}
	h.time = startTime + testParams.ExpeditedVotingPeriod
	h.endBlock(1)
	require.Equal(t, StatusPassed, h.status(id))
}

func (h *harness) pass(content submission) uint64 {
	id := h.open(content, usei(10))
	for _, voter := range []common.Address{alice, bob, carol} {
		h.vote(voter, id, OptionYes)
	}
	h.time += testParams.VotingPeriod
	h.endBlock(1)
	return id
}

func TestSubmitRejectsInvalidContent(t *testing.T) {
	h := newHarness(t)
	for name, tc := range map[string]struct {
		sub  submission
		want error
	}{
		"no title":         {textSubmission("", "d", false), errMissingText},
		"no description":   {textSubmission("t", "", false), errMissingText},
		"long title":       {textSubmission(strings.Repeat("t", maxTitleLength+1), "d", false), errContentTooLong},
		"long description": {textSubmission("t", strings.Repeat("d", maxDescriptionLength+1), false), errContentTooLong},
	} {
		_, err := h.call(alice, callOpts{}, tc.sub.method, tc.sub.args...)
		requireReverts(t, err, tc.want, name)
	}
	require.Zero(t, newStore(h.state).lastProposalID())
}

func TestSubmitAcceptsContentAtLimits(t *testing.T) {
	h := newHarness(t)
	h.submit(alice, textSubmission(strings.Repeat("t", maxTitleLength), strings.Repeat("d", maxDescriptionLength), false), nil)
}

// delegatedElectorate has validators v1 (100 tokens, 50 shares) and v2 (60
// tokens, 60 shares). alice delegates 10 shares to v1 and 30 to v2.
type delegatedElectorate struct{}

var (
	v1 = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	v2 = common.HexToAddress("0x00000000000000000000000000000000000000a2")
)

func (delegatedElectorate) BondedValidators(precompiles.State) []Validator {
	return []Validator{
		{Operator: v1, BondedTokens: big.NewInt(100), DelegatorShares: big.NewInt(50)},
		{Operator: v2, BondedTokens: big.NewInt(60), DelegatorShares: big.NewInt(60)},
	}
}

func (delegatedElectorate) Delegations(_ precompiles.State, delegator common.Address) []Delegation {
	if delegator != alice {
		return nil
	}
	return []Delegation{{Validator: v1, Shares: big.NewInt(10)}, {Validator: v2, Shares: big.NewInt(30)}}
}

func (delegatedElectorate) TotalBonded(precompiles.State) *big.Int {
	return big.NewInt(160)
}

func TestTallyDeductsDelegatorVotesFromValidators(t *testing.T) {
	h := newElectedHarness(t, delegatedElectorate{})
	id := h.open(textProposal, usei(10))
	h.vote(alice, id, OptionNo) // 10 shares of v1 = 20 tokens, 30 of v2 = 30 tokens
	h.vote(v1, id, OptionYes)   // v1 keeps 40 shares = 80 tokens
	h.vote(bob, id, OptionYes)  // no delegations, no power

	h.time += testParams.VotingPeriod
	h.endBlock(1)
	got := h.proposal(id)
	require.Equal(t, StatusPassed, got.Status)
	require.Equal(t, [4]string{"80", "0", "50", "0"}, tally(got.FinalTallyResult))
	require.Zero(t, newStore(h.state).tallyDeduction(id, v1).Sign(), "deductions are cleared")
}

// depositor returns the nth of a run of distinct test depositors.
func depositor(n int) common.Address {
	return common.BigToAddress(big.NewInt(int64(0x1000 + n)))
}

// fillDepositors gives proposal id n distinct deposits of the least a new
// depositor may give.
func (h *harness) fillDepositors(id uint64, n int) {
	prop, _ := newStore(h.state).proposal(id)
	share := depositorShare(newStore(h.state).params(), prop.isExpedited)
	for d := range n {
		h.mustCall(depositor(d), callOpts{value: share}, DepositMethod, id)
	}
}

func TestDepositorsAreCapped(t *testing.T) {
	h := newHarness(t)
	id := h.submit(alice, textProposal, nil)
	h.fillDepositors(id, maxDepositors)
	require.Equal(t, StatusVotingPeriod, h.status(id), "a full set of depositors reaches the minimum deposit")

	_, err := h.call(bob, callOpts{value: usei(1)}, DepositMethod, id)
	require.ErrorIs(t, err, errTooManyDepositors)
	h.mustCall(depositor(0), callOpts{value: usei(1)}, DepositMethod, id)
	require.Equal(t, usei(2), newStore(h.state).deposit(id, depositor(0)), "an existing depositor may add more")
}

func TestEndBlockCarriesOverPastBudget(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	params := testParams
	params.MinDeposit = usei(1_000_000)
	params.MinExpeditedDeposit = usei(2_000_000)
	require.NoError(t, SetParams(h.state, params))
	const proposals = 6
	for range proposals {
		h.fillDepositors(h.submit(alice, textProposal, nil), maxDepositors-1)
	}
	q := newStore(h.state).depositQueue()
	settled := int(endBlockBudget / must(dropCost(newStore(h.state), 1)))
	require.Equal(t, 4, settled, "each costs its depositors and content")

	h.time = startTime + params.MaxDepositPeriod
	h.endBlock(1)
	require.Equal(t, uint64(proposals-settled), q.Len(), "the rest wait for the next block")
	for id := range uint64(proposals) {
		_, open := newStore(h.state).proposal(id + 1)
		require.Equal(t, id >= uint64(settled), open, "proposal %d: lowest ids settle first", id+1)
	}

	h.endBlock(1)
	require.Zero(t, q.Len())
	require.Zero(t, h.state.GetBalance(DepositEscrow).Sign(), "all deposits burn")
}

func TestDropCostCountsContent(t *testing.T) {
	h := newHarness(t)
	short := h.submit(alice, textProposal, nil)
	long := h.submit(alice, textSubmission("t", strings.Repeat("d", maxDescriptionLength), false), nil)
	s := newStore(h.state)
	require.Equal(t, 1+must(s.proposalContentChunks(short)), must(dropCost(s, short)))
	require.Greater(t, must(dropCost(s, long)), uint64(maxDescriptionLength/32), "a word per 32 bytes of content")
}

func TestSettleCostCountsContent(t *testing.T) {
	h := newHarness(t)
	short := h.submit(alice, textProposal, nil)
	long := h.submit(alice, textSubmission("t", strings.Repeat("d", maxDescriptionLength), false), nil)
	s := newStore(h.state)
	snap := snapshotElectorate(h.state, equalElectorate)
	require.Equal(t, uint64(len(snap.validators))+1+must(s.proposalContentChunks(short)), must(settleCost(s, short, snap)))
	require.Greater(t, must(settleCost(s, long, snap)), uint64(maxDescriptionLength/32), "a word per 32 bytes of content")
}

func TestEndBlockEndsVotingPastSpentDropBudget(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	params := testParams
	params.MinDeposit = usei(1_000_000)
	params.MinExpeditedDeposit = usei(2_000_000)
	require.NoError(t, SetParams(h.state, params))
	voted := h.open(textProposal, params.MinDeposit)
	h.vote(alice, voted, OptionYes)
	h.vote(bob, voted, OptionYes)
	for range 6 {
		h.fillDepositors(h.submit(alice, textProposal, nil), maxDepositors-1)
	}

	h.time = startTime + params.VotingPeriod
	h.endBlock(1)
	require.NotZero(t, newStore(h.state).depositQueue().Len(), "drops past their budget wait")
	require.Equal(t, StatusPassed, h.status(voted), "drops do not spend the voting budget")
}

func TestProposalQueueOrdersByEndThenID(t *testing.T) {
	q := newStore(newMemState()).votingQueue()
	q.add(5, 100)
	q.add(3, 200)
	q.add(4, 100)
	q.add(9, 50)

	_, _, ok := q.due(49)
	require.False(t, ok, "nothing ends by 49")
	var order []uint64
	for id, end, ok := q.due(200); ok; id, end, ok = q.due(200) {
		order = append(order, id)
		require.Equal(t, map[uint64]uint64{3: 200, 4: 100, 5: 100, 9: 50}[id], end)
		require.True(t, q.contains(id, end))
		q.remove(id, end)
	}
	require.Equal(t, []uint64{9, 4, 5, 3}, order)
}

func TestEndBlockTalliesAcrossBlocks(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.open(textProposal, usei(10))
	h.vote(alice, id, OptionYes)
	h.vote(bob, id, OptionYes)
	for d := range endBlockBudget {
		h.vote(depositor(d), id, OptionNo) // no stake, so no power
	}

	h.time += testParams.VotingPeriod
	h.endBlock(0) // the next block shares the voting end time
	s := newStore(h.state)
	require.Equal(t, StatusVotingPeriod, h.status(id), "the count carries over")
	require.True(t, s.tallyCounting(id))
	_, err := h.call(carol, callOpts{}, VoteMethod, id, OptionNo)
	require.ErrorIs(t, err, errInactiveProposal, "no votes once counting starts")
	_, err = h.call(carol, callOpts{value: usei(1)}, DepositMethod, id)
	require.ErrorIs(t, err, errInactiveProposal, "no deposits once counting starts")

	h.endBlock(1)
	got := h.proposal(id)
	require.Equal(t, StatusPassed, got.Status)
	require.Equal(t, [4]string{"200", "0", "0", "0"}, tally(got.FinalTallyResult))
	require.False(t, s.tallyCounting(id))
	require.Zero(t, s.votersByProposal(id).Len())
	require.Empty(t, must(s.vote(id, alice)), "counted votes are removed")
	require.Empty(t, must(s.vote(id, depositor(0))))
}

func TestDecArithmeticRoundsHalfToEven(t *testing.T) {
	half := "500000000000000000"
	dec := func(s string) *big.Int {
		v, ok := new(big.Int).SetString(s, 10)
		require.True(t, ok)
		return v
	}
	// 0.5e-18 rounds to even 0; 1.5e-18 rounds up to 2e-18.
	require.Equal(t, "0", mulDec(big.NewInt(1), dec(half)).String())
	require.Equal(t, "2", mulDec(big.NewInt(3), dec(half)).String())
	// 2/3 is 0.666666666666666667.
	require.Equal(t, "666666666666666667", quoDec(dec("2000000000000000000"), dec("3000000000000000000")).String())
	// 1/8e18 is 0.125e-18, which rounds to 0.
	require.Equal(t, "0", quoDec(big.NewInt(1), dec("8000000000000000000")).String())
}

func TestDepositorFirstDepositMustMeetShare(t *testing.T) {
	h := newHarness(t)
	params := testParams
	params.MinDeposit = usei(1_000)
	params.MinExpeditedDeposit = usei(2_000)
	require.NoError(t, SetParams(h.state, params))
	id := h.submit(alice, textProposal, nil)

	share := depositorShare(params, false)
	require.Equal(t, usei(5), share, "1000 usei across 200 depositors")
	_, err := h.call(bob, callOpts{value: usei(4)}, DepositMethod, id)
	require.ErrorIs(t, err, errDepositTooSmall)
	h.mustCall(bob, callOpts{value: share}, DepositMethod, id)
	h.mustCall(bob, callOpts{value: usei(1)}, DepositMethod, id)
	require.Equal(t, usei(6), newStore(h.state).deposit(id, bob), "a depositor may then add any amount")
}

func TestDepositorShareRoundsUpToWholeUsei(t *testing.T) {
	params := testParams
	params.MinDeposit = usei(201)
	require.Equal(t, usei(2), depositorShare(params, false))
	require.Equal(t, usei(1), depositorShare(testParams, false))
}

func TestTakeRunsOversizedWorkAlone(t *testing.T) {
	budget := uint64(endBlockBudget)
	require.True(t, take(&budget, endBlockBudget+1), "a fresh budget runs it")
	require.Zero(t, budget)

	budget = endBlockBudget
	require.True(t, take(&budget, 1))
	require.False(t, take(&budget, endBlockBudget), "a spent budget waits for the next block")
}

func TestEndBlockFinishesTallyLargerThanBudget(t *testing.T) {
	electorate := GenesisElectorate{alice: big.NewInt(endBlockBudget)}
	for v := range endBlockBudget {
		electorate[depositor(v)] = big.NewInt(1)
	}
	h := newElectedHarness(t, electorate)
	id := h.open(textProposal, usei(10))
	h.vote(alice, id, OptionYes)

	h.time += testParams.VotingPeriod
	h.endBlock(1)
	require.Equal(t, StatusVotingPeriod, h.status(id), "counting the vote spent part of the budget")
	h.endBlock(1)
	require.Equal(t, StatusPassed, h.status(id), "the finish runs alone on a fresh budget")
}

func (h *harness) tallyResult(id uint64) TallyResultData {
	var got TallyResultData
	h.convert(h.mustCall(alice, callOpts{}, TallyResultMethod, id)[0], &got)
	return got
}

func TestTallyResultIsLiveDuringVoting(t *testing.T) {
	h := newElectedHarness(t, delegatedElectorate{})
	id := h.open(textProposal, usei(10))
	h.vote(alice, id, OptionNo)
	h.vote(v1, id, OptionYes)

	live := [4]string{"80", "0", "50", "0"}
	require.Equal(t, live, tally(h.tallyResult(id)))
	require.Zero(t, newStore(h.state).tallyDeduction(id, v1).Sign(), "the query writes nothing")
	require.False(t, newStore(h.state).tallyCounting(id))

	h.time += testParams.VotingPeriod
	h.endBlock(1)
	require.Equal(t, StatusPassed, h.status(id))
	require.Equal(t, live, tally(h.tallyResult(id)), "the final tally matches the live one")
}

func TestTallyResultIsLiveWhileCounting(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.open(textProposal, usei(10))
	h.vote(alice, id, OptionYes)
	h.vote(bob, id, OptionYes)
	for d := range endBlockBudget {
		h.vote(depositor(d), id, OptionNo)
	}
	h.time += testParams.VotingPeriod
	h.endBlock(0)
	require.True(t, newStore(h.state).tallyCounting(id))
	require.Equal(t, [4]string{"200", "0", "0", "0"}, tally(h.tallyResult(id)))
}

func TestTallyResultStartsOverAfterExpeditedConversion(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.open(expedProposal, usei(40))
	h.vote(alice, id, OptionYes)
	h.vote(bob, id, OptionYes)
	h.time = startTime + testParams.ExpeditedVotingPeriod
	h.endBlock(1)
	require.Equal(t, StatusVotingPeriod, h.status(id))
	require.Equal(t, [4]string{"0", "0", "0", "0"}, tally(h.tallyResult(id)))
}

func TestEndBlockDrainsQueuedMissingProposals(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	s := newStore(h.state)
	s.SetUint64(at(solstorage.Slot(slotProposalIDs), lastProposalID), lastProposalID.offset, 99) // ids issued, records gone
	s.depositQueue().add(98, startTime)
	s.votingQueue().add(99, startTime)
	h.endBlock(1)
	require.Zero(t, s.depositQueue().Len())
	require.Zero(t, s.votingQueue().Len())
	_, ok := s.proposal(99)
	require.False(t, ok, "no proposal is written for a missing id")
}

func TestLiveTallyStopsOnceReadsFail(t *testing.T) {
	electorate := GenesisElectorate{}
	for i := range 300 {
		electorate[depositor(1_000+i)] = big.NewInt(100)
	}
	h := newElectedHarness(t, electorate)
	id := h.open(textProposal, usei(10))
	for i := range 50 {
		h.vote(depositor(i), id, OptionYes)
	}
	h.state.failAt = h.state.reads + 20
	_, _ = liveTally(h.gov.Electorate, newStore(h.state), id)
	require.True(t, h.state.Failed())
	require.Equal(t, 1, h.state.readsAfterFailure, "neither the voter nor the validator loop reads on for free")
}

func TestStoredVoteOutsideTheOptionsIsAnError(t *testing.T) {
	h := newHarness(t)
	s := newStore(h.state)
	s.setVote(1, alice, []weightedVote{{option: 0, weight: decOne}})
	_, err := s.vote(1, alice)
	require.ErrorIs(t, err, errInvalidStoredVote, "a vote castVote could not have written")
}

// passing opens a proposal and votes it through, ready to pass at the end of
// its voting period.
func (h *harness) passing() uint64 {
	id := h.open(textProposal, usei(10))
	for _, voter := range []common.Address{alice, bob, carol} {
		h.vote(voter, id, OptionYes)
	}
	return id
}

// corruptContent gives proposal id a content length no write produces.
func (h *harness) corruptContent(id uint64) {
	head := at(proposalBase(id), proposalContent)
	h.state.SetState(Address, head, common.BigToHash(new(big.Int).SetUint64((solstorage.MaxStringLength+1)*2+1)))
}

func TestEndBlockFailsABadProposalInPlaceAndSettlesAGoodOne(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	bad := h.passing()
	good := h.passing()
	h.corruptContent(bad)
	h.time += testParams.VotingPeriod
	h.endBlock(1)

	require.Equal(t, StatusFailed, h.status(bad))
	require.Equal(t, StatusPassed, h.status(good), "the good proposal settles in the same EndBlock")
	require.Len(t, h.failures, 1)
	require.ErrorIs(t, h.failures[0], precompiles.ErrDegraded)
	require.ErrorIs(t, h.failures[0], solstorage.ErrInvalidString)
	require.Zero(t, newStore(h.state).votingQueue().Len())
	require.Zero(t, h.state.GetBalance(DepositEscrow).Sign(), "both refunded")
	require.Equal(t, usei(20), h.state.GetBalance(alice))
}

func TestUndecodableContentFailsAsDegraded(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.passing()
	newStore(h.state).SetUint8(at(proposalBase(id), proposalContentType), proposalContentType.offset, 0xff)
	h.time += testParams.VotingPeriod
	h.endBlock(1)

	require.Equal(t, StatusFailed, h.status(id))
	require.Len(t, h.failures, 1)
	require.ErrorIs(t, h.failures[0], precompiles.ErrDegraded)
	require.ErrorIs(t, h.failures[0], errInvalidContent)
}

func TestAFailedProposalStepWritesNothing(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.passing()
	h.corruptContent(id)
	h.time += testParams.VotingPeriod
	before := newStore(h.state).tally(id)
	h.endBlock(1)

	s := newStore(h.state)
	require.Equal(t, StatusFailed, h.status(id))
	require.Equal(t, before, s.tally(id), "the reverted step's counting is undone")
	require.Equal(t, uint64(3), s.votersByProposal(id).Len(), "its votes stay where they were")
	require.Equal(t, [4]string{"0", "0", "0", "0"}, tally(h.proposal(id).FinalTallyResult))
}

func TestEscrowShortfallFailsTheProposalAndKeepsTheDeposits(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.passing()
	require.NoError(t, h.state.SubBalance(DepositEscrow, big.NewInt(1)))
	h.time += testParams.VotingPeriod
	h.endBlock(1)

	require.Equal(t, StatusFailed, h.status(id))
	require.Len(t, h.failures, 2, "the failed step, then the refund that cannot be paid")
	for _, err := range h.failures {
		require.ErrorIs(t, err, precompiles.ErrDegraded)
		require.ErrorIs(t, err, errEscrowShort)
	}
	require.Equal(t, usei(10), newStore(h.state).deposit(id, alice), "the deposit stays recorded")
	require.Zero(t, newStore(h.state).votingQueue().Len())
}

func TestDepositsPastTheTotalAreAnError(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.passing()
	s := newStore(h.state)
	s.setDeposit(id, alice, usei(30)) // more than the proposal's 10
	h.time += testParams.VotingPeriod
	h.endBlock(1)
	require.Equal(t, StatusFailed, h.status(id))
	require.ErrorIs(t, h.failures[0], errDepositOverTotal)
	require.Equal(t, usei(10), h.state.GetBalance(DepositEscrow), "nothing paid from the pool")
}

func TestAProposalThatWillNotLeaveItsQueueIsFailed(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	stuck := h.passing()
	good := h.passing()
	q := newStore(h.state).votingQueue()
	newStore(h.state).Store.SetWord(solstorage.MapWord(solstorage.Offset(solstorage.Slot(slotVotingQueue), 1), q.heap.At(0)), common.Hash{}) // the head's position entry lost
	h.time += testParams.VotingPeriod
	h.endBlock(1)

	require.Equal(t, StatusFailed, h.status(stuck))
	require.ErrorIs(t, h.failures[0], errStuckInQueue)
	require.Equal(t, StatusPassed, h.status(good))
	require.Zero(t, newStore(h.state).votingQueue().Len())
}

func TestACorruptQueueLengthIsReportedAndDrained(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.passing()
	s := newStore(h.state)
	s.Store.SetWord(solstorage.Slot(slotVotingQueue), solstorage.Slot(s.votingQueue().Len()+3))
	h.time += testParams.VotingPeriod
	h.endBlock(1)

	require.ErrorIs(t, h.failures[0], errQueueCorrupt)
	require.Equal(t, StatusPassed, h.status(id), "the queue's real proposal proceeds")
	require.Zero(t, newStore(h.state).votingQueue().Len(), "the entries past the real ones are drained")
}

func TestAHugeCorruptQueueLengthDrainsWithinTheBudget(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.passing()
	newStore(h.state).Store.SetWord(solstorage.Slot(slotVotingQueue), solstorage.Slot(1_000_000))
	h.time += testParams.VotingPeriod
	h.endBlock(1)

	require.ErrorIs(t, h.failures[0], errQueueCorrupt)
	require.Equal(t, StatusPassed, h.status(id))
	require.LessOrEqual(t, len(h.failures), 1+endBlockBudget/failCost)
}

func TestMoreDepositorsThanAllowedFailsTheProposal(t *testing.T) {
	h := newElectedHarness(t, equalElectorate)
	id := h.passing()
	good := h.passing()
	d := newStore(h.state).depositors(id)
	d.Store.SetWord(d.Base, solstorage.Slot(maxDepositors+1))
	h.time += testParams.VotingPeriod
	h.endBlock(1)

	require.Equal(t, StatusFailed, h.status(id))
	require.ErrorIs(t, h.failures[0], errInvalidStoredDeposits)
	require.Equal(t, StatusPassed, h.status(good))
}
