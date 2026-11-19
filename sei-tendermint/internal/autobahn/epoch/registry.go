package epoch

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// DefaultEpochLength is the default number of road blocks per epoch.
const DefaultEpochLength types.RoadHeight = 108_000

// Registry stores the retained execution-derived epochs.
type Registry struct {
	firstHeight      types.RoadHeight
	epochLength      types.RoadHeight
	genesisTimestamp time.Time
	state            utils.Mutex[*utils.AtomicSend[*persistedRegistry]]
	stateRecv        utils.AtomicRecv[*persistedRegistry]
	persister        persist.Persister[*pb.PersistedEpochRegistry]
}

// NewRegistry opens a registry using the genesis committee, road origin, timestamp, and epoch length.
// Without persisted state it starts with epochs 0 and 1. stateDir None disables persistence.
func NewRegistry(
	committee *types.Committee,
	firstHeight types.RoadHeight,
	genesisTimestamp time.Time,
	epochLength types.RoadHeight,
	stateDir utils.Option[string],
) (*Registry, error) {
	if epochLength == 0 {
		return nil, fmt.Errorf("epoch length must be greater than 0")
	}
	persister, loaded, err := openEpochSnapshot(stateDir)
	if err != nil {
		return nil, err
	}
	r := &Registry{
		firstHeight:      firstHeight,
		epochLength:      epochLength,
		genesisTimestamp: genesisTimestamp,
		persister:        persister,
	}
	state := newGenesisRegistry(committee, firstHeight, epochLength)
	if raw, ok := loaded.Get(); ok {
		if state, err = persistedRegistryConv.DecodeReq(raw); err != nil {
			return nil, fmt.Errorf("restore epoch snapshot: %w", err)
		}
		for idx := state.liveRange.First; idx < state.liveRange.Next; idx++ {
			epoch := state.live[idx]
			if got, want := epoch.RoadRange(), r.roadRange(idx); got != want {
				return nil, fmt.Errorf("restore epoch snapshot: epoch %d roads %v, want %v", idx, got, want)
			}
			if state.liveRange.First < idx {
				if err := checkDerived(state.live[idx-1], epoch); err != nil {
					return nil, fmt.Errorf("restore epoch snapshot: epoch %d: %w", idx, err)
				}
			}
		}
		for idx := range types.EpochIndex(2) {
			if epoch, ok := state.live[idx]; ok && !epoch.Committee().Equal(committee) {
				return nil, fmt.Errorf("restore epoch snapshot: epoch %d does not match genesis", idx)
			}
		}
	}
	stateSend := new(utils.NewAtomicSend(state))
	r.state = utils.NewMutex(stateSend)
	r.stateRecv = stateSend.Subscribe()
	return r, nil
}

func (r *Registry) roadRange(idx types.EpochIndex) types.RoadRange {
	first := r.firstHeight + types.RoadHeight(idx)*r.epochLength
	return types.RoadRange{First: first, Next: first + r.epochLength}
}

// LastRoad returns the last road height of epoch idx.
func (r *Registry) LastRoad(idx types.EpochIndex) types.RoadHeight {
	return r.roadRange(idx).Next - 1
}

// IndexForRoad returns the epoch index containing road. Road must not precede
// the genesis epoch's road range.
func (r *Registry) IndexForRoad(road types.RoadHeight) types.EpochIndex {
	return types.EpochIndex((road - r.firstHeight) / r.epochLength)
}

// ClosingEpoch returns the epoch that road ends, if road is that epoch's last
// road. Otherwise None.
func (r *Registry) ClosingEpoch(road types.RoadHeight) utils.Option[types.EpochIndex] {
	if road < r.firstHeight {
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

// EpochAt returns the registered epoch containing height.
func (r *Registry) EpochAt(height types.RoadHeight) (*types.Epoch, error) {
	firstHeight := r.firstHeight
	if height < firstHeight {
		return nil, fmt.Errorf("road %d: %w", height, types.ErrPruned)
	}
	return r.Epoch(r.IndexForRoad(height))
}

// commit persists and publishes next. Persistence failure leaves the current snapshot unchanged.
func (r *Registry) commit(state *utils.AtomicSend[*persistedRegistry], next *persistedRegistry) error {
	if err := r.persister.Persist(persistedRegistryConv.Encode(next)); err != nil {
		return fmt.Errorf("persist epoch registry: %w", err)
	}
	state.Store(next)
	return nil
}

// TryPushEpoch derives and persists the next epoch when n closes the required execution epoch.
// Earlier roads are ignored; roads beyond the required boundary return an error.
func (r *Registry) TryPushEpoch(n types.RoadHeight, weights map[types.PublicKey]uint64) error {
	for state := range r.state.Lock() {
		s := state.Load()
		if next, err := s.tryPushEpoch(n, weights); err != nil {
			return err
		} else if next != s {
			return r.commit(state, next)
		}
	}
	return nil
}

// PruneBefore drops persisted epochs below keep, retaining the latest live epoch.
// Pruning only moves forward.
// TODO: archive nodes need special handling (never prune epochs).
func (r *Registry) PruneBefore(keep types.EpochIndex) error {
	for state := range r.state.Lock() {
		s := state.Load()
		if next := s.pruneBefore(keep); next != s {
			return r.commit(state, next)
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
