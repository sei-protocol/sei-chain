package epoch

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// epochLength is the number of road indices per epoch.
// TODO: move on-chain when epoch length becomes configurable.
const epochLength types.RoadIndex = 108_000

// Registry stores the retained execution-derived epochs.
type Registry struct {
	firstRoad        types.RoadIndex
	genesisTimestamp time.Time
	state            utils.Mutex[*utils.AtomicSend[*persistedRegistry]]
	stateRecv        utils.AtomicRecv[*persistedRegistry]
}

// NewRegistry returns a registry for committee, starting at firstRoad at genesisTimestamp.
// stored, when set, replaces the genesis epochs with the registry those bytes decode to.
func NewRegistry(
	committee *types.Committee,
	firstRoad types.RoadIndex,
	genesisTimestamp time.Time,
	stored utils.Option[[]byte],
) (*Registry, error) {
	r := &Registry{
		firstRoad:        firstRoad,
		genesisTimestamp: genesisTimestamp,
	}
	state := newGenesisRegistry(committee, firstRoad)
	if raw, ok := stored.Get(); ok {
		var err error
		if state, err = decodeStored(raw, r.roadRange); err != nil {
			return nil, err
		}
	}
	stateSend := new(utils.NewAtomicSend(state))
	r.state = utils.NewMutex(stateSend)
	r.stateRecv = stateSend.Subscribe()
	return r, nil
}

// decodeStored decodes registry bytes from storage and checks each retained
// epoch's road layout and that its committee follows the one before it.
func decodeStored(
	raw []byte,
	roadRange func(types.EpochIndex) types.RoadRange,
) (*persistedRegistry, error) {
	state, err := decodePersisted(raw)
	if err != nil {
		return nil, err
	}
	for idx := state.liveRange.First; idx < state.liveRange.Next; idx++ {
		epoch := state.live[idx]
		if got, want := epoch.RoadRange(), roadRange(idx); got != want {
			return nil, fmt.Errorf("epoch registry: epoch %d roads %v, want %v", idx, got, want)
		}
		if state.liveRange.First < idx {
			if err := checkDerived(state.live[idx-1], epoch); err != nil {
				return nil, fmt.Errorf("epoch registry: epoch %d: %w", idx, err)
			}
		}
	}
	return state, nil
}

func (r *Registry) roadRange(idx types.EpochIndex) types.RoadRange {
	first := r.firstRoad + types.RoadIndex(idx)*epochLength
	return types.RoadRange{First: first, Next: first + epochLength}
}

// LastRoad returns the last road index of epoch idx.
func (r *Registry) LastRoad(idx types.EpochIndex) types.RoadIndex {
	return r.roadRange(idx).Next - 1
}

// IndexForRoad returns the epoch index containing road. Road must not precede
// the genesis epoch's road range.
func (r *Registry) IndexForRoad(road types.RoadIndex) types.EpochIndex {
	return types.EpochIndex((road - r.firstRoad) / epochLength)
}

// ClosingEpoch returns the epoch that road ends, if road is that epoch's last
// road. Otherwise None.
func (r *Registry) ClosingEpoch(road types.RoadIndex) utils.Option[types.EpochIndex] {
	if road < r.firstRoad {
		return utils.None[types.EpochIndex]()
	}
	idx := r.IndexForRoad(road)
	if road != r.LastRoad(idx) {
		return utils.None[types.EpochIndex]()
	}
	return utils.Some(idx)
}

// GenesisTimestamp returns the genesis epoch timestamp.
func (r *Registry) GenesisTimestamp() time.Time {
	return r.genesisTimestamp
}

// Epoch returns the registered epoch at idx.
// It returns ErrPruned if idx has been dropped by PruneBefore.
func (r *Registry) Epoch(idx types.EpochIndex) (*types.Epoch, error) {
	s := r.stateRecv.Load()
	if idx < s.liveRange.First {
		return nil, fmt.Errorf("epoch %d: %w", idx, types.ErrPruned)
	}
	if s.liveRange.Next <= idx {
		return nil, fmt.Errorf("epoch %d: %w", idx, types.ErrNotFound)
	}
	return s.live[idx], nil
}

// EpochAt returns the registered epoch containing roadIndex.
func (r *Registry) EpochAt(roadIndex types.RoadIndex) (*types.Epoch, error) {
	firstRoad := r.firstRoad
	if roadIndex < firstRoad {
		return nil, fmt.Errorf("road %d: %w", roadIndex, types.ErrPruned)
	}
	return r.Epoch(r.IndexForRoad(roadIndex))
}

// FirstRoadState is the persisted registry and the committee of the epoch a
// first road opens.
type FirstRoadState struct {
	Registry  []byte
	Committee []byte
}

// FirstRoadState returns the registry window and the committee of the epoch road
// opens, when road is that epoch's first road, and none for every other road.
// The window is the epoch before that one, that epoch, and the epoch after it;
// older retained epochs are left out. It returns an error unless the retained
// epochs reach back to the epoch before and end just after the epoch after it.
func (r *Registry) FirstRoadState(road types.RoadIndex) (utils.Option[FirstRoadState], error) {
	if road == 0 {
		return utils.None[FirstRoadState](), nil
	}
	closing, ok := r.ClosingEpoch(road - 1).Get()
	if !ok {
		return utils.None[FirstRoadState](), nil
	}
	entering := closing + 1
	s := r.stateRecv.Load()
	if s.liveRange.First > entering-1 || s.liveRange.Next != entering+2 {
		return utils.None[FirstRoadState](), fmt.Errorf("epoch registry at road %d: live [%d,%d), want first <= %d and next == %d",
			road, s.liveRange.First, s.liveRange.Next, entering-1, entering+2)
	}
	window := s.pruneBefore(entering - 1)
	committee := hashable.MarshalCanonical(types.CommitteeConv.Encode(s.live[entering].Committee()))
	registry := hashable.MarshalCanonical(persistedRegistryConv.Encode(window))
	return utils.Some(FirstRoadState{Registry: registry, Committee: committee}), nil
}

// TryPushEpoch derives and publishes the next epoch when n closes the required execution epoch.
// Earlier roads are ignored; roads beyond the required boundary return an error.
func (r *Registry) TryPushEpoch(n types.RoadIndex, weights map[types.PublicKey]uint64) error {
	for state := range r.state.Lock() {
		s := state.Load()
		if next, err := s.tryPushEpoch(n, weights); err != nil {
			return err
		} else if next != s {
			state.Store(next)
		}
	}
	return nil
}

// PruneBefore drops retained epochs below keep, retaining the latest live epoch.
// Pruning only moves forward.
// TODO: archive nodes need special handling (never prune epochs).
func (r *Registry) PruneBefore(keep types.EpochIndex) error {
	for state := range r.state.Lock() {
		s := state.Load()
		if next := s.pruneBefore(keep); next != s {
			state.Store(next)
		}
	}
	return nil
}

// WaitForEpoch blocks until epoch i is registered.
func (r *Registry) WaitForEpoch(ctx context.Context, i types.EpochIndex) (*types.Epoch, error) {
	state, err := r.stateRecv.Wait(ctx, func(s *persistedRegistry) bool { return i < s.liveRange.Next })
	if err != nil {
		return nil, err
	}
	if i < state.liveRange.First {
		return nil, fmt.Errorf("epoch %d: %w", i, types.ErrPruned)
	}
	return state.live[i], nil
}
