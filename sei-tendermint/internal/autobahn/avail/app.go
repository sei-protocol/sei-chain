package avail

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// appProgress owns the in-memory AppQC tip and AppVote accumulators.
type appProgress struct {
	latestAppQC utils.Option[*types.AppQC]
	votes       *queue[types.GlobalBlockNumber, appVotes]
}

// LastAppQC returns the latest observed AppQC.
func (s *State) LastAppQC() utils.Option[*types.AppQC] {
	for inner := range s.inner.Lock() {
		return inner.app.latestAppQC
	}
	panic("unreachable")
}

// WaitForAppQC waits until there is an AppQC for the given index or higher.
// Returns this AppQC and the corresponding CommitQC.
// Together they provide enough information to prune the availability state.
func (s *State) WaitForAppQC(ctx context.Context, idx types.RoadIndex) (*types.AppQC, *types.CommitQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		for {
			if appQC, ok := inner.app.latestAppQC.Get(); ok {
				if x := appQC.Proposal().RoadIndex(); x >= idx && inner.commits.qcs.next > x {
					return appQC, inner.commits.qcs.q[x], nil
				}
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, nil, err
			}
		}
	}
	panic("unreachable")
}

// PushAppVote pushes an AppVote to the state.
// Same admit-then-verify as PushAppQC: far-future roads park until the duo
// and CommitQC tip catch up (one stream goroutine; does not block others).
func (s *State) PushAppVote(ctx context.Context, v *types.Signed[*types.AppVote]) error {
	idx := v.Msg().Proposal().RoadIndex()
	// A vote may arrive before its CommitQC advances the tip.
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return err
	}
	// Too-early roads (ahead of Prev|Current) backpressure; too-late are dropped.
	admitted, err := s.waitForEpochDuoOrDropStale(ctx, "AppVote", idx)
	if err != nil {
		return err
	}
	duo, ok := admitted.Get()
	if !ok {
		return nil
	}
	ep := utils.OrPanic1(duo.EpochForRoad(idx))
	if got, want := v.Msg().Proposal().EpochIndex(), ep.EpochIndex(); got != want {
		return fmt.Errorf("appVote epoch_index %d, want %d", got, want)
	}
	committee := ep.Committee()
	if err := v.VerifySig(committee); err != nil {
		return fmt.Errorf("v.VerifySig(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		// Early exit if not useful (we collect <=1 AppQC per road index).
		if idx < types.NextOpt(inner.app.latestAppQC) {
			return nil
		}
		// Verify the vote against the CommitQC.
		qc := inner.commits.qcs.q[idx]
		if err := v.Msg().Proposal().Verify(qc); err != nil {
			return fmt.Errorf("invalid vote: %w", err)
		}
		// Push the vote.
		n := v.Msg().Proposal().GlobalNumber()
		q := inner.app.votes
		for q.next <= n {
			q.pushBack(newAppVotes())
		}
		appQC, ok := q.q[n].pushVote(committee, v)
		if !ok {
			return nil
		}
		updated, err := inner.pushPruneAnchor(&PruneAnchor{AppQC: appQC, CommitQC: qc})
		if err != nil {
			return err
		}
		if updated {
			ctrl.Updated()
		}
	}
	return nil
}

// PushAppQC requires a justifying CommitQC. Epoch slide is async in
// runAdvanceEpoch (same as PushCommitQC). Prune before insert so latestAppQC is
// visible before the advance task observes the new tip.
//
// Same admit-then-verify as PushCommitQC.
func (s *State) PushAppQC(ctx context.Context, appQC *types.AppQC, commitQC *types.CommitQC) error {
	// Check whether it is needed before verifying.
	for inner := range s.inner.Lock() {
		if types.NextOpt(inner.app.latestAppQC) > appQC.Proposal().RoadIndex() {
			return nil
		}
	}
	// Pair consistency only; ahead-of-window still waits in waitForEpochDuo.
	if appQC.Proposal().RoadIndex() != commitQC.Proposal().Index() {
		return fmt.Errorf("mismatched QCs: appQC index %v, commitQC index %v", appQC.Proposal().RoadIndex(), commitQC.Proposal().Index())
	}
	if got, want := appQC.Proposal().EpochIndex(), commitQC.Proposal().EpochIndex(); got != want {
		return fmt.Errorf("appQC epoch_index %d != commitQC epoch_index %d", got, want)
	}
	if !commitQC.GlobalRange().Has(appQC.Proposal().GlobalNumber()) {
		return fmt.Errorf("appQC GlobalNumber not in commitQC range")
	}
	idx := commitQC.Proposal().Index()
	admitted, err := s.waitForEpochDuoOrDropStale(ctx, "AppQC", idx)
	if err != nil {
		return err
	}
	duo, ok := admitted.Get()
	if !ok {
		return nil
	}
	ep := utils.OrPanic1(duo.EpochForRoad(idx))
	if err := appQC.Verify(ep); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	if err := commitQC.Verify(ep); err != nil {
		return fmt.Errorf("commitQC.Verify(): %w", err)
	}
	// Seal CommitQC paired with this AppQC: incoming AppQC satisfies the prune
	// leash; still wait on the execution leash when idx closes Current.
	if duo.Current.RoadRange().Next-1 == idx && ep.EpochIndex() == duo.Current.EpochIndex() {
		if err := s.waitSealLeashes(ctx, duo.Current, idx, utils.Some(appQC.Proposal().EpochIndex())); err != nil {
			return err
		}
	}
	for inner, ctrl := range s.inner.Lock() {
		updated, err := inner.pushPruneAnchor(&PruneAnchor{AppQC: appQC, CommitQC: commitQC})
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}
		ctrl.Updated()
	}
	return nil
}
