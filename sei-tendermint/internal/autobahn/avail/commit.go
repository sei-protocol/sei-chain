package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// waitForCommitTip waits until the durable CommitQC tip has advanced past idx.
func (s *State) waitForCommitTip(ctx context.Context, idx types.RoadIndex) error {
	_, err := s.LastCommitQC().Wait(ctx, func(qc utils.Option[*types.CommitQC]) bool {
		return types.NextIndexOpt(qc) > idx
	})
	return err
}

// waitForCommitQCAndEpoch waits for the CommitQC at idx and returns it with the
// epoch needed to verify it: Current when the QC is in Current, otherwise the
// App-tip epoch (Prev).
func (s *State) waitForCommitQCAndEpoch(ctx context.Context, idx types.RoadIndex) (*types.CommitQC, *types.Epoch, error) {
	qc, err := s.WaitForCommitQC(ctx, idx)
	if err != nil {
		return nil, nil, err
	}
	for inner := range s.inner.Lock() {
		cur := inner.epoch.Load()
		if cur.EpochIndex() == qc.Proposal().EpochIndex() {
			return qc, cur, nil
		}
		tip, ok := inner.app.tip.Get()
		if !ok || tip.Epoch == nil {
			return nil, nil, fmt.Errorf("commitQC epoch %d needs App-tip epoch", qc.Proposal().EpochIndex())
		}
		return qc, tip.Epoch, nil
	}
	panic("unreachable")
}

// WaitForCommitQC waits until the durable tip covers idx, then returns that
// CommitQC. Returns ErrPruned if the road was pruned after the tip advanced.
func (s *State) WaitForCommitQC(ctx context.Context, idx types.RoadIndex) (*types.CommitQC, error) {
	if err := s.waitForCommitTip(ctx, idx); err != nil {
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

// PushCommitQC admits qc for Current only (too early waits; stale drops).
// N+1 CommitQCs park on Epoch until runAdvanceEpoch slides Current.
// Seal leashes (App anchor + registry N+1) gate that advance, not this admit.
func (s *State) PushCommitQC(ctx context.Context, qc *types.CommitQC) error {
	if i := qc.Proposal().Index(); i > 0 {
		if err := s.waitForCommitTip(ctx, i-1); err != nil {
			return err
		}
	}
	epoch, err := s.Epoch(ctx, qc.Proposal().EpochIndex())
	if err != nil {
		if errors.Is(err, types.ErrPruned) {
			return nil
		}
		return err
	}
	if err := qc.Verify(epoch); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	for inner, ctrl := range s.inner.Lock() {
		if inner.commits.push(qc) {
			ctrl.Updated()
		}
	}
	return nil
}

// fullCommitQC returns the FullCommitQC for road n.
func (s *State) fullCommitQC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, error) {
	qc, epoch, err := s.waitForCommitQCAndEpoch(ctx, n)
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
