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

func TestProposalVerifyRejectsEmptyTipcut(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}

	// Direct Proposal.Verify rejects empty tipcuts (no LaneQCs / zero GlobalRange).
	// Local propose waits via WaitForLaneQCs; verification must refuse empty tipcuts
	// from peers as well.
	empty := newProposal(vs.View(), time.Now(), nil, vs.NextGlobalBlock())
	require.Equal(t, uint64(0), empty.GlobalRange().Len())
	require.Error(t, empty.Verify(ep))
}

// oneLaneQCMap builds a single LaneQC so NewProposal is non-empty (empty tipcuts
// are rejected by Proposal.Verify).
func oneLaneQCMap(rng utils.Rng, committee *Committee, keys []SecretKey, vs ViewSpec) map[LaneID]*LaneQC {
	lane := committee.Lanes().At(0)
	n := LaneRangeOpt(vs.CommitQC, lane).Next()
	return map[LaneID]*LaneQC{lane: makeLaneQC(rng, committee, keys, lane, n, BlockHeaderHash{})}
}

func TestProposalVerifyFreshWithBlocks(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	// Produce a LaneQC for the proposer's lane.
	lane := committee.Lane(proposerKey.Public()).OrPanic("missing lane")
	laneQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(),
		map[LaneID]*LaneQC{lane: laneQC}))
	require.NoError(t, fp.Verify(vs, testViewTimeout))
}

func TestNewProposalRejectsLaneRangeLongerThanMaxLaneRangeInProposal(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())
	lane := committee.Lane(proposerKey.Public()).OrPanic("missing lane")

	laneQC := makeLaneQC(rng, committee, keys, lane, MaxLaneRangeInProposal, GenBlockHeaderHash(rng))
	_, err := NewProposal(
		proposerKey,
		vs,
		time.Now(),
		map[LaneID]*LaneQC{lane: laneQC},
	)
	require.Error(t, err)
}

func TestProposalBlockTimestampStrictlyMonotone(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	firstBlock := ep.FirstBlock()
	vs0 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposer0 := leaderKey(committee, keys, vs0.View())
	lane := committee.Lane(proposer0.Public()).OrPanic("missing lane")

	firstProposal := utils.OrPanic1(NewProposal(
		proposer0,
		vs0, time.Now(),
		map[LaneID]*LaneQC{
			lane: makeLaneQC(rng, committee, keys, lane, 2, GenBlockHeaderHash(rng)),
		},
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
	vs1 := ViewSpec{ConsensusSpec: ConsensusSpec{CommitQC: utils.Some(commitQC0), Epoch: ep}}
	proposer1 := leaderKey(committee, keys, vs1.View())

	secondProposal := utils.OrPanic1(NewProposal(
		proposer1,
		vs1, vs1.ClampTimestamp(time.Now(), testViewTimeout),
		map[LaneID]*LaneQC{
			lane: makeLaneQC(rng, committee, keys, lane, 3, GenBlockHeaderHash(rng)),
		},
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
		vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
		k := leaderKey(committee, keys, vs.View())
		fp := utils.OrPanic1(NewProposal(k, vs, genesisTimestamp, oneLaneQCMap(rng, committee, keys, vs)))
		require.NoError(t, fp.Verify(vs, testViewTimeout))

		vsLater := vs
		vsLater.Epoch = NewEpoch(ep.EpochIndex(), ep.RoadRange(), fp.Proposal().Msg().Timestamp().Add(time.Nanosecond), committee, ep.FirstBlock())
		require.Error(t, fp.Verify(vsLater, testViewTimeout))
	})

	t.Run("wrt previous proposal", func(t *testing.T) {
		rng := utils.TestRng()
		committee, keys := GenCommittee(rng, 4)
		ep := genFreshEpoch(rng, committee)
		vs0 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
		proposer0 := leaderKey(committee, keys, vs0.View())
		lane := committee.Lane(proposer0.Public()).OrPanic("missing lane")
		lQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))

		fp0a := utils.OrPanic1(NewProposal(
			proposer0,
			vs0, time.Now(),
			map[LaneID]*LaneQC{lane: lQC},
		))
		fp0b := utils.OrPanic1(NewProposal(
			proposer0,
			vs0, fp0a.Proposal().Msg().NextTimestamp().Add(time.Hour),
			map[LaneID]*LaneQC{lane: lQC},
		))

		vs1a := ViewSpec{ConsensusSpec: ConsensusSpec{CommitQC: utils.Some(makeCommitQCFromProposal(keys, fp0a)), Epoch: ep}}
		vs1b := ViewSpec{ConsensusSpec: ConsensusSpec{CommitQC: utils.Some(makeCommitQCFromProposal(keys, fp0b)), Epoch: ep}}
		proposer1 := leaderKey(committee, keys, vs1a.View())

		fp1a := utils.OrPanic1(NewProposal(
			proposer1,
			vs1a, fp0a.Proposal().Msg().NextTimestamp(),
			oneLaneQCMap(rng, committee, keys, vs1a),
		))

		require.NoError(t, fp1a.Verify(vs1a, testViewTimeout))
		require.Error(t, fp1a.Verify(vs1b, testViewTimeout))
	})
}

func viewSpecAtNumber(ep *Epoch, keys []SecretKey, n ViewNumber) ViewSpec {
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	if n == 0 {
		return vs
	}
	var votes []*FullTimeoutVote
	timedOut := View{Index: vs.Index(), Number: n - 1, EpochIndex: ep.EpochIndex()}
	for _, k := range keys {
		votes = append(votes, NewFullTimeoutVote(k, timedOut, utils.None[*PrepareQC]()))
	}
	vs.TimeoutQC = utils.Some(NewTimeoutQC(votes))
	return vs
}

func TestTimestampRange(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)

	width := func(n ViewNumber) time.Duration {
		vs := viewSpecAtNumber(ep, keys, n)
		earliest, latest := vs.TimestampRange(testViewTimeout)
		return latest.Sub(earliest)
	}
	require.Equal(t, 1*testViewTimeout, width(0))
	require.Equal(t, 2*testViewTimeout, width(1))
	require.Equal(t, 6*testViewTimeout, width(5))
	require.Equal(t, 7*testViewTimeout, width(6))

	vs := viewSpecAtNumber(ep, keys, 0)
	leader := leaderKey(committee, keys, vs.View())
	earliest, latest := vs.TimestampRange(testViewTimeout)
	require.True(t, vs.ClampTimestamp(earliest.Add(-time.Hour), testViewTimeout).Equal(earliest))
	ts := vs.ClampTimestamp(latest.Add(time.Hour), testViewTimeout)
	require.True(t, ts.Equal(latest))
	fp := utils.OrPanic1(NewProposal(leader, vs, ts, oneLaneQCMap(rng, committee, keys, vs)))
	require.True(t, fp.Proposal().Msg().Timestamp().Equal(ts))
	require.NoError(t, fp.Verify(vs, testViewTimeout))

	p := fp.Proposal().Msg()
	var ranges []*LaneRange
	for _, r := range p.laneRanges {
		ranges = append(ranges, r)
	}
	late := &FullProposal{
		proposal:  Sign(leader, newProposal(p.View(), latest.Add(time.Nanosecond), ranges, p.GlobalRange().First)),
		laneQCs:   fp.laneQCs,
		timeoutQC: fp.timeoutQC,
	}
	require.Error(t, late.Verify(vs, testViewTimeout))
}

func TestProposalVerifyRejectsViewMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)

	// Build a valid proposal at genesis view (0, 0).
	vs0 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp := utils.OrPanic1(NewProposal(leader0, vs0, vs0.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs0)))
	require.NoError(t, fp.Verify(vs0, testViewTimeout))

	// Verify it against a different ViewSpec (view 1, 0).
	commitQC := makeCommitQCFromProposal(keys, fp)
	vs1 := ViewSpec{ConsensusSpec: ConsensusSpec{CommitQC: utils.Some(commitQC), Epoch: ep}}
	err := fp.Verify(vs1, testViewTimeout)
	require.Error(t, err)
}

func TestProposalVerifyRejectsForgedSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	// Both timestamps sit inside [NextTimestamp(), NextTimestamp() + testViewTimeout].
	fp1 := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs)))
	fp2 := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp().Add(time.Millisecond), oneLaneQCMap(rng, committee, keys, vs)))

	// Graft fp1's signature onto fp2 (different content).
	fp2.proposal.sig = fp1.proposal.sig
	err := fp2.Verify(vs, testViewTimeout)
	require.Error(t, err)
}

func TestProposalVerifyRejectsWrongProposer(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	correctLeader := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(correctLeader, vs, vs.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs)))

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
		timeoutQC: fp.timeoutQC,
	}
	err := tamperedFP.Verify(vs, testViewTimeout)
	require.Error(t, err)
}

func TestProposalVerifyRejectsInconsistentTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}} // no timeoutQC
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs)))

	// Attach a timeoutQC that the ViewSpec doesn't expect.
	var timeoutVotes []*FullTimeoutVote
	for _, k := range keys {
		timeoutVotes = append(timeoutVotes, NewFullTimeoutVote(k, View{Index: 0, Number: 0}, utils.None[*PrepareQC]()))
	}
	tQC := NewTimeoutQC(timeoutVotes)

	tamperedFP := &FullProposal{
		proposal:  fp.proposal,
		laneQCs:   fp.laneQCs,
		timeoutQC: utils.Some(tQC),
	}
	err := tamperedFP.Verify(vs, testViewTimeout)
	require.Error(t, err)
}

func TestProposalVerifyRejectsNonCommitteeLane(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs)))

	// Keep the non-empty committee tipcut and add a non-committee lane.
	// LaneRange.Verify rejects X because it's not a committee lane.
	extraLane := LaneID{Validator: GenSecretKey(rng).Public(), Joined: GenEpochIndex(rng)}
	require.False(t, committee.HasLane(extraLane))

	origProposal := fp.Proposal().Msg()
	tamperedRanges := make([]*LaneRange, 0, len(origProposal.laneRanges)+1)
	for _, r := range origProposal.laneRanges {
		tamperedRanges = append(tamperedRanges, r)
	}
	tamperedRanges = append(tamperedRanges, NewLaneRange(extraLane, 0, utils.None[*BlockHeader]()))

	tamperedProposal := newProposal(origProposal.view, origProposal.timestamp, tamperedRanges, origProposal.GlobalRange().First)
	maliciousFP := &FullProposal{
		proposal:  Sign(proposerKey, tamperedProposal),
		laneQCs:   fp.laneQCs,
		timeoutQC: fp.timeoutQC,
	}
	err := maliciousFP.Verify(vs, testViewTimeout)
	require.Error(t, err)
}

func TestProposalVerifyAcceptsImplicitLaneRange(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs)))

	// Drop one empty lane — the omitted lane gets an implicit [0, 0) range,
	// which matches the expected first=0 at genesis. Keep the non-empty range
	// (and its LaneQC) so the tipcut is non-empty.
	origP := fp.Proposal().Msg()
	var keptRanges []*LaneRange
	droppedEmpty := false
	for _, r := range origP.laneRanges {
		if !droppedEmpty && r.Len() == 0 {
			droppedEmpty = true
			continue
		}
		keptRanges = append(keptRanges, r)
	}
	require.True(t, droppedEmpty)

	shortProposal := newProposal(origP.view, origP.timestamp, keptRanges, origP.GlobalRange().First)
	shortFP := &FullProposal{
		proposal: Sign(proposerKey, shortProposal),
		laneQCs:  fp.laneQCs,
	}
	require.NoError(t, shortFP.Verify(vs, testViewTimeout))
}

func TestProposalVerifyAcceptsNonContiguousImplicitRanges(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs)))

	// Drop every other empty lane (keep the non-empty range and its LaneQC).
	origP := fp.Proposal().Msg()
	var keptRanges []*LaneRange
	emptyIdx := 0
	for _, r := range origP.laneRanges {
		if r.Len() == 0 {
			if emptyIdx%2 == 0 {
				emptyIdx++
				continue
			}
			emptyIdx++
		}
		keptRanges = append(keptRanges, r)
	}

	shortProposal := newProposal(origP.view, origP.timestamp, keptRanges, origP.GlobalRange().First)
	shortFP := &FullProposal{
		proposal: Sign(proposerKey, shortProposal),
		laneQCs:  fp.laneQCs,
	}
	require.NoError(t, shortFP.Verify(vs, testViewTimeout))
}

func TestLaneRangeVerifyRejectsHashAtHeightZero(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := GenCommittee(rng, 4)
	lane := committee.Lanes().At(0)

	require.Error(t, (&LaneRange{lane: lane, first: 0, next: 0, lastHash: GenBlockHeaderHash(rng)}).Verify(committee))
	require.NoError(t, (&LaneRange{lane: lane, first: 0, next: 0}).Verify(committee))
	// Past height 0 an empty range may carry a tip. FullProposal.Verify checks that it matches.
	require.NoError(t, (&LaneRange{
		lane: lane, first: 1, next: 1, lastHash: GenBlockHeaderHash(rng),
	}).Verify(committee))
}

func TestProposalEmptyLaneAtHeightZeroHasNoHash(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs)))
	require.NoError(t, fp.Verify(vs, testViewTimeout))
	var idle LaneID
	for lane := range committee.Lanes().All() {
		r := fp.Proposal().Msg().LaneRange(lane)
		if r.Len() == 0 {
			require.Equal(t, BlockNumber(0), r.Next())
			require.Equal(t, BlockHeaderHash{}, r.LastHash())
			idle = lane
		}
	}
	require.NotEqual(t, LaneID{}, idle)

	// A lane that has not started must not name a tip. There is no previous
	// CommitQC to copy.
	var ranges []*LaneRange
	for _, r := range fp.Proposal().Msg().laneRanges {
		if r.Lane() == idle {
			ranges = append(ranges, &LaneRange{lane: idle, first: 0, next: 0, lastHash: GenBlockHeaderHash(rng)})
			continue
		}
		ranges = append(ranges, r)
	}
	invented := &FullProposal{
		proposal: Sign(proposerKey, newProposal(
			fp.Proposal().Msg().view, fp.Proposal().Msg().timestamp, ranges, fp.Proposal().Msg().GlobalRange().First)),
		laneQCs: fp.laneQCs,
	}
	require.Error(t, invented.Verify(vs, testViewTimeout))
}

func TestProposalCopiesEmptyLaneLastHash(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs0 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	lane0 := committee.Lanes().At(0)
	lane1 := committee.Lanes().At(1)

	fp0 := utils.OrPanic1(NewProposal(
		leaderKey(committee, keys, vs0.View()), vs0, vs0.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs0)))
	require.NoError(t, fp0.Verify(vs0, testViewTimeout))
	tip := fp0.Proposal().Msg().LaneRange(lane0).LastHash()
	require.NotEqual(t, BlockHeaderHash{}, tip)

	cqc := makeCommitQCFromProposal(keys, fp0)
	vs1 := ViewSpec{ConsensusSpec: ConsensusSpec{CommitQC: utils.Some(cqc), Epoch: ep}}
	// Extend a different lane so lane0's range is empty and must carry tip.
	qc1 := makeLaneQC(rng, committee, keys, lane1, 0, BlockHeaderHash{})
	fp1 := utils.OrPanic1(NewProposal(
		leaderKey(committee, keys, vs1.View()), vs1, vs1.NextTimestamp(), map[LaneID]*LaneQC{lane1: qc1}))
	require.NoError(t, fp1.Verify(vs1, testViewTimeout))
	empty := fp1.Proposal().Msg().LaneRange(lane0)
	require.Equal(t, uint64(0), empty.Len())
	require.Equal(t, tip, empty.LastHash())

	var ranges []*LaneRange
	for _, r := range fp1.Proposal().Msg().laneRanges {
		if r.Lane() == lane0 {
			ranges = append(ranges, &LaneRange{lane: lane0, first: r.First(), next: r.First(), lastHash: BlockHeaderHash{}})
			continue
		}
		ranges = append(ranges, r)
	}
	stripped := &FullProposal{
		proposal: Sign(leaderKey(committee, keys, vs1.View()), newProposal(
			fp1.Proposal().Msg().view, fp1.Proposal().Msg().timestamp, ranges, fp1.Proposal().Msg().GlobalRange().First)),
		laneQCs: fp1.laneQCs,
	}
	require.Error(t, stripped.Verify(vs1, testViewTimeout))
}

func TestProposalVerifyRejectsLaneRangeFirstMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs)))

	// Tamper the non-empty lane's First (genesis expects 0) while keeping a
	// non-empty range and a matching LaneQC so Verify reaches the first-mismatch check.
	origP := fp.Proposal().Msg()
	var target LaneID
	for _, r := range origP.laneRanges {
		if r.Len() > 0 {
			target = r.Lane()
			break
		}
	}
	require.NotEqual(t, LaneID{}, target)
	badQC := makeLaneQC(rng, committee, keys, target, 5, GenBlockHeaderHash(rng))
	var tamperedRanges []*LaneRange
	for _, r := range origP.laneRanges {
		if r.Lane() == target {
			tamperedRanges = append(tamperedRanges, NewLaneRange(target, 5, utils.Some(badQC.Header())))
		} else {
			tamperedRanges = append(tamperedRanges, r)
		}
	}
	tamperedProposal := newProposal(origP.view, origP.timestamp, tamperedRanges, origP.GlobalRange().First)
	tamperedFP := &FullProposal{
		proposal: Sign(proposerKey, tamperedProposal),
		laneQCs:  map[LaneID]*LaneQC{target: badQC},
	}
	err := tamperedFP.Verify(vs, testViewTimeout)
	require.Error(t, err)
}

func TestProposalVerifyRejectsMissingLaneQC(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := committee.Lane(keys[0].Public()).OrPanic("missing lane")
	laneQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))

	// Build a valid proposal with a block, then strip the laneQC.
	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(),
		map[LaneID]*LaneQC{lane: laneQC}))

	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		laneQCs:  map[LaneID]*LaneQC{},
	}
	err := tamperedFP.Verify(vs, testViewTimeout)
	require.Error(t, err)
}

func TestProposalVerifyRejectsLaneQCBlockNumberMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := committee.Lane(keys[0].Public()).OrPanic("missing lane")

	// Build a valid proposal with a QC certifying block 1 (range [0, 2)).
	goodQC := makeLaneQC(rng, committee, keys, lane, 1, GenBlockHeaderHash(rng))
	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(),
		map[LaneID]*LaneQC{lane: goodQC}))

	// Swap in a QC certifying block 0 — range expects block 1.
	wrongQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		laneQCs:  map[LaneID]*LaneQC{lane: wrongQC},
	}
	err := tamperedFP.Verify(vs, testViewTimeout)
	require.Error(t, err)
}

func TestProposalVerifyRejectsInvalidLaneQCSignature(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := committee.Lane(keys[0].Public()).OrPanic("missing lane")
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

	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(),
		map[LaneID]*LaneQC{lane: badLaneQC}))

	err := fp.Verify(vs, testViewTimeout)
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
	lane := committee.Lane(leaderKey(committee, keys, View{}).Public()).OrPanic("missing lane")
	// Bypass NewProposal's check by constructing the proposal directly.
	oversized := newProposal(
		View{},
		time.Now(),
		[]*LaneRange{NewLaneRange(lane, 0, utils.Some(NewBlock(lane, MaxLaneRangeInProposal, GenBlockHeaderHash(rng), GenPayload(rng)).Header()))},
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
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{CommitQC: prev, Epoch: ep}}
	return utils.OrPanic1(NewProposal(
		leaderKey(committee, keys, vs.View()),
		vs, time.Now(),
		laneQCs,
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

func TestProposalVerifyRejectsLaneQCHeaderHashMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposerKey := leaderKey(committee, keys, vs.View())

	lane := committee.Lane(proposerKey.Public()).OrPanic("missing lane")

	// Build a valid proposal with a QC for block 0.
	realQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	fp := utils.OrPanic1(NewProposal(proposerKey, vs, vs.NextTimestamp(),
		map[LaneID]*LaneQC{lane: realQC}))

	// Swap in a different QC for block 0 (different payload → different hash).
	differentQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	require.NotEqual(t, realQC.Header().Hash(), differentQC.Header().Hash())

	tamperedFP := &FullProposal{
		proposal: fp.proposal,
		laneQCs:  map[LaneID]*LaneQC{lane: differentQC},
	}
	err := tamperedFP.Verify(vs, testViewTimeout)
	require.Error(t, err)
}

func TestProposalAtView(t *testing.T) {
	rng := utils.TestRng()
	view := GenView(rng)
	timestamp := utils.GenTimestamp(rng)
	laneRanges := utils.GenSlice(rng, GenLaneRange)
	globalFirst := GlobalBlockNumber(rng.Uint64())
	p := newProposal(view, timestamp, laneRanges, globalFirst)

	next := GenView(rng)
	got := p.atView(next)
	if err := utils.TestDiff(newProposal(next, timestamp, laneRanges, globalFirst), got); err != nil {
		t.Fatal(err)
	}
	if err := utils.TestDiff(newProposal(view, timestamp, laneRanges, globalFirst), p); err != nil {
		t.Fatal(err)
	}
}

func TestProposalVerifyValidReproposal(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	// Build a proposal at view (0, 0) with one lane block so sum(lane.First) > 0.
	// firstBlock > 0 ensures a reproposal bug that passes GlobalRange().First
	// (= sum(lane.First)+firstBlock) instead of firstBlock would be caught.
	ep := genFreshEpoch(rng, committee)
	vs0 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	leader0 := leaderKey(committee, keys, vs0.View())
	lane := committee.Lane(committee.Leader(vs0.View())).OrPanic("missing lane")
	laneQC0 := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	fp0 := utils.OrPanic1(NewProposal(leader0, vs0, vs0.NextTimestamp(),
		map[LaneID]*LaneQC{lane: laneQC0}))

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

	vs1 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}, TimeoutQC: utils.Some(timeoutQC)}
	require.Equal(t, View{Index: 0, Number: 1, EpochIndex: ep.EpochIndex()}, vs1.View())

	leader1 := leaderKey(committee, keys, vs1.View())
	reproposal := utils.OrPanic1(NewProposal(leader1, vs1, time.Now(), oneLaneQCMap(rng, committee, keys, vs1)))

	// Reproposal keeps the locked proposal's timestamp and global range.
	require.Equal(t, fp0.Proposal().Msg().GlobalRange(), reproposal.Proposal().Msg().GlobalRange())
	require.True(t, fp0.Proposal().Msg().Timestamp().Equal(reproposal.Proposal().Msg().Timestamp()))
	require.NoError(t, reproposal.Verify(vs1, testViewTimeout))
}

func TestProposalVerifyRejectsReproposalWithUnnecessaryData(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)

	// Build a PrepareQC at (0, 0).
	vs0 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0 := utils.OrPanic1(NewProposal(leader0, vs0, vs0.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs0)))

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

	vs1 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}, TimeoutQC: utils.Some(timeoutQC)}
	leader1 := leaderKey(committee, keys, vs1.View())

	// Create a valid reproposal, then tamper it with unnecessary laneQCs.
	reproposal := utils.OrPanic1(NewProposal(leader1, vs1, time.Now(), oneLaneQCMap(rng, committee, keys, vs1)))

	lane := committee.Lane(keys[0].Public()).OrPanic("missing lane")
	laneQC := makeLaneQC(rng, committee, keys, lane, 0, GenBlockHeaderHash(rng))
	tamperedFP := &FullProposal{
		proposal:  reproposal.proposal,
		laneQCs:   map[LaneID]*LaneQC{lane: laneQC},
		timeoutQC: reproposal.timeoutQC,
	}
	err := tamperedFP.Verify(vs1, testViewTimeout)
	require.Error(t, err)
}

func TestProposalVerifyRejectsReproposalHashMismatch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := genFreshEpoch(rng, committee)

	// Build a PrepareQC at (0, 0).
	vs0 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	leader0 := leaderKey(committee, keys, vs0.View())
	fp0 := utils.OrPanic1(NewProposal(leader0, vs0, vs0.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs0)))

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

	vs1 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}, TimeoutQC: utils.Some(timeoutQC)}
	leader1 := leaderKey(committee, keys, vs1.View())

	// Build the valid reproposal, then tamper its timestamp to get a different hash.
	reproposal := utils.OrPanic1(NewProposal(leader1, vs1, time.Now(), oneLaneQCMap(rng, committee, keys, vs1)))

	origP := reproposal.Proposal().Msg()
	var ranges []*LaneRange
	for _, r := range origP.laneRanges {
		ranges = append(ranges, r)
	}
	wrongP := newProposal(origP.view, origP.timestamp.Add(time.Millisecond), ranges, origP.GlobalRange().First)
	wrongFP := &FullProposal{
		proposal:  Sign(leader1, wrongP),
		timeoutQC: reproposal.timeoutQC,
	}
	err := wrongFP.Verify(vs1, testViewTimeout)
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

	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}, TimeoutQC: utils.Some(badTimeoutQC)}
	leader := leaderKey(committee, keys, vs.View())

	fp := utils.OrPanic1(NewProposal(leader, vs, vs.NextTimestamp(), oneLaneQCMap(rng, committee, keys, vs)))

	err := fp.Verify(vs, testViewTimeout)
	require.Error(t, err)
}

func TestViewSpecViewStampsEpochIndex(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	epochIdx := EpochIndex(7)
	ep := NewEpoch(epochIdx, OpenRoadRange(), time.Time{}, committee, 0)

	// Without TimeoutQC: epoch index must come from vs.Epoch.
	vs0 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	if got := vs0.View().EpochIndex; got != epochIdx {
		t.Fatalf("no-TimeoutQC path: EpochIndex = %d, want %d", got, epochIdx)
	}

	// With TimeoutQC: epoch index must still come from vs.Epoch, not the QC's stored value.
	tqc := NewTimeoutQC([]*FullTimeoutVote{
		NewFullTimeoutVote(keys[0], View{EpochIndex: 0}, utils.None[*PrepareQC]()),
	})
	vs1 := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}, TimeoutQC: utils.Some(tqc)}
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
