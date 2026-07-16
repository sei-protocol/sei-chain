package types

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// genFreshEpoch returns an epoch whose road range is OpenRoadRange (so road index 0
// is always valid for a ViewSpec with no CommitQC) but whose epoch index and first
// block are randomised to prevent tests from silently passing on zero-value defaults.
func genFreshEpoch(rng utils.Rng, committee *Committee) *Epoch {
	return NewEpoch(
		GenEpochIndex(rng),
		OpenRoadRange(),
		time.Time{},
		committee,
		GlobalBlockNumber(rng.Uint64()%1000000)+1,
	)
}

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
) *LaneQC {
	v := NewLaneVote(NewBlock(lane, blockNum, parent, GenPayload(rng)).Header())
	var votes []*Signed[*LaneVote]
	for _, k := range TestKeysWithWeight(committee, keys, committee.LaneQuorum()) {
		votes = append(votes, Sign(k, v))
	}
	return NewLaneQC(votes)
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
func makeAppQCFor(keys []SecretKey, globalNum GlobalBlockNumber, roadIdx RoadIndex, appHash AppHash, epochIdx EpochIndex) *AppQC {
	appProposal := NewAppProposal(globalNum, roadIdx, appHash, epochIdx)
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
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(), nil, utils.None[*AppQC]()))
	require.NoError(t, fp.Verify(vs))
}

func TestProposalVerifyFreshWithBlocks(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	// Produce a LaneQC for the proposer's lane.
	lane := proposerKey.Public()
	laneQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(),
		map[LaneID]*LaneQC{lane: laneQC}, utils.None[*AppQC]()))
	require.NoError(t, fp.Verify(vs))
}

func TestNewProposalRejectsLaneRangeLongerThanMaxLaneRangeInProposal(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())
	lane := proposerKey.Public()

	laneQC := makeLaneQC(rng, committee, keys, lane, MaxLaneRangeInProposal, GenBlockHeaderHash(rng))
	_, err := NewProposal(
		proposerKey,
		vs,
		time.Now(),
		map[LaneID]*LaneQC{lane: laneQC},
		utils.None[*AppQC](),
	)
	require.Error(t, err)
}

func TestProposalBlockTimestampStrictlyMonotone(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	firstBlock := ep.FirstBlock()
	vs0 := ViewSpec{Epoch: ep}
	proposer0 := leaderKey(committee, keys, vs0.View())
	lane := proposer0.Public()

	firstProposal := utils.OrPanic1(NewProposal(
		proposer0,
		vs0, time.Now(),
		map[LaneID]*LaneQC{
			lane: makeLaneQC(rng, committee, keys, lane, 2, GenBlockHeaderHash(rng)),
		},
		utils.None[*AppQC](),
	))
	p0 := firstProposal.Proposal().Msg()
	gr0 := p0.GlobalRange()
	require.Equal(t, firstBlock, gr0.First)
	require.Equal(t, firstBlock+3, gr0.Next)
	first0 := p0.BlockTimestamp(gr0.First).OrPanic("missing first block timestamp")
	second0 := p0.BlockTimestamp(gr0.First + 1).OrPanic("missing second block timestamp")
	third0 := p0.BlockTimestamp(gr0.First + 2).OrPanic("missing third block timestamp")
	require.True(t, first0.Before(second0), "block timestamps within one proposal must be strictly increasing")
	require.True(t, second0.Before(third0), "block timestamps within one proposal must be strictly increasing")

	commitQC0 := makeCommitQCFromProposal(keys, firstProposal)
	vs1 := ViewSpec{CommitQC: utils.Some(commitQC0), Epoch: ep}
	proposer1 := leaderKey(committee, keys, vs1.View())

	secondProposal := utils.OrPanic1(NewProposal(
		proposer1,
		vs1, time.Now(),
		map[LaneID]*LaneQC{
			lane: makeLaneQC(rng, committee, keys, lane, 3, GenBlockHeaderHash(rng)),
		},
		utils.None[*AppQC](),
	))
	p1 := secondProposal.Proposal().Msg()
	gr1 := p1.GlobalRange()
	require.Equal(t, gr0.Next, gr1.First)
	last0 := p0.BlockTimestamp(gr0.Next - 1).OrPanic("missing last block timestamp")
	first1 := p1.BlockTimestamp(gr1.First).OrPanic("missing first timestamp of next proposal")
	require.True(t, last0.Before(first1), "block timestamps across consecutive proposals must be strictly increasing")
}

func TestProposalVerifyRejectsNonMonotoneTimestamp(t *testing.T) {
	t.Run("wrt genesis timestamp", func(t *testing.T) {
		rng := utils.TestRng()
		committee, keys := GenCommittee(rng, 4)
		genesisTimestamp := time.Now()
		ep := NewEpoch(GenEpochIndex(rng), OpenRoadRange(), genesisTimestamp, committee, GlobalBlockNumber(rng.Uint64()%1000000)+1)
		vs := ViewSpec{Epoch: ep}
		k := leaderKey(committee, keys, vs.View())
		fp := utils.OrPanic1(NewProposal(k, vs, genesisTimestamp, nil, utils.None[*AppQC]()))
		require.NoError(t, fp.Verify(vs))

		vsLater := vs
		vsLater.Epoch = NewEpoch(ep.EpochIndex(), ep.RoadRange(), fp.Proposal().Msg().Timestamp().Add(time.Nanosecond), committee, ep.FirstBlock())
		require.Error(t, fp.Verify(vsLater))
	})

	t.Run("wrt previous proposal", func(t *testing.T) {
		rng := utils.TestRng()
		committee, keys := GenCommittee(rng, 4)
		ep := genFreshEpoch(rng, committee)
		vs0 := ViewSpec{Epoch: ep}
		proposer0 := leaderKey(committee, keys, vs0.View())
		lane := proposer0.Public()
		lQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))

		fp0a := utils.OrPanic1(NewProposal(
			proposer0,
			vs0, time.Now(),
			map[LaneID]*LaneQC{lane: lQC},
			utils.None[*AppQC](),
		))
		fp0b := utils.OrPanic1(NewProposal(
			proposer0,
			vs0, fp0a.Proposal().Msg().NextTimestamp().Add(time.Hour),
			map[LaneID]*LaneQC{lane: lQC},
			utils.None[*AppQC](),
		))

		vs1a := ViewSpec{CommitQC: utils.Some(makeCommitQCFromProposal(keys, fp0a)), Epoch: ep}
		vs1b := ViewSpec{CommitQC: utils.Some(makeCommitQCFromProposal(keys, fp0b)), Epoch: ep}
		proposer1 := leaderKey(committee, keys, vs1a.View())

		fp1a := utils.OrPanic1(NewProposal(
			proposer1,
			vs1a, fp0a.Proposal().Msg().NextTimestamp(),
			nil,
			utils.None[*AppQC](),
		))

		require.NoError(t, fp1a.Verify(vs1a))
		require.Error(t, fp1a.Verify(vs1b))
	})
}

func TestProposalVerifyRejectsViewMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)

	// Build a valid proposal at genesis view (0, 0).
	vs0 := ViewSpec{Epoch: ep}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp := utils.OrPanic1(NewProposal(leader0, vs0, time.Now(), nil, utils.None[*AppQC]()))

	// Verify it against a different ViewSpec (view 1, 0).
	commitQC := makeCommitQCFromProposal(keys, fp)
	vs1 := ViewSpec{CommitQC: utils.Some(commitQC), Epoch: ep}
	err := fp.Verify(vs1)
	require.Error(t, err)
}

func TestProposalVerifyRejectsForgedSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	// Build two valid proposals with different timestamps.
	fp1 := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(), nil, utils.None[*AppQC]()))
	fp2 := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now().Add(time.Hour), nil, utils.None[*AppQC]()))

	// Graft fp1's signature onto fp2 (different content).
	fp2.proposal.sig = fp1.proposal.sig
	err := fp2.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsWrongProposer(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	correctLeader := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(correctLeader, vs, time.Now(), nil, utils.None[*AppQC]()))

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
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsInconsistentTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep} // no timeoutQC
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(), nil, utils.None[*AppQC]()))

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
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsNonCommitteeLane(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(), nil, utils.None[*AppQC]()))

	// Replace one committee lane with a non-committee lane.
	// E.g. committee = {A, B, C, D}, proposal = {A, B, C, X}.
	// LaneRange.Verify rejects X because it's not a committee lane.
	extraLane := GenSecretKey(rng).Public()
	require.False(t, committee.HasLane(extraLane))
	var victim LaneID
	for l := range committee.Lanes().All() {
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

	tamperedProposal := newProposal(origProposal.view, origProposal.timestamp, tamperedRanges, origProposal.app, origProposal.GlobalRange().First)
	maliciousFP := &FullProposal{
		proposal:  Sign(proposerKey, tamperedProposal),
		laneQCs:   fp.laneQCs,
		appQC:     fp.appQC,
		timeoutQC: fp.timeoutQC,
	}
	err := maliciousFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyAcceptsImplicitLaneRange(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(), nil, utils.None[*AppQC]()))

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

	shortProposal := newProposal(origP.view, origP.timestamp, keptRanges, origP.app, origP.GlobalRange().First)
	shortFP := &FullProposal{
		proposal: Sign(proposerKey, shortProposal),
	}
	require.NoError(t, shortFP.Verify(vs))
}

func TestProposalVerifyAcceptsNonContiguousImplicitRanges(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(), nil, utils.None[*AppQC]()))

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

	shortProposal := newProposal(origP.view, origP.timestamp, keptRanges, origP.app, origP.GlobalRange().First)
	shortFP := &FullProposal{
		proposal: Sign(proposerKey, shortProposal),
	}
	require.NoError(t, shortFP.Verify(vs))
}

func TestProposalVerifyRejectsLaneRangeFirstMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(), nil, utils.None[*AppQC]()))

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
	tamperedProposal := newProposal(origP.view, origP.timestamp, tamperedRanges, origP.app, origP.GlobalRange().First)
	tamperedFP := &FullProposal{
		proposal: Sign(proposerKey, tamperedProposal),
	}
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsMissingLaneQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := keys[0].Public()
	laneQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))

	// Build a valid proposal with a block, then strip the laneQC.
	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(),
		map[LaneID]*LaneQC{lane: laneQC}, utils.None[*AppQC]()))

	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		laneQCs:  map[LaneID]*LaneQC{},
	}
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsLaneQCBlockNumberMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := keys[0].Public()

	// Build a valid proposal with a QC certifying block 1 (range [0, 2)).
	goodQC := makeLaneQC(rng, committee, keys, lane, 1, GenBlockHeaderHash(rng))
	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(),
		map[LaneID]*LaneQC{lane: goodQC}, utils.None[*AppQC]()))

	// Swap in a QC certifying block 0 — range expects block 1.
	wrongQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		laneQCs:  map[LaneID]*LaneQC{lane: wrongQC},
	}
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsInvalidLaneQCSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := keys[0].Public()
	block := NewBlock(lane, 0, GenBlockHeaderHash(rng), GenPayload(rng))
	header := block.Header()

	// Build a LaneQC signed by NON-committee keys.
	otherKeys := make([]SecretKey, len(TestKeysWithWeight(committee, keys, committee.LaneQuorum())))
	for i := range otherKeys {
		otherKeys[i] = GenSecretKey(rng)
	}
	var badVotes []*Signed[*LaneVote]
	for _, k := range otherKeys {
		badVotes = append(badVotes, Sign(k, NewLaneVote(header)))
	}
	badLaneQC := NewLaneQC(badVotes)

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(),
		map[LaneID]*LaneQC{lane: badLaneQC}, utils.None[*AppQC]()))

	err := fp.Verify(vs)
	require.Error(t, err)
}

func TestProposalConvDecode_RejectsDuplicateLaneRanges(t *testing.T) {
	rng := utils.TestRng()
	encoded := ProposalConv.Encode(GenProposal(rng))
	_, err := ProposalConv.Decode(encoded)
	require.NoError(t, err)
	// Add a duplicate lane range. Now decoding should fail.
	require.NotEqual(t, 0, len(encoded.LaneRanges))
	encoded.LaneRanges = append(encoded.LaneRanges, encoded.LaneRanges[0])
	_, err = ProposalConv.Decode(encoded)
	require.Error(t, err)
}

func TestProposalVerifyRejectsLaneRangeLongerThanMaxLaneRangeInProposal(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	lane := leaderKey(committee, keys, View{}).Public()
	// Bypass NewProposal's check by constructing the proposal directly.
	oversized := newProposal(
		View{},
		time.Now(),
		[]*LaneRange{NewLaneRange(lane, 0, utils.Some(NewBlock(lane, MaxLaneRangeInProposal, GenBlockHeaderHash(rng), GenPayload(rng)).Header()))},
		utils.None[*AppProposal](),
		ep.FirstBlock(),
	)
	require.Error(t, oversized.Verify(ep))
}

func makeFullProposal(
	ep *Epoch,
	keys []SecretKey,
	prev utils.Option[*CommitQC],
	laneQCs map[LaneID]*LaneQC,
	appQC utils.Option[*AppQC],
) *FullProposal {
	committee := ep.Committee()
	vs := ViewSpec{CommitQC: prev, Epoch: ep}
	return utils.OrPanic1(NewProposal(
		leaderKey(committee, keys, vs.View()),
		vs, time.Now(),
		laneQCs,
		appQC,
	))
}

func makeCommitQC(keys []SecretKey, fullProposal *FullProposal) *CommitQC {
	vote := NewCommitVote(fullProposal.Proposal().Msg())
	var votes []*Signed[*CommitVote]
	for _, k := range keys {
		votes = append(votes, Sign(k, vote))
	}
	return NewCommitQC(votes)
}

func TestProposalVerifyRejectsAppProposalLowerThanPrevious(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)

	// Construct commitQC for index 1 with AppProposal
	// and Proposal for index 2 without any app proposal.
	// Such a proposal should fail validation, because app proposals need to be monotone.
	l := keys[0].Public()
	lQCs := map[LaneID]*LaneQC{l: makeLaneQC(rng, committee, keys, l, 0, GenBlockHeaderHash(rng))}
	commitQC0 := makeCommitQC(keys, makeFullProposal(ep, keys, utils.None[*CommitQC](), lQCs, utils.None[*AppQC]()))
	appQC0 := makeAppQCFor(keys, commitQC0.GlobalRange().First, 0, GenAppHash(rng), ep.EpochIndex())
	commitQC1a := makeCommitQC(keys, makeFullProposal(ep, keys, utils.Some(commitQC0), nil, utils.Some(appQC0)))
	commitQC1b := makeCommitQC(keys, makeFullProposal(ep, keys, utils.Some(commitQC0), nil, utils.None[*AppQC]()))
	fp2a := makeFullProposal(ep, keys, utils.Some(commitQC1a), nil, utils.None[*AppQC]())
	fp2b := makeFullProposal(ep, keys, utils.Some(commitQC1b), nil, utils.None[*AppQC]())

	// We construct the invalid proposal by constructing 2 alternative futures: one with appQC, one without.
	vs := ViewSpec{CommitQC: utils.Some(commitQC1a), Epoch: ep}
	require.NoError(t, fp2a.Verify(vs))
	require.Error(t, fp2b.Verify(vs))
}

func TestProposalVerifyRejectsUnnecessaryAppQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep} // no previous commitQC, so app starts at None

	leader := leaderKey(committee, keys, vs.View())
	fp := utils.OrPanic1(NewProposal(leader, vs, time.Now(), nil, utils.None[*AppQC]()))

	// Attach an unrequested AppQC.
	appQC := makeAppQCFor(keys, ep.FirstBlock(), 0, GenAppHash(rng), ep.EpochIndex())
	tamperedFP := &FullProposal{
		proposal:  fp.proposal,
		laneQCs:   fp.laneQCs,
		appQC:     utils.Some(appQC),
		timeoutQC: fp.timeoutQC,
	}
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsMissingAppQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee) // firstBlock >= 1, so firstBlock-1 is valid
	vs := ViewSpec{Epoch: ep}           // no previous commitQC
	leader := leaderKey(committee, keys, vs.View())

	// Build a valid proposal with an AppQC, then strip it.
	goodAppQC := makeAppQCFor(keys, ep.FirstBlock()-1, 0, GenAppHash(rng), ep.EpochIndex())
	fp := utils.OrPanic1(NewProposal(leader, vs, time.Now(), nil, utils.Some(goodAppQC)))

	tamperedFP := &FullProposal{
		proposal: fp.proposal,
	}
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsAppQCMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	leader := leaderKey(committee, keys, vs.View())

	// Build a valid proposal with an AppQC, then swap in a different one.
	goodAppQC := makeAppQCFor(keys, ep.FirstBlock(), 0, GenAppHash(rng), ep.EpochIndex())
	fp := utils.OrPanic1(NewProposal(leader, vs, time.Now(), nil, utils.Some(goodAppQC)))

	differentAppQC := makeAppQCFor(keys, ep.FirstBlock(), 0, GenAppHash(rng), ep.EpochIndex())
	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		appQC:    utils.Some(differentAppQC),
	}
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsAppProposalWrongEpoch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)

	// firstBlock=1 so NextGlobalBlock()=1 and globalNumber=0 is a valid app target.
	vs := ViewSpec{Epoch: NewEpoch(0, OpenRoadRange(), time.Time{}, committee, 1)}
	leader := leaderKey(committee, keys, vs.View())

	makeAppQCWithEpoch := func(epochIdx EpochIndex) *AppQC {
		p := NewAppProposal(0, 0, GenAppHash(rng), epochIdx)
		v := NewAppVote(p)
		var votes []*Signed[*AppVote]
		for _, k := range keys {
			votes = append(votes, Sign(k, v))
		}
		return NewAppQC(votes)
	}

	// app epoch matches proposal epoch — accepted.
	fp := utils.OrPanic1(NewProposal(leader, vs, time.Now(), nil, utils.Some(makeAppQCWithEpoch(0))))
	require.NoError(t, fp.Verify(vs))

	// app epoch differs from proposal epoch — rejected.
	fpWrong := utils.OrPanic1(NewProposal(leader, vs, time.Now(), nil, utils.Some(makeAppQCWithEpoch(1))))
	require.Error(t, fpWrong.Verify(vs))
}

func TestProposalVerifyRejectsInvalidAppQCSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	leader := leaderKey(committee, keys, vs.View())

	appHash := GenAppHash(rng)
	goodAppQC := makeAppQCFor(keys, ep.FirstBlock(), 0, appHash, ep.EpochIndex())
	fp := utils.OrPanic1(NewProposal(leader, vs, time.Now(), nil, utils.Some(goodAppQC)))

	// Swap in an AppQC signed by NON-committee keys (same hash).
	otherKeys := make([]SecretKey, len(keys))
	for i := range otherKeys {
		otherKeys[i] = GenSecretKey(rng)
	}
	badAppQC := makeAppQCFor(otherKeys, ep.FirstBlock(), 0, appHash, ep.EpochIndex())
	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		appQC:    utils.Some(badAppQC),
	}
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyRejectsLaneQCHeaderHashMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{Epoch: ep}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := proposerKey.Public()

	// Build a valid proposal with a QC for block 0.
	realQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	fp := utils.OrPanic1(NewProposal(proposerKey, vs, time.Now(),
		map[LaneID]*LaneQC{lane: realQC}, utils.None[*AppQC]()))

	// Swap in a different QC for block 0 (different payload → different hash).
	differentQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	require.NotEqual(t, realQC.Header().Hash(), differentQC.Header().Hash())

	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		laneQCs:  map[LaneID]*LaneQC{lane: differentQC},
	}
	err := tamperedFP.Verify(vs)
	require.Error(t, err)
}

func TestProposalVerifyValidReproposal(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	// Build a proposal at view (0, 0) with one lane block so sum(lane.First) > 0.
	// firstBlock > 0 ensures a reproposal bug that passes GlobalRange().First
	// (= sum(lane.First)+firstBlock) instead of firstBlock would be caught.
	ep := genFreshEpoch(rng, committee)
	vs0 := ViewSpec{Epoch: ep}
	leader0 := leaderKey(committee, keys, vs0.View())
	lane := committee.Leader(vs0.View())
	laneQC0 := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	fp0 := utils.OrPanic1(NewProposal(leader0, vs0, time.Now(),
		map[LaneID]*LaneQC{lane: laneQC0}, utils.None[*AppQC]()))

	// Build a PrepareQC for the proposal at (0, 0).
	var prepareVotes []*Signed[*PrepareVote]
	for _, k := range keys {
		prepareVotes = append(prepareVotes, Sign(k, NewPrepareVote(fp0.Proposal().Msg())))
	}
	prepareQC := NewPrepareQC(prepareVotes)

	// Timeout at view (0, 0) with the PrepareQC → forces reproposal at (0, 1).
	var timeoutVotes []*FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, NewFullTimeoutVote(k, View{Index: 0, Number: 0, EpochIndex: ep.EpochIndex()}, utils.Some(prepareQC)))
	}
	timeoutQC := NewTimeoutQC(timeoutVotes)

	vs1 := ViewSpec{TimeoutQC: utils.Some(timeoutQC), Epoch: ep}
	require.Equal(t, View{Index: 0, Number: 1, EpochIndex: ep.EpochIndex()}, vs1.View())

	leader1 := leaderKey(committee, keys, vs1.View())
	reproposal := utils.OrPanic1(NewProposal(leader1, vs1, time.Now(), nil, utils.None[*AppQC]()))

	// Reproposal must carry the same GlobalRange as the original.
	require.Equal(t, fp0.Proposal().Msg().GlobalRange(), reproposal.Proposal().Msg().GlobalRange())
	require.NoError(t, reproposal.Verify(vs1))
}

func TestProposalVerifyRejectsReproposalWithUnnecessaryData(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)

	// Build a PrepareQC at (0, 0).
	vs0 := ViewSpec{Epoch: ep}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0 := utils.OrPanic1(NewProposal(leader0, vs0, time.Now(), nil, utils.None[*AppQC]()))

	var prepareVotes []*Signed[*PrepareVote]
	for _, k := range keys {
		prepareVotes = append(prepareVotes, Sign(k, NewPrepareVote(fp0.Proposal().Msg())))
	}
	prepareQC := NewPrepareQC(prepareVotes)

	var timeoutVotes []*FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, NewFullTimeoutVote(k, View{Index: 0, Number: 0, EpochIndex: ep.EpochIndex()}, utils.Some(prepareQC)))
	}
	timeoutQC := NewTimeoutQC(timeoutVotes)

	vs1 := ViewSpec{TimeoutQC: utils.Some(timeoutQC), Epoch: ep}
	leader1 := leaderKey(committee, keys, vs1.View())

	// Create a valid reproposal, then tamper it with unnecessary laneQCs.
	reproposal := utils.OrPanic1(NewProposal(leader1, vs1, time.Now(), nil, utils.None[*AppQC]()))

	lane := keys[0].Public()
	laneQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	tamperedFP := &FullProposal{
		proposal:  reproposal.proposal,
		laneQCs:   map[LaneID]*LaneQC{lane: laneQC},
		timeoutQC: reproposal.timeoutQC,
	}
	err := tamperedFP.Verify(vs1)
	require.Error(t, err)
}

func TestProposalVerifyRejectsReproposalHashMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)

	// Build a PrepareQC at (0, 0).
	vs0 := ViewSpec{Epoch: ep}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0 := utils.OrPanic1(NewProposal(leader0, vs0, time.Now(), nil, utils.None[*AppQC]()))

	var prepareVotes []*Signed[*PrepareVote]
	for _, k := range keys {
		prepareVotes = append(prepareVotes, Sign(k, NewPrepareVote(fp0.Proposal().Msg())))
	}
	prepareQC := NewPrepareQC(prepareVotes)

	var timeoutVotes []*FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, NewFullTimeoutVote(k, View{Index: 0, Number: 0, EpochIndex: ep.EpochIndex()}, utils.Some(prepareQC)))
	}
	timeoutQC := NewTimeoutQC(timeoutVotes)

	vs1 := ViewSpec{TimeoutQC: utils.Some(timeoutQC), Epoch: ep}
	leader1 := leaderKey(committee, keys, vs1.View())

	// Build the valid reproposal, then tamper its timestamp to get a different hash.
	reproposal := utils.OrPanic1(NewProposal(leader1, vs1, time.Now(), nil, utils.None[*AppQC]()))

	origP := reproposal.Proposal().Msg()
	var ranges []*LaneRange
	for _, r := range origP.laneRanges {
		ranges = append(ranges, r)
	}
	wrongP := newProposal(origP.view, time.Now().Add(time.Hour), ranges, origP.app, origP.GlobalRange().First)
	wrongFP := &FullProposal{
		proposal:  Sign(leader1, wrongP),
		timeoutQC: reproposal.timeoutQC,
	}
	err := wrongFP.Verify(vs1)
	require.Error(t, err)
}

func TestProposalVerifyRejectsInvalidTimeoutQCSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)

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

	vs := ViewSpec{TimeoutQC: utils.Some(badTimeoutQC), Epoch: ep}
	leader := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(leader, vs, time.Now(), nil, utils.None[*AppQC]()))

	err := fp.Verify(vs)
	require.Error(t, err)
}

func TestViewSpecViewStampsEpochIndex(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	epochIdx := EpochIndex(7)
	ep := NewEpoch(epochIdx, OpenRoadRange(), time.Time{}, committee, 0)

	// Without TimeoutQC: epoch index must come from vs.Epoch.
	vs0 := ViewSpec{Epoch: ep}
	if got := vs0.View().EpochIndex; got != epochIdx {
		t.Fatalf("no-TimeoutQC path: EpochIndex = %d, want %d", got, epochIdx)
	}

	// With TimeoutQC: epoch index must still come from vs.Epoch, not the QC's stored value.
	tqc := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[0], View{EpochIndex: 0}, utils.None[*PrepareQC]()),
	})
	vs1 := ViewSpec{TimeoutQC: utils.Some(tqc), Epoch: ep}
	if got := vs1.View().EpochIndex; got != epochIdx {
		t.Fatalf("TimeoutQC path: EpochIndex = %d, want %d", got, epochIdx)
	}
}

func TestViewLess(t *testing.T) {
	cases := []struct {
		a, b View
		want bool
	}{
		{View{EpochIndex: 0, Index: 0, Number: 0}, View{EpochIndex: 1, Index: 0, Number: 0}, true},
		{View{EpochIndex: 1, Index: 0, Number: 0}, View{EpochIndex: 0, Index: 0, Number: 0}, false},
		{View{EpochIndex: 0, Index: 0, Number: 0}, View{EpochIndex: 0, Index: 1, Number: 0}, true},
		{View{EpochIndex: 0, Index: 1, Number: 0}, View{EpochIndex: 0, Index: 0, Number: 0}, false},
		{View{EpochIndex: 0, Index: 0, Number: 0}, View{EpochIndex: 0, Index: 0, Number: 1}, true},
		{View{EpochIndex: 0, Index: 0, Number: 1}, View{EpochIndex: 0, Index: 0, Number: 0}, false},
		{View{EpochIndex: 0, Index: 0, Number: 0}, View{EpochIndex: 0, Index: 0, Number: 0}, false},
	}
	for _, c := range cases {
		if got := c.a.Less(c.b); got != c.want {
			t.Errorf("%v.Less(%v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
