package types

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// leaderKey returns the secret key for the leader of the given view.
func leaderKey(committee *Committee, keys []SecretKey, view View) SecretKey {
	leader := committee.Leader(view)
	for _, k := range keys {
		if k.Public() == leader {
			return k
		}
	}
	panic("leader not in keys")
}

// makeLaneQC produces a block on the given lane and forms a LaneQC from committee votes.
// Returns the LaneQC and the block header it certifies.
func makeLaneQC(
	rng utils.Rng,
	committee *Committee,
	keys []SecretKey,
	lane LaneID,
	blockNum BlockNumber,
	parent BlockHeaderHash,
) (*LaneQC, *BlockHeader) {
	block := NewBlock(lane, blockNum, parent, GenPayload(rng))
	header := block.Header()
	var votes []*Signed[*LaneVote]
	for _, k := range keys[:committee.LaneQuorum()] {
		votes = append(votes, Sign(k, NewLaneVote(header)))
	}
	return NewLaneQC(votes), header
}

// makeCommitQCFromProposal creates a CommitQC for a FullProposal, signed by all keys.
func makeCommitQCFromProposal(keys []SecretKey, fp *FullProposal) *CommitQC {
	vote := NewCommitVote(fp.Proposal().Msg())
	var votes []*Signed[*CommitVote]
	for _, k := range keys {
		votes = append(votes, Sign(k, vote))
	}
	return NewCommitQC(votes)
}

// makeAppQCFor creates an AppQC for the given parameters, signed by all keys.
func makeAppQCFor(keys []SecretKey, globalNum GlobalBlockNumber, roadIdx RoadIndex, appHash AppHash) *AppQC {
	appProposal := NewAppProposal(globalNum, roadIdx, appHash)
	vote := NewAppVote(appProposal)
	var votes []*Signed[*AppVote]
	for _, k := range keys {
		votes = append(votes, Sign(k, vote))
	}
	return NewAppQC(votes)
}

func TestProposalVerifyFreshEmptyRanges(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]()))
	require.NoError(t, fp.Verify(committee, vs))
}

func TestProposalVerifyFreshWithBlocks(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	// Produce a LaneQC for the proposer's lane.
	lane := proposerKey.Public()
	laneQC, _ := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))

	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(),
		map[LaneID]*LaneQC{lane: laneQC}, utils.None[*AppQC]()))
	require.NoError(t, fp.Verify(committee, vs))
}

func TestProposalVerifyRejectsViewMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// Build a valid proposal at genesis view (0, 0).
	vs0 := ViewSpec{}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp := utils.OrPanic1(NewProposal(leader0, committee, vs0, time.Now(), nil, utils.None[*AppQC]()))

	// Verify it against a different ViewSpec (view 1, 0).
	commitQC := makeCommitQCFromProposal(keys, fp)
	vs1 := ViewSpec{CommitQC: utils.Some(commitQC)}
	err := fp.Verify(committee, vs1)
	require.Error(t, err)
}

func TestProposalVerifyRejectsForgedSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	// Build two valid proposals with different timestamps.
	fp1 := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]()))
	fp2 := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now().Add(time.Hour), nil, utils.None[*AppQC]()))

	// Graft fp1's signature onto fp2 (different content).
	fp2.proposal.sig = fp1.proposal.sig
	err := fp2.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsWrongProposer(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	correctLeader := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(correctLeader, committee, vs, time.Now(), nil, utils.None[*AppQC]()))

	// Re-sign the same proposal with a different (non-leader) key.
	var wrongKey SecretKey
	for _, k := range keys {
		if k.Public() != correctLeader.Public() {
			wrongKey = k
			break
		}
	}
	tamperedFP := &FullProposal{
		proposal:  Sign(wrongKey, fp.Proposal().Msg()),
		laneQCs:   fp.laneQCs,
		appQC:     fp.appQC,
		timeoutQC: fp.timeoutQC,
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsInconsistentTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{} // no timeoutQC
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]()))

	// Attach a timeoutQC that the ViewSpec doesn't expect.
	var timeoutVotes []*FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, NewFullTimeoutVote(k, View{Index: 0, Number: 0}, utils.None[*PrepareQC]()))
	}
	tQC := NewTimeoutQC(timeoutVotes)

	tamperedFP := &FullProposal{
		proposal:  fp.proposal,
		laneQCs:   fp.laneQCs,
		appQC:     fp.appQC,
		timeoutQC: utils.Some(tQC),
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsNonCommitteeLane(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]()))

	// Replace one committee lane with a non-committee lane.
	// E.g. committee = {A, B, C, D}, proposal = {A, B, C, X}.
	// LaneRange.Verify rejects X because it's not a committee lane.
	extraLane := GenSecretKey(rng).Public()
	require.False(t, committee.Lanes().Has(extraLane))
	var victim LaneID
	for _, l := range committee.Lanes().All() {
		victim = l
		break
	}

	origProposal := fp.Proposal().Msg()
	var tamperedRanges []*LaneRange
	for _, r := range origProposal.laneRanges {
		if r.Lane() == victim {
			tamperedRanges = append(tamperedRanges, NewLaneRange(extraLane, 0, utils.None[*BlockHeader]()))
		} else {
			tamperedRanges = append(tamperedRanges, r)
		}
	}

	tamperedProposal := newProposal(origProposal.view, origProposal.createdAt, tamperedRanges, origProposal.app)
	maliciousFP := &FullProposal{
		proposal:  Sign(proposerKey, tamperedProposal),
		laneQCs:   fp.laneQCs,
		appQC:     fp.appQC,
		timeoutQC: fp.timeoutQC,
	}
	err := maliciousFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyAcceptsImplicitLaneRange(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]()))

	// Drop one lane — the omitted lane gets an implicit [0, 0) range,
	// which matches the expected first=0 at genesis.
	origP := fp.Proposal().Msg()
	var keptRanges []*LaneRange
	first := true
	for _, r := range origP.laneRanges {
		if first {
			first = false
			continue
		}
		keptRanges = append(keptRanges, r)
	}

	shortProposal := newProposal(origP.view, origP.createdAt, keptRanges, origP.app)
	shortFP := &FullProposal{
		proposal: Sign(proposerKey, shortProposal),
	}
	require.NoError(t, shortFP.Verify(committee, vs))
}

func TestProposalVerifyAcceptsNonContiguousImplicitRanges(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]()))

	// Keep only every other lane (e.g. {A, C} out of {A, B, C, D}).
	origP := fp.Proposal().Msg()
	var keptRanges []*LaneRange
	i := 0
	for _, r := range origP.laneRanges {
		if i%2 == 0 {
			keptRanges = append(keptRanges, r)
		}
		i++
	}

	shortProposal := newProposal(origP.view, origP.createdAt, keptRanges, origP.app)
	shortFP := &FullProposal{
		proposal: Sign(proposerKey, shortProposal),
	}
	require.NoError(t, shortFP.Verify(committee, vs))
}

func TestProposalVerifyRejectsLaneRangeFirstMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]()))

	// Tamper: change one lane's first to 5 (genesis expects 0).
	origP := fp.Proposal().Msg()
	lane := keys[0].Public()
	var tamperedRanges []*LaneRange
	for _, r := range origP.laneRanges {
		if r.Lane() == lane {
			tamperedRanges = append(tamperedRanges, &LaneRange{lane: lane, first: 5, next: 5})
		} else {
			tamperedRanges = append(tamperedRanges, r)
		}
	}
	tamperedProposal := newProposal(origP.view, origP.createdAt, tamperedRanges, origP.app)
	tamperedFP := &FullProposal{
		proposal: Sign(proposerKey, tamperedProposal),
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsMissingLaneQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := keys[0].Public()
	laneQC, _ := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))

	// Build a valid proposal with a block, then strip the laneQC.
	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(),
		map[LaneID]*LaneQC{lane: laneQC}, utils.None[*AppQC]()))

	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		laneQCs:  map[LaneID]*LaneQC{},
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsLaneQCBlockNumberMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := keys[0].Public()

	// Build a valid proposal with a QC certifying block 1 (range [0, 2)).
	goodQC, _ := makeLaneQC(rng, committee, keys, lane, 1, GenBlockHeaderHash(rng))
	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(),
		map[LaneID]*LaneQC{lane: goodQC}, utils.None[*AppQC]()))

	// Swap in a QC certifying block 0 — range expects block 1.
	wrongQC, _ := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		laneQCs:  map[LaneID]*LaneQC{lane: wrongQC},
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsInvalidLaneQCSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := keys[0].Public()
	block := NewBlock(lane, 0, GenBlockHeaderHash(rng), GenPayload(rng))
	header := block.Header()

	// Build a LaneQC signed by NON-committee keys.
	otherKeys := make([]SecretKey, committee.LaneQuorum())
	for i := range otherKeys {
		otherKeys[i] = GenSecretKey(rng)
	}
	var badVotes []*Signed[*LaneVote]
	for _, k := range otherKeys {
		badVotes = append(badVotes, Sign(k, NewLaneVote(header)))
	}
	badLaneQC := NewLaneQC(badVotes)

	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(),
		map[LaneID]*LaneQC{lane: badLaneQC}, utils.None[*AppQC]()))

	err := fp.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsAppProposalLowerThanPrevious(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// First, build index 0 with an AppProposal at RoadIndex 0.
	vs0 := ViewSpec{}
	leader0 := leaderKey(committee, keys, vs0.View())
	lane := leader0.Public()
	laneQC, _ := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	appQC := makeAppQCFor(keys, 0, 0, GenAppHash(rng))
	fp0 := utils.OrPanic1(NewProposal(leader0, committee, vs0, time.Now(),
		map[LaneID]*LaneQC{lane: laneQC}, utils.Some(appQC)))
	commitQC0 := makeCommitQCFromProposal(keys, fp0)

	// Build a valid proposal at view 1, then tamper its app to be lower.
	vs1 := ViewSpec{CommitQC: utils.Some(commitQC0)}
	leader1 := leaderKey(committee, keys, vs1.View())
	fp1 := utils.OrPanic1(NewProposal(leader1, committee, vs1, time.Now(), nil, utils.None[*AppQC]()))

	// Tamper: downgrade app to None (lower than commitQC0's app at RoadIndex 0).
	origP := fp1.Proposal().Msg()
	var ranges []*LaneRange
	for _, r := range origP.laneRanges {
		ranges = append(ranges, r)
	}
	tamperedProposal := newProposal(origP.view, origP.createdAt, ranges, utils.None[*AppProposal]())
	tamperedFP := &FullProposal{
		proposal: Sign(leader1, tamperedProposal),
	}
	err := tamperedFP.Verify(committee, vs1)
	require.Error(t, err)
}

func TestProposalVerifyRejectsUnnecessaryAppQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{} // no previous commitQC, so app starts at None

	leader := leaderKey(committee, keys, vs.View())
	fp := utils.OrPanic1(NewProposal(leader, committee, vs, time.Now(), nil, utils.None[*AppQC]()))

	// Attach an unrequested AppQC.
	appQC := makeAppQCFor(keys, 0, 0, GenAppHash(rng))
	tamperedFP := &FullProposal{
		proposal:  fp.proposal,
		laneQCs:   fp.laneQCs,
		appQC:     utils.Some(appQC),
		timeoutQC: fp.timeoutQC,
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsMissingAppQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{} // no previous commitQC
	leader := leaderKey(committee, keys, vs.View())

	// Build a valid proposal with an AppQC, then strip it.
	goodAppQC := makeAppQCFor(keys, 0, 0, GenAppHash(rng))
	fp := utils.OrPanic1(NewProposal(leader, committee, vs, time.Now(), nil, utils.Some(goodAppQC)))

	tamperedFP := &FullProposal{
		proposal: fp.proposal,
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsAppQCMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	leader := leaderKey(committee, keys, vs.View())

	// Build a valid proposal with an AppQC, then swap in a different one.
	goodAppQC := makeAppQCFor(keys, 0, 0, GenAppHash(rng))
	fp := utils.OrPanic1(NewProposal(leader, committee, vs, time.Now(), nil, utils.Some(goodAppQC)))

	differentAppQC := makeAppQCFor(keys, 0, 0, GenAppHash(rng))
	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		appQC:    utils.Some(differentAppQC),
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsInvalidAppQCSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	leader := leaderKey(committee, keys, vs.View())

	appHash := GenAppHash(rng)
	goodAppQC := makeAppQCFor(keys, 0, 0, appHash)
	fp := utils.OrPanic1(NewProposal(leader, committee, vs, time.Now(), nil, utils.Some(goodAppQC)))

	// Swap in an AppQC signed by NON-committee keys (same hash).
	otherKeys := make([]SecretKey, len(keys))
	for i := range otherKeys {
		otherKeys[i] = GenSecretKey(rng)
	}
	badAppQC := makeAppQCFor(otherKeys, 0, 0, appHash)
	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		appQC:    utils.Some(badAppQC),
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsLaneQCHeaderHashMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := proposerKey.Public()

	// Build a valid proposal with a QC for block 0.
	realQC, _ := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	fp := utils.OrPanic1(NewProposal(proposerKey, committee, vs, time.Now(),
		map[LaneID]*LaneQC{lane: realQC}, utils.None[*AppQC]()))

	// Swap in a different QC for block 0 (different payload → different hash).
	differentQC, _ := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	require.NotEqual(t, realQC.Header().Hash(), differentQC.Header().Hash())

	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		laneQCs:  map[LaneID]*LaneQC{lane: differentQC},
	}
	err := tamperedFP.Verify(committee, vs)
	require.Error(t, err)
}

func TestProposalVerifyValidReproposal(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// First, create a valid proposal at view (0, 0) with a PrepareQC.
	vs0 := ViewSpec{}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0 := utils.OrPanic1(NewProposal(leader0, committee, vs0, time.Now(), nil, utils.None[*AppQC]()))

	// Build a PrepareQC for the proposal at (0, 0).
	var prepareVotes []*Signed[*PrepareVote]
	for _, k := range keys {
		prepareVotes = append(prepareVotes, Sign(k, NewPrepareVote(fp0.Proposal().Msg())))
	}
	prepareQC := NewPrepareQC(prepareVotes)

	// Timeout at view (0, 0) with the PrepareQC → forces reproposal at (0, 1).
	var timeoutVotes []*FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, NewFullTimeoutVote(k, View{Index: 0, Number: 0}, utils.Some(prepareQC)))
	}
	timeoutQC := NewTimeoutQC(timeoutVotes)

	vs1 := ViewSpec{TimeoutQC: utils.Some(timeoutQC)}
	require.Equal(t, View{Index: 0, Number: 1}, vs1.View())

	leader1 := leaderKey(committee, keys, vs1.View())
	reproposal := utils.OrPanic1(NewProposal(leader1, committee, vs1, time.Now(), nil, utils.None[*AppQC]()))

	require.NoError(t, reproposal.Verify(committee, vs1))
}

func TestProposalVerifyRejectsReproposalWithUnnecessaryData(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// Build a PrepareQC at (0, 0).
	vs0 := ViewSpec{}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0 := utils.OrPanic1(NewProposal(leader0, committee, vs0, time.Now(), nil, utils.None[*AppQC]()))

	var prepareVotes []*Signed[*PrepareVote]
	for _, k := range keys {
		prepareVotes = append(prepareVotes, Sign(k, NewPrepareVote(fp0.Proposal().Msg())))
	}
	prepareQC := NewPrepareQC(prepareVotes)

	var timeoutVotes []*FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, NewFullTimeoutVote(k, View{Index: 0, Number: 0}, utils.Some(prepareQC)))
	}
	timeoutQC := NewTimeoutQC(timeoutVotes)

	vs1 := ViewSpec{TimeoutQC: utils.Some(timeoutQC)}
	leader1 := leaderKey(committee, keys, vs1.View())

	// Create a valid reproposal, then tamper it with unnecessary laneQCs.
	reproposal := utils.OrPanic1(NewProposal(leader1, committee, vs1, time.Now(), nil, utils.None[*AppQC]()))

	lane := keys[0].Public()
	laneQC, _ := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	tamperedFP := &FullProposal{
		proposal:  reproposal.proposal,
		laneQCs:   map[LaneID]*LaneQC{lane: laneQC},
		timeoutQC: reproposal.timeoutQC,
	}
	err := tamperedFP.Verify(committee, vs1)
	require.Error(t, err)
}

func TestProposalVerifyRejectsReproposalHashMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// Build a PrepareQC at (0, 0).
	vs0 := ViewSpec{}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0 := utils.OrPanic1(NewProposal(leader0, committee, vs0, time.Now(), nil, utils.None[*AppQC]()))

	var prepareVotes []*Signed[*PrepareVote]
	for _, k := range keys {
		prepareVotes = append(prepareVotes, Sign(k, NewPrepareVote(fp0.Proposal().Msg())))
	}
	prepareQC := NewPrepareQC(prepareVotes)

	var timeoutVotes []*FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, NewFullTimeoutVote(k, View{Index: 0, Number: 0}, utils.Some(prepareQC)))
	}
	timeoutQC := NewTimeoutQC(timeoutVotes)

	vs1 := ViewSpec{TimeoutQC: utils.Some(timeoutQC)}
	leader1 := leaderKey(committee, keys, vs1.View())

	// Build the valid reproposal, then tamper its timestamp to get a different hash.
	reproposal := utils.OrPanic1(NewProposal(leader1, committee, vs1, time.Now(), nil, utils.None[*AppQC]()))

	origP := reproposal.Proposal().Msg()
	var ranges []*LaneRange
	for _, r := range origP.laneRanges {
		ranges = append(ranges, r)
	}
	wrongP := newProposal(origP.view, time.Now().Add(time.Hour), ranges, origP.app)
	wrongFP := &FullProposal{
		proposal:  Sign(leader1, wrongP),
		timeoutQC: reproposal.timeoutQC,
	}
	err := wrongFP.Verify(committee, vs1)
	require.Error(t, err)
}

func TestProposalVerifyRejectsInvalidTimeoutQCSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// Build a TimeoutQC signed by NON-committee keys.
	otherKeys := make([]SecretKey, len(keys))
	for i := range otherKeys {
		otherKeys[i] = GenSecretKey(rng)
	}
	var timeoutVotes []*FullTimeoutVote
	for _, k := range otherKeys {
		timeoutVotes = append(timeoutVotes, NewFullTimeoutVote(k, View{Index: 0, Number: 0}, utils.None[*PrepareQC]()))
	}
	badTimeoutQC := NewTimeoutQC(timeoutVotes)

	vs := ViewSpec{TimeoutQC: utils.Some(badTimeoutQC)}
	leader := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(leader, committee, vs, time.Now(), nil, utils.None[*AppQC]()))

	err := fp.Verify(committee, vs)
	require.Error(t, err)
}
