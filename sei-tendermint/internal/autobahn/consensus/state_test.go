package consensus

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// newTestState creates a State for testing with no persistence and a long
// view timeout (so voteTimeout is only triggered explicitly).
// keys[0] is used as the node's signing key.
func newTestState(t *testing.T, keys []types.SecretKey) *State {
	committee := testCommittee(keys...)
	dataState := data.NewState(
		&data.Config{Committee: committee},
		utils.None[data.BlockStore](),
	)
	s, err := NewState(&Config{
		Key:                keys[0],
		ViewTimeout:        func(types.View) time.Duration { return time.Hour },
		PersistentStateDir: utils.None[string](),
	}, dataState)
	require.NoError(t, err)
	return s
}

// startRunOutputs launches runOutputs in the background and returns
// a context that is cancelled when the test finishes.
func startRunOutputs(t *testing.T, s *State) context.Context {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go s.runOutputs(ctx)
	return ctx
}

// makeTimeoutQC creates a TimeoutQC at the given view where all keys
// attach the given PrepareQC.
func makeTimeoutQC(keys []types.SecretKey, view types.View, pqc utils.Option[*types.PrepareQC]) *types.TimeoutQC {
	votes := make([]*types.FullTimeoutVote, len(keys))
	for i, k := range keys {
		votes[i] = types.NewFullTimeoutVote(k, view, pqc)
	}
	return types.NewTimeoutQC(votes)
}

// testTimeoutVotePrepareQC extracts the PrepareQC carried by a FullTimeoutVote
// by wrapping it in a single-vote TimeoutQC and reading LatestPrepareQC.
func testTimeoutVotePrepareQC(tv *types.FullTimeoutVote) utils.Option[*types.PrepareQC] {
	return types.NewTimeoutQC([]*types.FullTimeoutVote{tv}).LatestPrepareQC()
}

// --- voteTimeout PrepareQC selection tests ---
//
// These exercise the real State.pushTimeoutQC and State.voteTimeout methods
// rather than mirroring their logic, covering five scenarios:
//   1. Both None                           → None
//   2. i.PrepareQC present, no TimeoutQC   → uses PrepareQC
//   3. i.PrepareQC None, inherited present → inherited (consecutive timeout / offline leader)
//   4. Both present, current view higher   → uses current PrepareQC
//   5. i.PrepareQC present, inherited None → uses PrepareQC

func TestVoteTimeoutPrepareQC_BothNone(t *testing.T) {
	rng := utils.TestRng()
	_, keys := types.GenCommittee(rng, 3)
	s := newTestState(t, keys)
	ctx := startRunOutputs(t, s)

	require.NoError(t, s.voteTimeout(ctx, types.View{Index: 0, Number: 0}))

	tv, ok := s.innerRecv.Load().TimeoutVote.Get()
	require.True(t, ok)
	require.False(t, testTimeoutVotePrepareQC(tv).IsPresent())
}

func TestVoteTimeoutPrepareQC_OnlyCurrentView(t *testing.T) {
	rng := utils.TestRng()
	_, keys := types.GenCommittee(rng, 3)
	s := newTestState(t, keys)
	ctx := startRunOutputs(t, s)

	pqc := makePrepareQC(keys, types.GenProposalAt(rng, types.View{Index: 0, Number: 0}))
	require.NoError(t, s.pushPrepareQC(ctx, pqc))

	require.NoError(t, s.voteTimeout(ctx, types.View{Index: 0, Number: 0}))

	tv, ok := s.innerRecv.Load().TimeoutVote.Get()
	require.True(t, ok)
	require.True(t, testTimeoutVotePrepareQC(tv).IsPresent())
}

// TestVoteTimeoutPrepareQC_InheritedFromTimeoutQC is the core safety test:
// consecutive timeouts with an offline leader must not lose the PrepareQC.
func TestVoteTimeoutPrepareQC_InheritedFromTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	_, keys := types.GenCommittee(rng, 3)
	s := newTestState(t, keys)
	ctx := startRunOutputs(t, s)

	// View (0, 0): push PrepareQC for proposal P.
	view0 := types.View{Index: 0, Number: 0}
	pqc0 := makePrepareQC(keys, types.GenProposalAt(rng, view0))
	require.NoError(t, s.pushPrepareQC(ctx, pqc0))

	// Timeout at (0, 0) — all votes carry pqc0.
	tqc0 := makeTimeoutQC(keys, view0, utils.Some(pqc0))
	require.NoError(t, s.pushTimeoutQC(ctx, tqc0))

	// Now at (0, 1). PrepareQC was cleared; voteTimeout must inherit it.
	view1 := types.View{Index: 0, Number: 1}
	require.NoError(t, s.voteTimeout(ctx, view1))

	tv1, ok := s.innerRecv.Load().TimeoutVote.Get()
	require.True(t, ok)
	require.True(t, testTimeoutVotePrepareQC(tv1).IsPresent(),
		"PrepareQC must be inherited from TimeoutQC")

	// Chain through a second timeout to prove it propagates indefinitely.
	tqc1 := makeTimeoutQC(keys, view1, testTimeoutVotePrepareQC(tv1))
	require.NoError(t, s.pushTimeoutQC(ctx, tqc1))

	view2 := types.View{Index: 0, Number: 2}
	require.NoError(t, s.voteTimeout(ctx, view2))

	tv2, ok := s.innerRecv.Load().TimeoutVote.Get()
	require.True(t, ok)
	require.True(t, testTimeoutVotePrepareQC(tv2).IsPresent(),
		"PrepareQC must survive a third consecutive timeout")
}

// TestVoteTimeoutPrepareQC_CurrentViewHigherThanInherited verifies that when
// a reproposal succeeds (PrepareQC forms at the current view), the current
// view's PrepareQC is preferred over the older inherited one.
func TestVoteTimeoutPrepareQC_CurrentViewHigherThanInherited(t *testing.T) {
	rng := utils.TestRng()
	_, keys := types.GenCommittee(rng, 3)
	s := newTestState(t, keys)
	ctx := startRunOutputs(t, s)

	// View (0, 0): PrepareQC for P.
	view0 := types.View{Index: 0, Number: 0}
	pqc0 := makePrepareQC(keys, types.GenProposalAt(rng, view0))
	require.NoError(t, s.pushPrepareQC(ctx, pqc0))

	// Timeout at (0, 0) → advance to (0, 1).
	tqc0 := makeTimeoutQC(keys, view0, utils.Some(pqc0))
	require.NoError(t, s.pushTimeoutQC(ctx, tqc0))

	// Reproposal at (0, 1) succeeds — new PrepareQC at view (0, 1).
	view1 := types.View{Index: 0, Number: 1}
	pqc1 := makePrepareQC(keys, types.GenProposalAt(rng, view1))
	require.NoError(t, s.pushPrepareQC(ctx, pqc1))

	require.NoError(t, s.voteTimeout(ctx, view1))

	tv, ok := s.innerRecv.Load().TimeoutVote.Get()
	require.True(t, ok)
	gotPQC := testTimeoutVotePrepareQC(tv)
	require.True(t, gotPQC.IsPresent())
	pqc, _ := gotPQC.Get()
	require.Equal(t, view1, pqc.Proposal().View(),
		"should use current view PrepareQC, not the older inherited one")
}

// TestVoteTimeoutPrepareQC_CurrentViewPresentInheritedNone verifies that when
// the TimeoutQC has no PrepareQC but a fresh one forms in the current view,
// the current view's PrepareQC is used.
func TestVoteTimeoutPrepareQC_CurrentViewPresentInheritedNone(t *testing.T) {
	rng := utils.TestRng()
	_, keys := types.GenCommittee(rng, 3)
	s := newTestState(t, keys)
	ctx := startRunOutputs(t, s)

	// Timeout at (0, 0) without PrepareQC.
	view0 := types.View{Index: 0, Number: 0}
	tqc0 := makeTimeoutQC(keys, view0, utils.None[*types.PrepareQC]())
	require.NoError(t, s.pushTimeoutQC(ctx, tqc0))

	// Fresh PrepareQC at (0, 1).
	view1 := types.View{Index: 0, Number: 1}
	pqc1 := makePrepareQC(keys, types.GenProposalAt(rng, view1))
	require.NoError(t, s.pushPrepareQC(ctx, pqc1))

	require.NoError(t, s.voteTimeout(ctx, view1))

	tv, ok := s.innerRecv.Load().TimeoutVote.Get()
	require.True(t, ok)
	gotPQC := testTimeoutVotePrepareQC(tv)
	require.True(t, gotPQC.IsPresent())
	pqc, _ := gotPQC.Get()
	require.Equal(t, view1, pqc.Proposal().View(),
		"should use current view PrepareQC when TimeoutQC has none")
}
