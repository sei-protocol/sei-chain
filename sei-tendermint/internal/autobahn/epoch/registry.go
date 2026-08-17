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

type registryState struct {
	m      map[types.EpochIndex]*types.Epoch
	latest types.EpochIndex
}

// Registry stores activated epochs and placeholders.
type Registry struct {
	state utils.Watch[*registryState]
}

// NewRegistry creates a Registry with genesis epochs 0 and 1 (genesis committee).
func NewRegistry(
	committee *types.Committee,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	ep0 := types.NewEpoch(0, types.RoadRange{First: 0, Next: FirstRoad(1)}, genesisTimestamp, committee, firstBlock)
	ep1 := types.NewEpoch(1, types.RoadRange{First: FirstRoad(1), Next: FirstRoad(2)}, genesisTimestamp, committee, firstBlock)
	return &Registry{
		state: utils.NewWatch(&registryState{
			m:      map[types.EpochIndex]*types.Epoch{0: ep0, 1: ep1},
			latest: 0,
		}),
	}, nil
}

// SetupInitialEpochs registers placeholders covering commitQCs and the next epoch.
// With no CommitQCs this is a no-op (epochs 0 and 1 are already present).
func (r *Registry) SetupInitialEpochs(commitQCs utils.Option[types.RoadRange]) {
	span, ok := commitQCs.Get()
	if !ok {
		return
	}
	for s, ctrl := range r.state.Lock() {
		windowFirst := IndexForRoad(span.First)
		windowLast := IndexForRoad(span.Next - 1)
		r.ensureAround(s, span.First)
		for idx := windowFirst; idx <= windowLast; idx++ {
			r.ensureLocked(s, idx)
		}
		r.ensureAround(s, span.Next)
		// TODO: replace placeholders with execution-derived committee,
		// FirstTimestamp, and FirstBlock (genesis copies feed ViewSpec).
		r.ensureLocked(s, windowLast+1)
		ctrl.Updated()
	}
}

// FirstBlock returns the genesis epoch's first global block number.
func (r *Registry) FirstBlock() types.GlobalBlockNumber {
	for s := range r.state.Lock() {
		return s.m[0].FirstBlock()
	}
	panic("unreachable")
}

// GenesisTimestamp returns the genesis epoch timestamp.
func (r *Registry) GenesisTimestamp() time.Time {
	for s := range r.state.Lock() {
		return s.m[0].FirstTimestamp()
	}
	panic("unreachable")
}

// EpochByIndex returns the registered epoch at idx, if any.
func (r *Registry) EpochByIndex(idx types.EpochIndex) (*types.Epoch, bool) {
	for s := range r.state.Lock() {
		ep, ok := s.m[idx]
		return ep, ok
	}
	panic("unreachable")
}

// EpochAt returns the registered epoch containing roadIndex.
func (r *Registry) EpochAt(roadIndex types.RoadIndex) (*types.Epoch, error) {
	epochIdx := IndexForRoad(roadIndex)
	for s := range r.state.Lock() {
		if ep, ok := s.m[epochIdx]; ok {
			return ep, nil
		}
		return nil, fmt.Errorf("epoch %d (road %d) not registered", epochIdx, roadIndex)
	}
	panic("unreachable")
}

// LatestEpoch returns the ActivateEpoch tip.
func (r *Registry) LatestEpoch() *types.Epoch {
	for s := range r.state.Lock() {
		return s.m[s.latest]
	}
	panic("unreachable")
}

// ActivateEpoch registers the next vacant epoch after LatestEpoch with the given
// committee weights. Already-registered epochs are never modified. The first
// activation lands at index ≥ 2 (epochs 0 and 1 are always present). The new
// epoch's road range is FirstRoad(index)..FirstRoad(index+1).
func (r *Registry) ActivateEpoch(
	weights map[types.PublicKey]uint64,
	firstTimestamp time.Time,
	firstBlock types.GlobalBlockNumber,
) (*types.Epoch, error) {
	for s, ctrl := range r.state.Lock() {
		next := s.latest + 1
		for {
			if _, ok := s.m[next]; !ok {
				break
			}
			next++
		}
		prev := s.m[s.latest]
		committee, err := prev.Committee().DeriveNext(weights, next)
		if err != nil {
			return nil, err
		}
		roads := types.RoadRange{First: FirstRoad(next), Next: FirstRoad(next + 1)}
		ep := types.NewEpoch(next, roads, firstTimestamp, committee, firstBlock)
		s.m[next] = ep
		s.latest = next
		ctrl.Updated()
		return ep, nil
	}
	panic("unreachable")
}

// makeEpoch inserts a genesis-committee placeholder at epochIdx.
// Caller must hold r.state. Epochs 0 and 1 are always present (seeded at
// construction with the genesis committee); further epochs copy from epoch 0.
func (r *Registry) makeEpoch(s *registryState, epochIdx types.EpochIndex) *types.Epoch {
	ep0 := s.m[0]
	firstRoad := FirstRoad(epochIdx)
	epoch := types.NewEpoch(
		epochIdx,
		types.RoadRange{First: firstRoad, Next: FirstRoad(epochIdx + 1)},
		ep0.FirstTimestamp(),
		ep0.Committee(),
		ep0.FirstBlock(),
	)
	s.m[epochIdx] = epoch
	return epoch
}

// ensureLocked registers a genesis-committee placeholder for idx if missing.
// Caller must hold r.state.
func (r *Registry) ensureLocked(s *registryState, idx types.EpochIndex) {
	if _, ok := s.m[idx]; !ok {
		r.makeEpoch(s, idx)
	}
}

// ensureAround registers the epoch containing road and its predecessor.
// Caller must hold r.state.
func (r *Registry) ensureAround(s *registryState, road types.RoadIndex) {
	center := IndexForRoad(road)
	if center > 0 {
		r.ensureLocked(s, center-1)
	}
	r.ensureLocked(s, center)
}

// AdvanceIfNeeded registers epoch M+1 when roadIndex is LastRoad(M).
// M+2 is not seeded: tip may race to LastRoad(M+1) before AppQC, but
// ConsensusSpec withholds that next view until M+1's AppQC boundary fires
// AdvanceIfNeeded again.
func (r *Registry) AdvanceIfNeeded(roadIndex types.RoadIndex) {
	tipEpoch := IndexForRoad(roadIndex)
	if roadIndex != LastRoad(tipEpoch) {
		return
	}
	for s, ctrl := range r.state.Lock() {
		r.ensureLocked(s, tipEpoch+1)
		ctrl.Updated()
	}
}

// WaitForEpoch blocks until epoch i is registered.
func (r *Registry) WaitForEpoch(ctx context.Context, i types.EpochIndex) (*types.Epoch, error) {
	for inner, ctrl := range r.state.Lock() {
		for {
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
