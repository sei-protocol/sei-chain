package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// waitForCommitQC waits until commitqc queue reaches idx.
// CommitQC at idx is NOT guaranteed to be persisted yet. 
func (s *State) waitForCommitQC(ctx context.Context, idx types.RoadIndex) error {
	for inner,ctrl := range s.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return idx < inner.commits.qcs.next }); err!=nil {
			return err
		}
	}
	return nil
}

// Fetches CommitQC and a matching epoch. They are NOT guaranteed to be persisted.
func (s *State) commitQCAndEpoch(ctx context.Context, idx types.RoadIndex) (*types.CommitQC, *types.Epoch, error) {
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return nil, nil, err
	}
	for inner := range s.inner.Lock() {
		if idx < inner.commits.qcs.first {
			return nil, nil, types.ErrPruned
		}
		qc := inner.commits.qcs.q[idx]
		if epoch := inner.epoch.Load(); epoch.EpochIndex()==qc.Proposal().EpochIndex() {
			return qc,epoch,nil
		}
		return qc,inner.app.anchor.OrPanic("missing anchor").Epoch,nil
	}
	panic("unreachable")
}

// CommitQC returns the CommitQC for the given index.
func (s *State) CommitQC(ctx context.Context, idx types.RoadIndex) (*types.CommitQC, error) {
	if err := s.waitForCommitQC(ctx, idx); err != nil {
		return nil, err
	}
	for inner := range s.inner.Lock() {
		if idx < inner.commits.qcs.first {
			return nil, types.ErrPruned
		}
		return inner.commits.qcs.q[idx], nil
	}
	panic("unreachable")
}

// PushCommitQC pushes qc to the commit queue.
// Blocks until all previous CommitQCs are available and State enters this qc's epoch.
// Silently drops qc if not needed.
// NOT guaranteed to be persisted yet.
func (s *State) PushCommitQC(ctx context.Context, qc *types.CommitQC) error {
	// Await previous CommitQC.
	if i := qc.Proposal().Index(); i>0 {
		if err:=s.waitForCommitQC(ctx,i-1); err!=nil {
			return err
		}
	}
	// Await Epoch.
	epoch, err := s.Epoch(ctx, qc.Proposal().EpochIndex())
	if err != nil {
		if errors.Is(err,types.ErrPruned); err!=nil {
			return nil
		}
		return err
	}
	// Verify qc.
	if err := qc.Verify(epoch); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	// Push.
	for inner, ctrl := range s.inner.Lock() {
		if inner.commits.push(qc) {
			ctrl.Updated()
		}
	}
	return nil
}

// fullCommitQC returns the FullCommitQC for road n.
func (s *State) fullCommitQC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, error) {
	qc, epoch, err := s.commitQCAndEpoch(ctx, n)
	if err != nil {
		return nil, err
	}
	var commitHeaders []*types.BlockHeader
	for lane := range epoch.Committee().Lanes().All() {
		headers, err := s.headers(ctx, qc.LaneRange(lane))
		if err != nil {
			return nil, err
		}
		commitHeaders = append(commitHeaders, headers...)
	}
	return types.NewFullCommitQC(qc, commitHeaders), nil
}
