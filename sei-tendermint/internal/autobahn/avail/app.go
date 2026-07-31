package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// appProgress owns the in-memory AppQC tip and AppVote accumulators.
type appProgress struct {
	anchor utils.Option[*PruneAnchor]
	votes *queue[types.GlobalBlockNumber, appVotes]
}

// LastAppQC returns the latest observed AppQC.
func (s *State) LastAppQC() utils.Option[*types.AppQC] {
	for inner := range s.inner.Lock() {
		if anchor,ok := inner.app.anchor.Get(); ok {
			return utils.Some(anchor.AppQC)
		}
	}
	return utils.None[*types.AppQC]()
}

// PushAppVote pushes an AppVote to the state.
// Same admit-then-verify as PushAppQC: far-future roads park until the duo
// and CommitQC tip catch up (one stream goroutine; does not block others).
func (s *State) PushAppVote(ctx context.Context, v *types.Signed[*types.AppVote]) error {
	qc,epoch,err := s.commitQCAndEpoch(ctx, v.Msg().Proposal().RoadIndex())
	if err != nil {
		if errors.Is(err,types.ErrPruned) {
			return nil
		}
		return err
	}
	if err := v.Msg().Proposal().Verify(qc); err != nil {
		return fmt.Errorf("invalid vote: %w", err)
	}
	committee := epoch.Committee()
	if err := v.VerifySig(committee); err != nil {
		return fmt.Errorf("v.VerifySig(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		// Early exit if not useful (we collect <=1 AppQC per road index).
		if qc.Index() < types.NextOpt(inner.app.anchor) {
			return nil
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
		// Anchor is always valid here.
		updated := utils.OrPanic1(inner.pushPruneAnchor(&PruneAnchor{AppQC: appQC, CommitQC: qc, Epoch: epoch}))
		if updated {
			ctrl.Updated()
		}
	}
	return nil
}

// PushAppQC requires a justifying CommitQC.
// Prunes state up to AppQC.Proposal().Index().
func (s *State) PushAppQC(ctx context.Context, appQC *types.AppQC, commitQC *types.CommitQC) error {
	// If epoch is from the future then we are unable to process AppQC. 
	// If epoch is pruned, then it is from the past and there is no point participating in the consensus.
	epoch, err := s.data.Registry().WaitForEpoch(ctx, appQC.Proposal().EpochIndex())
	if err != nil {
		if errors.Is(err,types.ErrPruned) {
			return nil
		}
		return err
	}
	// Check whether it is needed before verifying.
	for inner := range s.inner.Lock() {
		if types.NextOpt(inner.app.anchor) >= appQC.Next() {
			return nil
		}
	}
	// Verify appQC <-> commitQC consistency.
	if appQC.Proposal().RoadIndex() != commitQC.Proposal().Index() {
		return fmt.Errorf("mismatched QCs: appQC index %v, commitQC index %v", appQC.Proposal().RoadIndex(), commitQC.Proposal().Index())
	}
	if got, want := appQC.Proposal().EpochIndex(), commitQC.Proposal().EpochIndex(); got != want {
		return fmt.Errorf("appQC epoch_index %d != commitQC epoch_index %d", got, want)
	}
	if !commitQC.GlobalRange().Has(appQC.Proposal().GlobalNumber()) {
		return fmt.Errorf("appQC GlobalNumber not in commitQC range")
	}
	if err := appQC.Verify(epoch); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	if err := commitQC.Verify(epoch); err != nil {
		return fmt.Errorf("commitQC.Verify(): %w", err)
	}
	anchor := &PruneAnchor{AppQC: appQC, CommitQC: commitQC, Epoch: epoch}
	for inner, ctrl := range s.inner.Lock() {
		if utils.OrPanic1(inner.pushPruneAnchor(anchor)) {
			ctrl.Updated()
		}
	}
	return nil
}
