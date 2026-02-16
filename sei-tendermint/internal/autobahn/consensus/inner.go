package consensus

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Persisted state file prefix.
const innerFile = "inner"

type inner struct {
	persistedInner
}

// newInner creates the inner state from persisted data loaded by newPersister.
// data is None on fresh start (persistence disabled or no prior state).
// Returns error if persisted state is corrupt (see persistedInner.validate).
func newInner(data utils.Option[[]byte], committee *types.Committee) (inner, error) {
	var persisted persistedInner

	if bz, ok := data.Get(); ok {
		decoded, err := innerProtoConv.Unmarshal(bz)
		if err != nil {
			return inner{}, fmt.Errorf("corrupt persisted state: %w", err)
		}
		persisted = *decoded
	}

	if err := persisted.validate(committee); err != nil {
		return inner{}, err
	}

	log.Info().Str("state", innerProtoConv.Encode(&persisted).String()).Msg("restored consensus state")

	return inner{persistedInner: persisted}, nil
}

func (s *State) pushCommitQC(qc *types.CommitQC) error {
	if i := s.innerRecv.Load(); qc.Proposal().Index() < i.View().Index {
		return nil
	}
	if err := qc.Verify(s.Data().Committee()); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for iSend := range s.inner.Lock() {
		i := iSend.Load()
		if qc.Proposal().Index() < i.View().Index {
			return nil
		}
		// CommitQC advances to new index; clear all state for new view
		iSend.Store(inner{persistedInner{
			CommitQC: utils.Some(qc),
		}})
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
	// Verify checks the invariant: TimeoutQC.View().Index == CommitQC.Index + 1
	if err := qc.Verify(s.Data().Committee(), i.CommitQC); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for isend := range s.inner.Lock() {
		i := isend.Load()
		if qc.View().Less(i.View()) {
			return nil
		}
		// TimeoutQC advances view number; clear votes and prepareQC (stale view).
		isend.Store(inner{persistedInner{
			CommitQC:  i.CommitQC,
			TimeoutQC: utils.Some(qc),
		}})
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
		if i.View() != proposal.View() || i.TimeoutVote.IsPresent() || i.PrepareVote.IsPresent() {
			return nil
		}
		v := types.Sign(s.cfg.Key, types.NewPrepareVote(proposal.Proposal().Msg()))
		i.PrepareVote = utils.Some(v)
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
		if i.View() != qc.Proposal().View() || i.TimeoutVote.IsPresent() || i.PrepareQC.IsPresent() {
			return nil
		}
		i.PrepareQC = utils.Some(qc)
		v := types.Sign(s.cfg.Key, types.NewCommitVote(qc.Proposal()))
		i.CommitVote = utils.Some(v)
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
		if i.View() != view || i.TimeoutVote.IsPresent() {
			return nil
		}
		v := types.NewFullTimeoutVote(s.cfg.Key, view, i.PrepareQC)
		i.TimeoutVote = utils.Some(v)
		isend.Store(i)
	}
	return nil
}
