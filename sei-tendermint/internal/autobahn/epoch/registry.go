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

// epochLength is the number of road indices per epoch.
// TODO: move on-chain when epoch length becomes configurable.
const epochLength types.RoadIndex = 108_000

// Registry stores at least one live epoch and optional committees staged for activation.
type Registry struct {
	firstRoad        types.RoadIndex
	genesisTimestamp time.Time
	state            utils.Mutex[*utils.AtomicSend[*persistedRegistry]]
	stateRecv        utils.AtomicRecv[*persistedRegistry]
	persister        persist.Persister[*pb.PersistedEpochRegistry]
}

// NewRegistry opens a registry using the genesis committee, road origin, and timestamp.
// Without persisted state it starts with epochs 0 and 1. stateDir None disables persistence.
func NewRegistry(
	committee *types.Committee,
	firstRoad types.RoadIndex,
	genesisTimestamp time.Time,
	stateDir utils.Option[string],
) (*Registry, error) {
	persister, loaded, err := openEpochSnapshot(stateDir)
	if err != nil {
		return nil, err
	}
	r := &Registry{
		firstRoad:        firstRoad,
		genesisTimestamp: genesisTimestamp,
		persister:        persister,
	}
	state := newGenesisRegistry(committee, firstRoad)
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

		// TODO: a missing pending is restaged on restart from GetValidators() at app
		// tip, not stake(LastRoad(E)). Use the LastRoad validator set once it is
		// persisted in app state.
		if pending, ok := state.pending.Get(); ok {
			idx := pending.EpochIndex()
			if got, want := pending.RoadRange(), r.roadRange(idx); got != want {
				return nil, fmt.Errorf("restore epoch snapshot: pending epoch %d roads %v, want %v", idx, got, want)
			}
			if err := checkDerived(state.live[idx-1], pending); err != nil {
				return nil, fmt.Errorf("restore epoch snapshot: pending: %w", err)
			}
		}	
	}
	stateSend := new(utils.NewAtomicSend(state))
	r.state = utils.NewMutex(stateSend)
	r.stateRecv = stateSend.Subscribe()
	return r, nil
}

func (r *Registry) roadRange(idx types.EpochIndex) types.RoadRange {
	first := r.firstRoad + types.RoadIndex(idx)*epochLength
	return types.RoadRange{First: first, Next: first + epochLength}
}

// FirstRoad returns the genesis epoch's first road index.
func (r *Registry) FirstRoad() types.RoadIndex {
	return r.firstRoad
}

// LastRoad returns the last road index of epoch idx.
func (r *Registry) LastRoad(idx types.EpochIndex) types.RoadIndex {
	return r.roadRange(idx).Next - 1
}

// IndexForRoad returns the epoch index containing road. Road must not precede
// FirstRoad.
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

// commit persists and publishes next. Persistence failure leaves the current snapshot unchanged.
func (r *Registry) commit(state *utils.AtomicSend[*persistedRegistry], next *persistedRegistry) error {
	if err := r.persister.Persist(persistedRegistryConv.Encode(next)); err != nil {
		return fmt.Errorf("persist epoch registry: %w", err)
	}
	state.Store(next)
	return nil
}

// StageEpoch derives C_{endEpoch+2} from weights and persists it as pending.
// A no-op if that epoch is already staged or live with the same committee.
// An error if it conflicts, if the target is not live.Next, or if endEpoch+1
// is not live.
func (r *Registry) StageEpoch(endEpoch types.EpochIndex, weights map[types.PublicKey]uint64) error {
	for state := range r.state.Lock() {
		s := state.Load()
		if next, err := s.stageEpoch(endEpoch, weights); err != nil {
			return err
		} else if next != s {
			return r.commit(state, next)
		}
		return nil
	}
	panic("unreachable")
}

// Activate publishes the staged committee for idx.
// A no-op if idx is already live. An error if idx is not the staged epoch.
func (r *Registry) Activate(idx types.EpochIndex) error {
	for state := range r.state.Lock() {
		s := state.Load()
		if next, err := s.activate(idx); err != nil || next == s {
			return err
		} else if next != s {
			return r.commit(state, next)
		}
		return nil
	}
	panic("unreachable")
}

// Pending returns the staged epoch, or None.
func (r *Registry) Pending() utils.Option[*types.Epoch] {
	return r.stateRecv.Load().pending
}

// PruneBefore drops persisted epochs below keep, retaining the latest live epoch.
// Pruning only moves forward and preserves staged epochs.
// TODO: archive nodes need special handling (never prune epochs).
func (r *Registry) PruneBefore(keep types.EpochIndex) error {
	for state := range r.state.Lock() {
		s := state.Load()
		if next := s.pruneBefore(keep); next != s {
			return r.commit(state, next)
		}
		return nil
	}
	panic("unreachable")
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
