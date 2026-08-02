package avail

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// WaitForCurrentEpoch waits until avail Current reaches exactly epoch i.
// If Current has already moved past i, returns ErrPruned.
func (s *State) WaitForCurrentEpoch(ctx context.Context, i types.EpochIndex) (*types.Epoch, error) {
	epoch, err := s.epoch.Wait(ctx, func(epoch *types.Epoch) bool {
		return i <= epoch.EpochIndex()
	})
	if err != nil {
		return nil, err
	}
	if epoch.EpochIndex() != i {
		return nil, types.ErrPruned
	}
	return epoch, nil
}

// waitForAppEpoch waits until roadIdx is in the App admit window: Current, or
// the App-tip epoch when App lags (Prev). Soft-returns ErrPruned when behind
// that window (caller drops). Parks while roadIdx is still ahead of Current.
func (s *State) waitForAppEpoch(ctx context.Context, roadIdx types.RoadIndex) (*types.Epoch, error) {
	for {
		if _, err := s.epoch.Wait(ctx, func(cur *types.Epoch) bool {
			return roadIdx < cur.RoadRange().Next
		}); err != nil {
			return nil, err
		}
		for inner := range s.inner.Lock() {
			cur := inner.epoch.Load()
			if roadIdx >= cur.RoadRange().Next {
				break // Current moved; retry Wait
			}
			if cur.RoadRange().Has(roadIdx) {
				return cur, nil
			}
			if tip, ok := inner.app.tip.Get(); ok && tip.Epoch != nil && tip.Epoch.RoadRange().Has(roadIdx) {
				return tip.Epoch, nil
			}
			return nil, types.ErrPruned
		}
	}
}

// runAdvanceEpoch is the sole post-construction writer of Current. Seal leashes:
//   - prune: App tip epoch >= Current
//   - execution: registry has Current+1
//
// N+1 CommitQCs park on WaitForCurrentEpoch(N+1) until this advances Current.
func (s *State) runAdvanceEpoch(ctx context.Context) error {
	return s.epoch.Iter(ctx, func(ctx context.Context, epoch *types.Epoch) error {
		for inner, ctrl := range s.inner.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				if inner.commits.qcs.next < epoch.RoadRange().Next {
					return false
				}
				tip, ok := inner.app.tip.Get()
				return ok && tip.Epoch != nil && tip.Epoch.EpochIndex() >= epoch.EpochIndex()
			}); err != nil {
				return err
			}
		}
		next, err := s.data.Registry().WaitForEpoch(ctx, epoch.EpochIndex()+1)
		if err != nil {
			return err
		}
		for inner, ctrl := range s.inner.Lock() {
			if inner.advanceEpoch(next) {
				ctrl.Updated()
			}
		}
		return nil
	})
}
