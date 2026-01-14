package consensus

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

type inner struct {
	viewSpec types.ViewSpec

	prepareVote utils.Option[*types.Signed[*types.PrepareVote]]
	timeoutVote utils.Option[*types.FullTimeoutVote]

	prepareQC  utils.Option[*types.PrepareQC]
	commitVote utils.Option[*types.Signed[*types.CommitVote]]
}

func (i *inner) View() types.View {
	return i.viewSpec.View()
}

func (s *State) pushCommitQC(qc *types.CommitQC) error {
	if i := s.inner.Load(); i.viewSpec.View().Index > qc.Proposal().Index() {
		return nil
	}
	if err := qc.Verify(s.Data().Committee()); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	s.inner.Update(func(i inner) (inner, bool) {
		if qc.Proposal().Index() < types.NextIndexOpt(i.viewSpec.CommitQC) {
			return inner{}, false
		}
		return inner{
			viewSpec: types.ViewSpec{
				CommitQC:  utils.Some(qc),
				TimeoutQC: utils.None[*types.TimeoutQC](),
			},
		}, true
	})
	return nil
}

func (s *State) waitForView(ctx context.Context, view types.View) (types.ViewSpec, error) {
	return s.myView.Wait(ctx, func(v types.ViewSpec) bool { return !v.View().Less(view) })
}

func (s *State) pushTimeoutQC(ctx context.Context, qc *types.TimeoutQC) error {
	i, err := s.inner.Wait(ctx, func(i inner) bool { return i.View().Index >= qc.View().Index })
	if err != nil {
		return err
	}
	if qc.View().Less(i.View()) {
		return nil
	}
	if err := qc.Verify(s.Data().Committee(), i.viewSpec.CommitQC); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	s.inner.Update(func(i inner) (inner, bool) {
		if qc.View().Less(i.View()) {
			return inner{}, false
		}
		i.viewSpec.TimeoutQC = utils.Some(qc)
		return inner{viewSpec: i.viewSpec}, true
	})
	return nil
}

// pushProposal processes an unverified FullProposal message.
func (s *State) pushProposal(ctx context.Context, proposal *types.FullProposal) error {
	// Wait for view.
	vs, err := s.waitForView(ctx, proposal.View())
	if err != nil {
		return err
	}
	// Verify message.
	if vs.View() != proposal.View() {
		return nil
	}
	if err := proposal.Verify(s.Data().Committee(), vs); err != nil {
		return fmt.Errorf("proposal.Verify(): %w", err)
	}
	// Update.
	s.inner.Update(func(i inner) (inner, bool) {
		if i.View() != proposal.View() {
			return i, false
		}
		if _, ok := i.timeoutVote.Get(); ok {
			return i, false
		}
		if _, ok := i.prepareVote.Get(); ok {
			return i, false
		}
		v := types.Sign(s.cfg.Key, types.NewPrepareVote(proposal.Proposal().Msg()))
		i.prepareVote = utils.Some(v)
		return i, true
	})
	return nil
}

func (s *State) pushPrepareQC(ctx context.Context, qc *types.PrepareQC) error {
	// Wait for view.
	i, err := s.waitForView(ctx, qc.Proposal().View())
	if err != nil {
		return err
	}
	// Verify message.
	if i.View() != qc.Proposal().View() {
		return nil
	}
	if err := qc.Verify(s.Data().Committee()); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	// Update.
	s.inner.Update(func(i inner) (inner, bool) {
		if i.View() != qc.Proposal().View() {
			return i, false
		}
		if _, ok := i.timeoutVote.Get(); ok {
			return i, false
		}
		if _, ok := i.prepareQC.Get(); ok {
			return i, false
		}
		i.prepareQC = utils.Some(qc)
		v := types.Sign(s.cfg.Key, types.NewCommitVote(qc.Proposal()))
		i.commitVote = utils.Some(v)
		return i, true
	})
	return nil
}

func (s *State) voteTimeout(ctx context.Context, view types.View) error {
	// Wait for view.
	_, err := s.waitForView(ctx, view)
	if err != nil {
		return err
	}
	s.inner.Update(func(i inner) (inner, bool) {
		if i.View() != view {
			return i, false
		}
		if _, ok := i.timeoutVote.Get(); ok {
			return i, false
		}
		i.timeoutVote = utils.Some(types.NewFullTimeoutVote(s.cfg.Key, view, i.prepareQC))
		return i, true
	})
	return nil
}
