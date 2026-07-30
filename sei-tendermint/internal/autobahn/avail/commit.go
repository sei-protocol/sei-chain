package avail

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// waitForCommitQC waits until the durable CommitQC tip has advanced past idx.
func (s *State) waitForCommitQC(ctx context.Context, idx types.RoadIndex) error {
	_, err := s.LastCommitQC().Wait(ctx, func(qc utils.Option[*types.CommitQC]) bool {
		return types.NextIndexOpt(qc) > idx
	})
	return err
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

// PushCommitQC admits qc for Current only (too early waits; stale drops).
// Epoch slide is async in runAdvanceEpoch (tip may sit at Current.Next while
// Current still N; N+1 CommitQCs park on waitForEpoch until the duo advances).
//
// Seal (last road of Current): prune + execution leashes before admit
// (interlocking doc CommitQC admission).
//
// Admit-then-verify is intentional backpressure for ahead-of-window QCs.
func (s *State) PushCommitQC(ctx context.Context, qc *types.CommitQC) error {
	idx := qc.Proposal().Index()
	if idx > 0 {
		if err := s.waitForCommitQC(ctx, idx-1); err != nil {
			return err
		}
	}
	admitted, err := s.waitForEpochOrDropStale(ctx, "CommitQC", idx)
	if err != nil {
		return err
	}
	duo, ok := admitted.Get()
	if !ok {
		return nil
	}
	ep := duo.Current
	if err := qc.Verify(ep); err != nil {
		return fmt.Errorf("qc.Verify(): %w", err)
	}
	if err := s.waitSealLeashes(ctx, ep, idx, utils.None[types.EpochIndex]()); err != nil {
		return err
	}

	for inner, ctrl := range s.inner.Lock() {
		if !inner.commits.push(qc) {
			return nil
		}
		// persistedCommitQC advances only after durable persist (or no-op persister).
		ctrl.Updated()
		return nil
	}
	return nil
}

// fullCommitQC returns the FullCommitQC for road n.
// ErrRoadBeforeWindow → ErrPruned (export may jump ahead). ErrRoadAfterWindow hard-fails.
func (s *State) fullCommitQC(ctx context.Context, n types.RoadIndex) (*types.FullCommitQC, error) {
	qc, err := s.CommitQC(ctx, n)
	if err != nil {
		return nil, err
	}
	ep, err := s.epochDuo.Load().EpochForRoad(qc.Proposal().Index())
	if err != nil {
		if errors.Is(err, types.ErrRoadBeforeWindow) {
			return nil, types.ErrPruned
		}
		return nil, err
	}
	var commitHeaders []*types.BlockHeader
	for lane := range ep.Committee().Lanes().All() {
		headers, err := s.headers(ctx, qc.LaneRange(lane))
		if err != nil {
			return nil, err
		}
		commitHeaders = append(commitHeaders, headers...)
	}
	return types.NewFullCommitQC(qc, commitHeaders), nil
}
