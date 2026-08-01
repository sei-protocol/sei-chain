package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// AppTip is avail's live App tip: AppQC + justifying CommitQC + that epoch
// (Prev when App lags Current). App owns and advances this tip; prune/persist
// only snapshots it for the durability watermark.
type AppTip struct {
	AppQC    *types.AppQC
	CommitQC *types.CommitQC
	Epoch    *types.Epoch
}

// Next is one past the AppQC road index (types.NextOpt).
func (t *AppTip) Next() types.RoadIndex { return t.AppQC.Next() }

// appProgress owns the App tip and AppVote accumulators.
type appProgress struct {
	tip   utils.Option[*AppTip]
	votes *queue[types.GlobalBlockNumber, appVotes]
}

// LastAppQC returns the AppQC peel of the live App tip.
func (s *State) LastAppQC() utils.Option[*types.AppQC] {
	for inner := range s.inner.Lock() {
		if tip, ok := inner.app.tip.Get(); ok {
			return utils.Some(tip.AppQC)
		}
		return utils.None[*types.AppQC]()
	}
	panic("unreachable")
}

// WaitForAppQC waits until the App tip is at idx or higher.
// Returns the tip AppQC and its matching CommitQC.
func (s *State) WaitForAppQC(ctx context.Context, idx types.RoadIndex) (*types.AppQC, *types.CommitQC, error) {
	for inner, ctrl := range s.inner.Lock() {
		for {
			if tip, ok := inner.app.tip.Get(); ok {
				if x := tip.AppQC.Proposal().RoadIndex(); x >= idx {
					return tip.AppQC, tip.CommitQC, nil
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
// Far-future roads park until CommitQC tip and App admit window catch up;
// behind-window votes soft-drop.
func (s *State) PushAppVote(ctx context.Context, v *types.Signed[*types.AppVote]) error {
	idx := v.Msg().Proposal().RoadIndex()
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return err
	}
	epoch, err := s.waitForAppEpoch(ctx, idx)
	if err != nil {
		if errors.Is(err, types.ErrPruned) {
			return nil
		}
		return err
	}
	if got, want := v.Msg().Proposal().EpochIndex(), epoch.EpochIndex(); got != want {
		return fmt.Errorf("appVote epoch_index %d, want %d", got, want)
	}
	qc, err := s.CommitQC(ctx, idx)
	if err != nil {
		if errors.Is(err, types.ErrPruned) {
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
		if idx < types.NextOpt(inner.app.tip) {
			return nil
		}
		n := v.Msg().Proposal().GlobalNumber()
		q := inner.app.votes
		for q.next <= n {
			q.pushBack(newAppVotes())
		}
		appQC, ok := q.q[n].pushVote(committee, v)
		if !ok {
			return nil
		}
		updated, err := inner.pushAppTip(&AppTip{AppQC: appQC, CommitQC: qc, Epoch: epoch})
		if err != nil {
			return err
		}
		if updated {
			ctrl.Updated()
		}
	}
	return nil
}

// PushAppQC requires a justifying CommitQC.
// Admits only in the App window (Current|App-tip/Prev); parks ahead, soft-drops
// behind. Advances the App tip (and thus the prune watermark) up to AppQC.
func (s *State) PushAppQC(ctx context.Context, appQC *types.AppQC, commitQC *types.CommitQC) error {
	for inner := range s.inner.Lock() {
		if types.NextOpt(inner.app.tip) >= appQC.Next() {
			return nil
		}
	}
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
	epoch, err := s.waitForAppEpoch(ctx, idx)
	if err != nil {
		if errors.Is(err, types.ErrPruned) {
			return nil
		}
		return err
	}
	if got, want := appQC.Proposal().EpochIndex(), epoch.EpochIndex(); got != want {
		return fmt.Errorf("appQC epoch_index %d, want %d", got, want)
	}
	if err := appQC.Verify(epoch); err != nil {
		return fmt.Errorf("appQC.Verify(): %w", err)
	}
	if err := commitQC.Verify(epoch); err != nil {
		return fmt.Errorf("commitQC.Verify(): %w", err)
	}
	tip := &AppTip{AppQC: appQC, CommitQC: commitQC, Epoch: epoch}
	for inner, ctrl := range s.inner.Lock() {
		updated, err := inner.pushAppTip(tip)
		if err != nil {
			return err
		}
		if updated {
			ctrl.Updated()
		}
	}
	return nil
}
