package epoch

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// EpochLength is the number of road indices per epoch.
// TODO: move on-chain when epoch length becomes configurable.
const EpochLength types.RoadIndex = 108_000

// IndexForRoad returns the epoch index containing road.
func IndexForRoad(road types.RoadIndex) types.EpochIndex {
	return types.EpochIndex(road / EpochLength)
}

// FirstRoad returns the first road index of epoch idx.
func FirstRoad(idx types.EpochIndex) types.RoadIndex {
	return types.RoadIndex(idx) * EpochLength
}

// LastRoad returns the last road index of epoch idx.
func LastRoad(idx types.EpochIndex) types.RoadIndex {
	return FirstRoad(idx+1) - 1
}

// ClosingEpoch returns the epoch that road ends, if road is that epoch's last
// road. Otherwise None.
func ClosingEpoch(road types.RoadIndex) utils.Option[types.EpochIndex] {
	idx := IndexForRoad(road)
	if road != LastRoad(idx) {
		return utils.None[types.EpochIndex]()
	}
	return utils.Some(idx)
}

type registryState struct {
	m map[types.EpochIndex]*types.Epoch
	// live is the supported non-zero epoch indices [First, Next). Epoch 0 is
	// always kept for genesis metadata and is not represented here.
	live types.EpochRange
}

// dropped reports whether idx is below live.First. Epoch 0 is never dropped.
func (s *registryState) dropped(idx types.EpochIndex) bool {
	return idx != 0 && idx < s.live.First
}

// Registry stores the epochs whose committee is known: genesis epochs 0 and 1,
// plus epochs registered from execution (AddEpoch).
//
// An epoch is never registered speculatively. Consensus reads committees only
// from here, so a missing epoch parks epoch advance rather than admitting a
// guessed committee.
type Registry struct {
	state utils.Watch[*registryState]
}

// NewRegistry creates a Registry with genesis epochs 0 and 1 (genesis committee).
// Both use the genesis committee because C_2 is the first committee derivable
// from execution: it needs the stake at end(0).
func NewRegistry(
	committee *types.Committee,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	ep0 := types.NewEpoch(0, types.RoadRange{First: 0, Next: FirstRoad(1)}, genesisTimestamp, committee, firstBlock)
	ep1 := types.NewEpoch(1, types.RoadRange{First: FirstRoad(1), Next: FirstRoad(2)}, genesisTimestamp, committee, firstBlock)
	return &Registry{
		state: utils.NewWatch(&registryState{
			m:    map[types.EpochIndex]*types.Epoch{0: ep0, 1: ep1},
			live: types.EpochRange{First: 1, Next: 2},
		}),
	}, nil
}

// FirstBlock returns the genesis epoch's first global block number.
func (r *Registry) FirstBlock() types.GlobalBlockNumber {
	for s := range r.state.Lock() {
		return s.m[0].FirstBlock()
	}
	panic("unreachable")
}

// EpochByIndex returns the registered epoch at idx.
// It returns ErrPruned if idx has been dropped by PruneBefore.
func (r *Registry) EpochByIndex(idx types.EpochIndex) (*types.Epoch, error) {
	for s := range r.state.Lock() {
		if s.dropped(idx) {
			return nil, fmt.Errorf("epoch %d: %w", idx, types.ErrPruned)
		}
		ep, ok := s.m[idx]
		if !ok {
			return nil, fmt.Errorf("epoch %d not registered", idx)
		}
		return ep, nil
	}
	panic("unreachable")
}

// EpochAt returns the registered epoch containing roadIndex.
func (r *Registry) EpochAt(roadIndex types.RoadIndex) (*types.Epoch, error) {
	epochIdx := IndexForRoad(roadIndex)
	for s := range r.state.Lock() {
		if s.dropped(epochIdx) {
			return nil, fmt.Errorf("epoch %d (road %d): %w", epochIdx, roadIndex, types.ErrPruned)
		}
		if ep, ok := s.m[epochIdx]; ok {
			return ep, nil
		}
		return nil, fmt.Errorf("epoch %d (road %d) not registered", epochIdx, roadIndex)
	}
	panic("unreachable")
}

// ActivateEpoch registers the next vacant epoch after parent. parent is the
// epoch at the execution tip; the new committee is derived from it. Already-
// registered epochs are never modified. Pruned indices are skipped.
func (r *Registry) ActivateEpoch(
	parent types.EpochIndex,
	weights map[types.PublicKey]uint64,
	firstTimestamp time.Time,
	firstBlock types.GlobalBlockNumber,
) (*types.Epoch, error) {
	for s, ctrl := range r.state.Lock() {
		if s.dropped(parent) {
			return nil, fmt.Errorf("epoch %d: %w", parent, types.ErrPruned)
		}
		prev, ok := s.m[parent]
		if !ok {
			return nil, fmt.Errorf("epoch %d not registered", parent)
		}
		next := parent + 1
		for {
			if s.dropped(next) {
				next++
				continue
			}
			if _, ok := s.m[next]; !ok {
				break
			}
			next++
		}
		committee, err := prev.Committee().DeriveNext(weights, next)
		if err != nil {
			return nil, err
		}
		roads := types.RoadRange{First: FirstRoad(next), Next: FirstRoad(next + 1)}
		ep := types.NewEpoch(next, roads, firstTimestamp, committee, firstBlock)
		s.m[next] = ep
		r.extendLive(s, next)
		ctrl.Updated()
		return ep, nil
	}
	panic("unreachable")
}

// AddEpoch derives C_{endEpoch+2} from weights (stake at the last block of
// endEpoch) and registers it. A no-op if that epoch is already registered.
// endEpoch+1 must already be registered.
func (r *Registry) AddEpoch(endEpoch types.EpochIndex, weights map[types.PublicKey]uint64) error {
	target := endEpoch + 2
	for s, ctrl := range r.state.Lock() {
		if s.dropped(endEpoch + 1) {
			return fmt.Errorf("epoch %d: %w", endEpoch+1, types.ErrPruned)
		}
		prev, ok := s.m[endEpoch+1]
		if !ok {
			return fmt.Errorf("epoch %d not registered", endEpoch+1)
		}
		committee, err := prev.Committee().DeriveNext(weights, target)
		if err != nil {
			return fmt.Errorf("DeriveNext(%d): %w", target, err)
		}
		if s.dropped(target) {
			return fmt.Errorf("epoch %d: %w", target, types.ErrPruned)
		}
		if _, ok := s.m[target]; ok {
			return nil
		}
		roads := types.RoadRange{First: FirstRoad(target), Next: FirstRoad(target + 1)}
		s.m[target] = types.NewEpoch(target, roads, s.m[0].FirstTimestamp(), committee, s.m[0].FirstBlock())
		r.extendLive(s, target)
		ctrl.Updated()
		return nil
	}
	panic("unreachable")
}

func (r *Registry) extendLive(s *registryState, idx types.EpochIndex) {
	if idx >= s.live.Next {
		s.live.Next = idx + 1
	}
}

// PruneBefore drops supported epochs in [live.First, keep). Epoch 0 is kept
// for genesis metadata. keep is exclusive and only moves live.First forward.
func (r *Registry) PruneBefore(keep types.EpochIndex) {
	for s, ctrl := range r.state.Lock() {
		if keep <= s.live.First {
			return
		}
		for idx := s.live.First; idx < keep; idx++ {
			delete(s.m, idx)
		}
		s.live.First = keep
		if s.live.First > s.live.Next {
			s.live.Next = s.live.First
		}
		ctrl.Updated()
	}
}

// Live returns the supported non-zero epoch window [First, Next).
func (r *Registry) Live() types.EpochRange {
	for s := range r.state.Lock() {
		return s.live
	}
	panic("unreachable")
}

// WaitForEpoch blocks until epoch i is registered. For i > 1 that means waiting
// on execution of end(i-2).
func (r *Registry) WaitForEpoch(ctx context.Context, i types.EpochIndex) (*types.Epoch, error) {
	for inner, ctrl := range r.state.Lock() {
		for {
			if inner.dropped(i) {
				return nil, fmt.Errorf("epoch %d: %w", i, types.ErrPruned)
			}
			if ep, ok := inner.m[i]; ok {
				return ep, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	panic("unreachable")
}
