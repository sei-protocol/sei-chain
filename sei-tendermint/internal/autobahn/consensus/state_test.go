package consensus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// newTestState creates a State for testing with no persistence and a long
// view timeout (so voteTimeout is only triggered explicitly).
// keys[0] is used as the node's signing key.
func newTestState(rng utils.Rng) (*State, []types.SecretKey) {
	committee, keys := types.GenCommittee(rng, 3)
	dataState := utils.OrPanic1(data.NewState(
		&data.Config{Committee: committee},
		utils.OrPanic1(data.NewDataWAL(utils.None[string](), committee)),
	))
	s := utils.OrPanic1(NewState(&Config{
		Key:                keys[0],
		ViewTimeout:        func(types.View) time.Duration { return time.Hour },
		PersistentStateDir: utils.None[string](),
	}, dataState))
	return s, keys
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
	s, _ := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		if err := s.voteTimeout(ctx, types.View{Index: 0, Number: 0}); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present")
		}
		if testTimeoutVotePrepareQC(tv).IsPresent() {
			return fmt.Errorf("PrepareQC should not be present")
		}
		return nil
	})
	require.NoError(t, err)
}

func TestVoteTimeoutPrepareQC_OnlyCurrentView(t *testing.T) {
	rng := utils.TestRng()
	s, keys := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		pqc := makePrepareQC(keys, types.GenProposalAt(rng, types.View{Index: 0, Number: 0}))
		if err := s.pushPrepareQC(ctx, pqc); err != nil {
			return fmt.Errorf("pushPrepareQC: %w", err)
		}
		if err := s.voteTimeout(ctx, types.View{Index: 0, Number: 0}); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present")
		}
		if !testTimeoutVotePrepareQC(tv).IsPresent() {
			return fmt.Errorf("PrepareQC should be present")
		}
		return nil
	})
	require.NoError(t, err)
}

// TestVoteTimeoutPrepareQC_InheritedFromTimeoutQC is the core safety test:
// consecutive timeouts with an offline leader must not lose the PrepareQC.
func TestVoteTimeoutPrepareQC_InheritedFromTimeoutQC(t *testing.T) {
	rng := utils.TestRng()
	s, keys := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		// View (0, 0): push PrepareQC for proposal P.
		view0 := types.View{Index: 0, Number: 0}
		pqc0 := makePrepareQC(keys, types.GenProposalAt(rng, view0))
		if err := s.pushPrepareQC(ctx, pqc0); err != nil {
			return fmt.Errorf("pushPrepareQC: %w", err)
		}

		// Timeout at (0, 0) — all votes carry pqc0.
		tqc0 := makeTimeoutQC(keys, view0, utils.Some(pqc0))
		if err := s.pushTimeoutQC(ctx, tqc0); err != nil {
			return fmt.Errorf("pushTimeoutQC(tqc0): %w", err)
		}

		// Now at (0, 1). PrepareQC was cleared; voteTimeout must inherit it.
		view1 := types.View{Index: 0, Number: 1}
		if err := s.voteTimeout(ctx, view1); err != nil {
			return fmt.Errorf("voteTimeout(view1): %w", err)
		}
		tv1, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present at view1")
		}
		if !testTimeoutVotePrepareQC(tv1).IsPresent() {
			return fmt.Errorf("PrepareQC must be inherited from TimeoutQC")
		}

		// Chain through a second timeout to prove it propagates indefinitely.
		tqc1 := makeTimeoutQC(keys, view1, testTimeoutVotePrepareQC(tv1))
		if err := s.pushTimeoutQC(ctx, tqc1); err != nil {
			return fmt.Errorf("pushTimeoutQC(tqc1): %w", err)
		}
		view2 := types.View{Index: 0, Number: 2}
		if err := s.voteTimeout(ctx, view2); err != nil {
			return fmt.Errorf("voteTimeout(view2): %w", err)
		}
		tv2, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present at view2")
		}
		if !testTimeoutVotePrepareQC(tv2).IsPresent() {
			return fmt.Errorf("PrepareQC must survive a third consecutive timeout")
		}
		return nil
	})
	require.NoError(t, err)
}

// TestVoteTimeoutPrepareQC_CurrentViewHigherThanInherited verifies that when
// a reproposal succeeds (PrepareQC forms at the current view), the current
// view's PrepareQC is preferred over the older inherited one.
func TestVoteTimeoutPrepareQC_CurrentViewHigherThanInherited(t *testing.T) {
	rng := utils.TestRng()
	s, keys := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		// View (0, 0): PrepareQC for P.
		view0 := types.View{Index: 0, Number: 0}
		pqc0 := makePrepareQC(keys, types.GenProposalAt(rng, view0))
		if err := s.pushPrepareQC(ctx, pqc0); err != nil {
			return fmt.Errorf("pushPrepareQC(pqc0): %w", err)
		}

		// Timeout at (0, 0) → advance to (0, 1).
		tqc0 := makeTimeoutQC(keys, view0, utils.Some(pqc0))
		if err := s.pushTimeoutQC(ctx, tqc0); err != nil {
			return fmt.Errorf("pushTimeoutQC: %w", err)
		}

		// Reproposal at (0, 1) succeeds — new PrepareQC at view (0, 1).
		view1 := types.View{Index: 0, Number: 1}
		pqc1 := makePrepareQC(keys, types.GenProposalAt(rng, view1))
		if err := s.pushPrepareQC(ctx, pqc1); err != nil {
			return fmt.Errorf("pushPrepareQC(pqc1): %w", err)
		}

		if err := s.voteTimeout(ctx, view1); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present")
		}
		gotPQC := testTimeoutVotePrepareQC(tv)
		if !gotPQC.IsPresent() {
			return fmt.Errorf("PrepareQC should be present")
		}
		pqc, _ := gotPQC.Get()
		if pqc.Proposal().View() != view1 {
			return fmt.Errorf("expected PrepareQC at view %v, got %v", view1, pqc.Proposal().View())
		}
		return nil
	})
	require.NoError(t, err)
}

// TestVoteTimeoutPrepareQC_CurrentViewPresentInheritedNone verifies that when
// the TimeoutQC has no PrepareQC but a fresh one forms in the current view,
// the current view's PrepareQC is used.
func TestVoteTimeoutPrepareQC_CurrentViewPresentInheritedNone(t *testing.T) {
	rng := utils.TestRng()
	s, keys := newTestState(rng)

	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		// Timeout at (0, 0) without PrepareQC.
		view0 := types.View{Index: 0, Number: 0}
		tqc0 := makeTimeoutQC(keys, view0, utils.None[*types.PrepareQC]())
		if err := s.pushTimeoutQC(ctx, tqc0); err != nil {
			return fmt.Errorf("pushTimeoutQC: %w", err)
		}

		// Fresh PrepareQC at (0, 1).
		view1 := types.View{Index: 0, Number: 1}
		pqc1 := makePrepareQC(keys, types.GenProposalAt(rng, view1))
		if err := s.pushPrepareQC(ctx, pqc1); err != nil {
			return fmt.Errorf("pushPrepareQC: %w", err)
		}

		if err := s.voteTimeout(ctx, view1); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present")
		}
		gotPQC := testTimeoutVotePrepareQC(tv)
		if !gotPQC.IsPresent() {
			return fmt.Errorf("PrepareQC should be present")
		}
		pqc, _ := gotPQC.Get()
		if pqc.Proposal().View() != view1 {
			return fmt.Errorf("expected PrepareQC at view %v, got %v", view1, pqc.Proposal().View())
		}
		return nil
	})
	require.NoError(t, err)
}

// TestVoteTimeoutPrepareQC_PersistedRestart verifies that after a restart,
// voteTimeout still inherits the PrepareQC from the persisted TimeoutQC.
func TestVoteTimeoutPrepareQC_PersistedRestart(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	dir := t.TempDir()

	makeCfg := func() *Config {
		return &Config{
			Key:                keys[0],
			ViewTimeout:        func(types.View) time.Duration { return time.Hour },
			PersistentStateDir: utils.Some(dir),
		}
	}
	makeDataState := func() *data.State {
		return utils.OrPanic1(data.NewState(
			&data.Config{Committee: committee},
			utils.OrPanic1(data.NewDataWAL(utils.None[string](), committee)),
		))
	}

	view0 := types.View{Index: 0, Number: 0}
	pqc0 := makePrepareQC(keys, types.GenProposalAt(rng, view0))

	// Session 1: push PrepareQC + TimeoutQC, let runOutputs persist.
	err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		s, err := NewState(makeCfg(), makeDataState())
		if err != nil {
			return fmt.Errorf("NewState: %w", err)
		}
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s.Run(ctx)) })

		if err := s.pushPrepareQC(ctx, pqc0); err != nil {
			return fmt.Errorf("pushPrepareQC: %w", err)
		}
		tqc0 := makeTimeoutQC(keys, view0, utils.Some(pqc0))
		if err := s.pushTimeoutQC(ctx, tqc0); err != nil {
			return fmt.Errorf("pushTimeoutQC: %w", err)
		}
		// Wait until runOutputs has processed the state change (and persisted it).
		if _, err := s.myView.Wait(ctx, func(vs types.ViewSpec) bool {
			return vs.TimeoutQC.IsPresent()
		}); err != nil {
			return fmt.Errorf("wait for persist: %w", err)
		}
		return nil
	})
	require.NoError(t, err)

	// Session 2: restart from persisted state, verify PrepareQC inheritance.
	err = scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
		s2, err := NewState(makeCfg(), makeDataState())
		if err != nil {
			return fmt.Errorf("NewState (restart): %w", err)
		}
		sc.SpawnBg(func() error { return utils.IgnoreCancel(s2.Run(ctx)) })

		view1 := types.View{Index: 0, Number: 1}
		if err := s2.voteTimeout(ctx, view1); err != nil {
			return fmt.Errorf("voteTimeout: %w", err)
		}
		tv, ok := s2.innerRecv.Load().TimeoutVote.Get()
		if !ok {
			return fmt.Errorf("TimeoutVote not present after restart")
		}
		if !testTimeoutVotePrepareQC(tv).IsPresent() {
			return fmt.Errorf("PrepareQC must be inherited from persisted TimeoutQC after restart")
		}
		return nil
	})
	require.NoError(t, err)
}
