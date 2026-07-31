package avail

import (
	"context"
	"errors"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// waitForEpoch wait for epoch to advance to roadIdx.
// Returned EpochDuo may be past roadIdx.
func (s *State) Epoch(ctx context.Context, i types.EpochIndex) (*types.Epoch, error) {
	epoch,err := s.epoch.Wait(ctx, func(epoch *types.Epoch) bool { return i <= epoch.EpochIndex() })
	if err!=nil { return nil, err }
	if epoch.EpochIndex()!=i { return nil,types.ErrPruned }
	return epoch,nil
}

// runAdvanceEpoch is the sole post-construction writer of epochDuo. When
// commitQCs tip passes Current's last road, seal leashes have already been
// satisfied at PushCommitQC/PushAppQC admit; this waits for tip, re-checks
// leashes (no-op if already met), then advances. N+1 CommitQCs park on
// waitForEpoch until the duo slides.
func (s *State) runAdvanceEpoch(ctx context.Context) error {
	return s.epoch.Iter(ctx, func(ctx context.Context, epoch *types.Epoch) error {
		for inner, ctrl := range s.inner.Lock() {
			return ctrl.WaitUntil(ctx,func() bool {
				// All commits of the current epoch are required.
				if inner.commits.qcs.next < epoch.RoadRange().Next {
					return false
				}
				anchor,ok := inner.app.anchor.Get()
				// Anchor in the current epoch is required.
				return ok && anchor.Epoch.EpochIndex() >= epoch.EpochIndex()
			})
		}
		epoch,err:=s.data.Registry().WaitForEpoch(ctx,epoch.EpochIndex()+1)
		if err!=nil {
			if errors.Is(err,types.ErrPruned); err!=nil {
				return nil
			}
			return err
		}
		for inner,ctrl := range s.inner.Lock() {
			if inner.advanceEpoch(epoch) {
				ctrl.Updated()
			}
		}
		return nil
	})
}
