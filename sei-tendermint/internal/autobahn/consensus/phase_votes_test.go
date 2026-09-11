package consensus

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestPrepareVotes_QuorumFormsQC(t *testing.T) {
	rng := utils.TestRng()
	e := newVoteTestEnv(rng)
	pv := newPrepareVotes()
	proposal := types.GenProposalForEpoch(rng, e.ep, e.view)

	for _, k := range e.quorum {
		pv.pushVerifiedVote(e.ep.Committee(), types.Sign(k, types.NewPrepareVote(proposal)))
	}
	got, ok := pv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, e.view, got.Proposal().View())
	require.NoError(t, got.Verify(e.ep))
}

func TestPrepareVotes_DoesNotReplaceQCAtSameView(t *testing.T) {
	rng := utils.TestRng()
	e := newVoteTestEnv(rng)
	pv := newPrepareVotes()
	view1 := e.view.Next()
	proposal := types.GenProposalForEpoch(rng, e.ep, view1)

	for _, k := range e.quorum {
		pv.pushVerifiedVote(e.ep.Committee(), types.Sign(k, types.NewPrepareVote(proposal)))
	}
	before, ok := pv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, view1, before.Proposal().View())
	pv.pushVerifiedVote(e.ep.Committee(), types.Sign(e.keys[len(e.quorum)], types.NewPrepareVote(proposal)))
	after, ok := pv.qc.Load().Get()
	require.True(t, ok)
	require.True(t, before == after)
}

func TestPrepareVotes_ReplacesQCAtNewerView(t *testing.T) {
	rng := utils.TestRng()
	e := newVoteTestEnv(rng)
	pv := newPrepareVotes()
	view1 := e.view.Next()
	c := e.ep.Committee()

	p0 := types.GenProposalForEpoch(rng, e.ep, e.view)
	for _, k := range e.quorum {
		pv.pushVerifiedVote(c, types.Sign(k, types.NewPrepareVote(p0)))
	}
	got, ok := pv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, e.view, got.Proposal().View())

	p1 := types.GenProposalForEpoch(rng, e.ep, view1)
	for _, k := range e.quorum {
		pv.pushVerifiedVote(c, types.Sign(k, types.NewPrepareVote(p1)))
	}
	got, ok = pv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, view1, got.Proposal().View())
	require.NoError(t, got.Verify(e.ep))
}

// Prepare votes are bucketed by vote hash, so votes for conflicting proposals at the
// same view never combine into a QC.
func TestPrepareVotes_ConflictingProposalsDoNotFormQC(t *testing.T) {
	rng := utils.TestRng()
	e := newVoteTestEnv(rng)
	pv := newPrepareVotes()
	first, second := e.splitBelowQuorum(t)
	a := types.GenProposalForEpoch(rng, e.ep, e.view)
	b := types.GenProposalForEpoch(rng, e.ep, e.view)

	for _, k := range first {
		pv.pushVerifiedVote(e.ep.Committee(), types.Sign(k, types.NewPrepareVote(a)))
	}
	for _, k := range second {
		pv.pushVerifiedVote(e.ep.Committee(), types.Sign(k, types.NewPrepareVote(b)))
	}
	require.False(t, pv.qc.Load().IsPresent())
}

func TestCommitVotes_QuorumFormsQC(t *testing.T) {
	rng := utils.TestRng()
	e := newVoteTestEnv(rng)
	cv := newCommitVotes()
	proposal := types.GenProposalForEpoch(rng, e.ep, e.view)

	for _, k := range e.quorum {
		cv.pushVerifiedVote(e.ep.Committee(), types.Sign(k, types.NewCommitVote(proposal)))
	}
	got, ok := cv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, e.view, got.Proposal().View())
	require.NoError(t, got.Verify(e.ep))
}

// Commit votes are bucketed by vote hash, so votes for conflicting proposals at the same
// view never combine into a QC.
func TestCommitVotes_ConflictingProposalsDoNotFormQC(t *testing.T) {
	rng := utils.TestRng()
	e := newVoteTestEnv(rng)
	cv := newCommitVotes()
	first, second := e.splitBelowQuorum(t)
	a := types.GenProposalForEpoch(rng, e.ep, e.view)
	b := types.GenProposalForEpoch(rng, e.ep, e.view)

	for _, k := range first {
		cv.pushVerifiedVote(e.ep.Committee(), types.Sign(k, types.NewCommitVote(a)))
	}
	for _, k := range second {
		cv.pushVerifiedVote(e.ep.Committee(), types.Sign(k, types.NewCommitVote(b)))
	}
	require.False(t, cv.qc.Load().IsPresent())
}

func TestTimeoutVotes_QuorumFormsQC(t *testing.T) {
	rng := utils.TestRng()
	e := newVoteTestEnv(rng)
	tv := newTimeoutVotes()
	pqc := makePrepareQC(e.keys, types.GenProposalForEpoch(rng, e.ep, e.view))

	for _, k := range e.quorum {
		tv.pushVerifiedVote(e.ep.Committee(), types.NewFullTimeoutVote(k, e.view, utils.Some(pqc)))
	}
	got, ok := tv.qc.Load().Get()
	require.True(t, ok)
	require.Equal(t, e.view, got.View())
	require.NoError(t, got.Verify(e.ep, utils.None[*types.CommitQC]()))
	require.True(t, got.LatestPrepareQC().IsPresent())
}

// Timeout votes are bucketed by view rather than by content, so a quorum still forms when
// signers report different prepare QCs.
func TestTimeoutVotes_DifferingPrepareQCsFormQC(t *testing.T) {
	rng := utils.TestRng()
	e := newVoteTestEnv(rng)
	tv := newTimeoutVotes()
	pqc := makePrepareQC(e.keys, types.GenProposalForEpoch(rng, e.ep, e.view))

	for i, k := range e.quorum {
		latest := utils.None[*types.PrepareQC]()
		if i == 0 {
			latest = utils.Some(pqc)
		}
		tv.pushVerifiedVote(e.ep.Committee(), types.NewFullTimeoutVote(k, e.view, latest))
	}
	got, ok := tv.qc.Load().Get()
	require.True(t, ok)
	require.NoError(t, got.Verify(e.ep, utils.None[*types.CommitQC]()))
	require.True(t, got.LatestPrepareQC().IsPresent())
}
