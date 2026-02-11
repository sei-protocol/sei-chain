package consensus

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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
	if i := s.innerRecv.Load(); qc.Proposal().Index() < i.viewSpec.View().Index {
		return nil
	}
	if err := qc.Verify(s.Data().Committee()); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for iSend := range s.inner.Lock() {
		i := iSend.Load()
		if qc.Proposal().Index() < i.viewSpec.View().Index {
			return nil
		}
		iSend.Store(inner{
			viewSpec: types.ViewSpec{
				CommitQC:  utils.Some(qc),
				TimeoutQC: utils.None[*types.TimeoutQC](),
			},
		})
	}
	return nil
}

func (s *State) waitForView(ctx context.Context, view types.View) (types.ViewSpec, error) {
	return s.myView.Wait(ctx, func(v types.ViewSpec) bool { return !v.View().Less(view) })
}

func (s *State) pushTimeoutQC(ctx context.Context, qc *types.TimeoutQC) error {
	i, err := s.innerRecv.Wait(ctx, func(i inner) bool { return i.View().Index >= qc.View().Index })
	if err != nil {
		return err
	}
	if qc.View().Less(i.View()) {
		return nil
	}
	if err := qc.Verify(s.Data().Committee(), i.viewSpec.CommitQC); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for isend := range s.inner.Lock() {
		i := isend.Load()
		if qc.View().Less(i.View()) {
			return nil
		}
		i.viewSpec.TimeoutQC = utils.Some(qc)
		isend.Store(inner{viewSpec: i.viewSpec})
	}
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
	for isend := range s.inner.Lock() {
		i := isend.Load()
		if i.View() != proposal.View() || i.timeoutVote.IsPresent() || i.prepareVote.IsPresent() {
			return nil
		}
		v := types.Sign(s.cfg.Key, types.NewPrepareVote(proposal.Proposal().Msg()))
		i.prepareVote = utils.Some(v)
		isend.Store(i)
	}
	return nil
}

func (s *State) pushPrepareQC(ctx context.Context, qc *types.PrepareQC) error {
	// Wait for view.
	vs, err := s.waitForView(ctx, qc.Proposal().View())
	if err != nil {
		return err
	}
	// Verify message.
	if vs.View() != qc.Proposal().View() {
		return nil
	}
	if err := qc.Verify(s.Data().Committee()); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	// Update.
	for isend := range s.inner.Lock() {
		i := isend.Load()
		if i.View() != qc.Proposal().View() || i.timeoutVote.IsPresent() || i.prepareQC.IsPresent() {
			return nil
		}
		i.prepareQC = utils.Some(qc)
		v := types.Sign(s.cfg.Key, types.NewCommitVote(qc.Proposal()))
		i.commitVote = utils.Some(v)
		isend.Store(i)
	}
	return nil
}

func (s *State) voteTimeout(ctx context.Context, view types.View) error {
	// Wait for view.
	if _, err := s.waitForView(ctx, view); err != nil {
		return err
	}
	for isend := range s.inner.Lock() {
		i := isend.Load()
		if i.View() != view || i.timeoutVote.IsPresent() {
			return nil
		}
		i.timeoutVote = utils.Some(types.NewFullTimeoutVote(s.cfg.Key, view, i.prepareQC))
		isend.Store(i)
	}
	return nil
}
