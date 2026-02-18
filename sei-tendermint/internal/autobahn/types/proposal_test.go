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

func TestVerifyFreshProposalEmptyRanges(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp, err := NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)
	require.NoError(t, fp.Verify(committee, vs))
}

func TestVerifyFreshProposalWithBlocks(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	// Produce a LaneQC for the proposer's lane.
	lane := proposerKey.Public()
	laneQC, _ := makeLaneQC(rng, committee, keys, lane, 0, BlockHeaderHash{})

	fp, err := NewProposal(proposerKey, committee, vs, time.Now(),
		map[LaneID]*LaneQC{lane: laneQC}, utils.None[*AppQC]())
	require.NoError(t, err)
	require.NoError(t, fp.Verify(committee, vs))
}

func TestVerifyRejectsViewMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// Create a proposal at view (5, 0) but verify against genesis ViewSpec (0, 0).
	vs := ViewSpec{}
	otherView := View{Index: 5, Number: 0}
	otherKey := leaderKey(committee, keys, otherView)

	otherProposal := newProposal(otherView, time.Now(), nil, utils.None[*AppProposal]())
	fp := &FullProposal{
		proposal: Sign(otherKey, otherProposal),
	}
	err := fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "view")
}

func TestVerifyRejectsForgedProposalSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	// Build a valid proposal.
	fp, err := NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

	// Tamper: build a different proposal and attach the original signature.
	// The claimed key is still the leader's, but the signature doesn't match.
	differentProposal := newProposal(vs.View(), time.Now().Add(time.Hour), nil, utils.None[*AppProposal]())
	forgedFP := &FullProposal{
		proposal: &Signed[*Proposal]{
			hashed: NewHashed(differentProposal),
			sig:    fp.proposal.sig, // reuse leader's sig from the original proposal
		},
	}
	err = forgedFP.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "proposal signature")
}

func TestVerifyRejectsWrongProposer(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	correctLeader := leaderKey(committee, keys, vs.View())

	fp, err := NewProposal(correctLeader, committee, vs, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

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
	err = tamperedFP.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "proposer")
}

func TestVerifyRejectsInconsistentTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{} // no timeoutQC
	proposerKey := leaderKey(committee, keys, vs.View())

	fp, err := NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

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
	err = tamperedFP.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inconsistent timeoutQC")
}

func TestVerifyRejectsNonCommitteeLane(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp, err := NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

	// Replace one committee lane with a non-committee lane (same count).
	// E.g. committee = {A, B, C, D}, proposal = {A, B, C, X}.
	// The lane count still matches (4 == 4), but Proposal.LaneRange(D)
	// returns a synthetic [0, 0) fallback which would silently pass
	// at genesis without the explicit map lookup.
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
	err = maliciousFP.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing lane range")
}

func TestVerifyRejectsMissingLaneRange(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp, err := NewProposal(proposerKey, committee, vs, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

	// Drop one committee lane from the proposal (fewer lanes than committee).
	origProposal := fp.Proposal().Msg()
	var tamperedRanges []*LaneRange
	first := true
	for _, r := range origProposal.laneRanges {
		if first {
			first = false
			continue
		}
		tamperedRanges = append(tamperedRanges, r)
	}

	tamperedProposal := newProposal(origProposal.view, origProposal.createdAt, tamperedRanges, origProposal.app)
	maliciousFP := &FullProposal{
		proposal:  Sign(proposerKey, tamperedProposal),
		laneQCs:   fp.laneQCs,
		appQC:     fp.appQC,
		timeoutQC: fp.timeoutQC,
	}
	err = maliciousFP.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lanes")
}

func TestVerifyRejectsLaneRangeFirstMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	// Build a proposal where a lane starts at block 5 instead of 0 (genesis expects 0).
	lane := keys[0].Public()
	tamperedRange := &LaneRange{lane: lane, first: 5, next: 5}

	var allRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		if l == lane {
			allRanges = append(allRanges, tamperedRange)
		} else {
			allRanges = append(allRanges, NewLaneRange(l, 0, utils.None[*BlockHeader]()))
		}
	}
	tamperedProposal := newProposal(vs.View(), time.Now(), allRanges, utils.None[*AppProposal]())
	fp := &FullProposal{
		proposal: Sign(proposerKey, tamperedProposal),
	}
	err := fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "First()")
}

func TestVerifyRejectsMissingLaneQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := keys[0].Public()
	header := NewBlock(lane, 0, BlockHeaderHash{}, GenPayload(rng)).Header()

	// Create proposal with a non-empty range but NO laneQC.
	var allRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		if l == lane {
			allRanges = append(allRanges, NewLaneRange(lane, 0, utils.Some(header)))
		} else {
			allRanges = append(allRanges, NewLaneRange(l, 0, utils.None[*BlockHeader]()))
		}
	}
	tamperedProposal := newProposal(vs.View(), time.Now(), allRanges, utils.None[*AppProposal]())
	fp := &FullProposal{
		proposal: Sign(proposerKey, tamperedProposal),
		laneQCs:  map[LaneID]*LaneQC{}, // empty — missing the required QC
	}
	err := fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing qc")
}

func TestVerifyRejectsLaneQCBlockNumberMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := keys[0].Public()

	// Build a LaneQC for block 0.
	laneQC, _ := makeLaneQC(rng, committee, keys, lane, 0, BlockHeaderHash{})

	// But create a proposal claiming blocks 0..2 (next=2), so it expects QC for block 1.
	block1 := NewBlock(lane, 1, BlockHeaderHash{}, GenPayload(rng))
	var allRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		if l == lane {
			allRanges = append(allRanges, NewLaneRange(lane, 0, utils.Some(block1.Header())))
		} else {
			allRanges = append(allRanges, NewLaneRange(l, 0, utils.None[*BlockHeader]()))
		}
	}
	tamperedProposal := newProposal(vs.View(), time.Now(), allRanges, utils.None[*AppProposal]())
	fp := &FullProposal{
		proposal: Sign(proposerKey, tamperedProposal),
		laneQCs:  map[LaneID]*LaneQC{lane: laneQC}, // QC for block 0, but range says next=2 (wants block 1)
	}
	err := fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "BlockNumber()")
}

func TestVerifyRejectsInvalidLaneQCSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := keys[0].Public()
	block := NewBlock(lane, 0, BlockHeaderHash{}, GenPayload(rng))
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

	fp, err := NewProposal(proposerKey, committee, vs, time.Now(),
		map[LaneID]*LaneQC{lane: badLaneQC}, utils.None[*AppQC]())
	require.NoError(t, err)

	err = fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "qc[")
}

func TestVerifyRejectsAppProposalLowerThanPrevious(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// First, build index 0 with an AppProposal at RoadIndex 0.
	vs0 := ViewSpec{}
	leader0 := leaderKey(committee, keys, vs0.View())
	lane := leader0.Public()
	laneQC, _ := makeLaneQC(rng, committee, keys, lane, 0, BlockHeaderHash{})
	appQC := makeAppQCFor(keys, 0, 0, GenAppHash(rng))
	fp0, err := NewProposal(leader0, committee, vs0, time.Now(),
		map[LaneID]*LaneQC{lane: laneQC}, utils.Some(appQC))
	require.NoError(t, err)
	commitQC0 := makeCommitQCFromProposal(keys, fp0)

	// Now build index 1 with a tampered AppProposal that is LOWER than what commitQC0 carries.
	vs1 := ViewSpec{CommitQC: utils.Some(commitQC0)}
	leader1 := leaderKey(committee, keys, vs1.View())

	// Build the lane ranges correctly for index 1.
	var allRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		first := LaneRangeOpt(vs1.CommitQC, l).Next()
		allRanges = append(allRanges, NewLaneRange(l, first, utils.None[*BlockHeader]()))
	}

	// Set app to None — this is lower than what commitQC0 carried (RoadIndex 0).
	tamperedProposal := newProposal(vs1.View(), time.Now(), allRanges, utils.None[*AppProposal]())
	fp1 := &FullProposal{
		proposal: Sign(leader1, tamperedProposal),
	}
	err = fp1.Verify(committee, vs1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "AppProposal lower")
}

func TestVerifyRejectsUnnecessaryAppQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{} // no previous commitQC, so app starts at None

	leader := leaderKey(committee, keys, vs.View())
	fp, err := NewProposal(leader, committee, vs, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

	// Attach an unrequested AppQC.
	appQC := makeAppQCFor(keys, 0, 0, GenAppHash(rng))
	tamperedFP := &FullProposal{
		proposal:  fp.proposal,
		laneQCs:   fp.laneQCs,
		appQC:     utils.Some(appQC),
		timeoutQC: fp.timeoutQC,
	}
	err = tamperedFP.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unnecessary appQC")
}

func TestVerifyRejectsMissingAppQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{} // no previous commitQC
	leader := leaderKey(committee, keys, vs.View())

	// Build lane ranges with a new AppProposal but no AppQC to justify it.
	var allRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		allRanges = append(allRanges, NewLaneRange(l, 0, utils.None[*BlockHeader]()))
	}
	// Include an AppProposal (advancing from None to Some).
	appProposal := NewAppProposal(0, 0, GenAppHash(rng))
	tamperedProposal := newProposal(vs.View(), time.Now(), allRanges, utils.Some(appProposal))
	fp := &FullProposal{
		proposal: Sign(leader, tamperedProposal),
		// No appQC — should fail.
	}
	err := fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "appQC missing")
}

func TestVerifyRejectsAppQCMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	leader := leaderKey(committee, keys, vs.View())

	// Build an AppQC for a DIFFERENT AppProposal than what the proposal carries.
	var allRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		allRanges = append(allRanges, NewLaneRange(l, 0, utils.None[*BlockHeader]()))
	}
	appProposal := NewAppProposal(0, 0, GenAppHash(rng))
	differentAppQC := makeAppQCFor(keys, 0, 0, GenAppHash(rng)) // different hash

	tamperedProposal := newProposal(vs.View(), time.Now(), allRanges, utils.Some(appProposal))
	fp := &FullProposal{
		proposal: Sign(leader, tamperedProposal),
		appQC:    utils.Some(differentAppQC),
	}
	err := fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "appQC doesn't match")
}

func TestVerifyRejectsInvalidAppQCSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	leader := leaderKey(committee, keys, vs.View())

	appHash := GenAppHash(rng)
	appProposal := NewAppProposal(0, 0, appHash)

	// Build AppQC with NON-committee keys.
	otherKeys := make([]SecretKey, len(keys))
	for i := range otherKeys {
		otherKeys[i] = GenSecretKey(rng)
	}
	badAppQC := makeAppQCFor(otherKeys, 0, 0, appHash)

	var allRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		allRanges = append(allRanges, NewLaneRange(l, 0, utils.None[*BlockHeader]()))
	}
	tamperedProposal := newProposal(vs.View(), time.Now(), allRanges, utils.Some(appProposal))
	fp := &FullProposal{
		proposal: Sign(leader, tamperedProposal),
		appQC:    utils.Some(badAppQC),
	}
	err := fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "appQC")
}

func TestVerifyRejectsLaneQCHeaderHashMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	vs := ViewSpec{}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := proposerKey.Public()

	// Build a valid LaneQC for block 0 with header H1.
	realQC, realHeader := makeLaneQC(rng, committee, keys, lane, 0, BlockHeaderHash{})

	// Build a DIFFERENT block 0 on the same lane → header H2 with a different payload hash.
	differentBlock := NewBlock(lane, 0, BlockHeaderHash{}, GenPayload(rng))
	differentHeader := differentBlock.Header()

	// Sanity: the two headers have the same BlockNumber but different hashes.
	require.Equal(t, realHeader.BlockNumber(), differentHeader.BlockNumber())
	require.NotEqual(t, realHeader.Hash(), differentHeader.Hash())

	// Construct a LaneRange using the DIFFERENT header (LastHash = H2).
	var allRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		if l == lane {
			allRanges = append(allRanges, NewLaneRange(lane, 0, utils.Some(differentHeader)))
		} else {
			allRanges = append(allRanges, NewLaneRange(l, 0, utils.None[*BlockHeader]()))
		}
	}

	tamperedProposal := newProposal(vs.View(), time.Now(), allRanges, utils.None[*AppProposal]())
	fp := &FullProposal{
		proposal: Sign(proposerKey, tamperedProposal),
		laneQCs:  map[LaneID]*LaneQC{lane: realQC}, // QC signs H1, but proposal says H2
	}

	// Confirm the mismatch exists.
	lr := fp.Proposal().Msg().LaneRange(lane)
	require.NotEqual(t, realQC.Header().Hash(), lr.LastHash(),
		"LaneQC header hash should differ from LaneRange.LastHash()")

	err := fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Hash()")
}

func TestVerifyValidReproposal(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// First, create a valid proposal at view (0, 0) with a PrepareQC.
	vs0 := ViewSpec{}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0, err := NewProposal(leader0, committee, vs0, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

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
	reproposal, err := NewProposal(leader1, committee, vs1, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

	require.NoError(t, reproposal.Verify(committee, vs1))
}

func TestVerifyRejectsReproposalWithUnnecessaryData(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// Build a PrepareQC at (0, 0).
	vs0 := ViewSpec{}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0, err := NewProposal(leader0, committee, vs0, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

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
	reproposal, err := NewProposal(leader1, committee, vs1, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

	lane := keys[0].Public()
	laneQC, _ := makeLaneQC(rng, committee, keys, lane, 0, BlockHeaderHash{})
	tamperedFP := &FullProposal{
		proposal:  reproposal.proposal,
		laneQCs:   map[LaneID]*LaneQC{lane: laneQC},
		timeoutQC: reproposal.timeoutQC,
	}
	err = tamperedFP.Verify(committee, vs1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unnecessary data")
}

func TestVerifyRejectsReproposalHashMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// Build a PrepareQC at (0, 0).
	vs0 := ViewSpec{}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0, err := NewProposal(leader0, committee, vs0, time.Now(), nil, utils.None[*AppQC]())
	require.NoError(t, err)

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

	// Build a DIFFERENT proposal at view (0, 1) instead of reproposing the PrepareQC's proposal.
	var wrongRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		wrongRanges = append(wrongRanges, NewLaneRange(l, 0, utils.None[*BlockHeader]()))
	}
	// Use a different timestamp to get a different hash.
	wrongProposal := newProposal(vs1.View(), time.Now().Add(time.Hour), wrongRanges, utils.None[*AppProposal]())
	wrongFP := &FullProposal{
		proposal:  Sign(leader1, wrongProposal),
		timeoutQC: utils.Some(timeoutQC),
	}
	err = wrongFP.Verify(committee, vs1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reproposal")
}

func TestVerifyRejectsInvalidTimeoutQCSignature(t *testing.T) {
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

	// The NewProposal path with timeoutQC but no prepareQC means no reproposal —
	// it will try to create a fresh proposal. But Verify will fail on timeoutQC verification.
	var allRanges []*LaneRange
	for _, l := range committee.Lanes().All() {
		allRanges = append(allRanges, NewLaneRange(l, 0, utils.None[*BlockHeader]()))
	}
	proposal := newProposal(vs.View(), time.Now(), allRanges, utils.None[*AppProposal]())
	fp := &FullProposal{
		proposal:  Sign(leader, proposal),
		timeoutQC: utils.Some(badTimeoutQC),
	}
	err := fp.Verify(committee, vs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeoutQC")
}
