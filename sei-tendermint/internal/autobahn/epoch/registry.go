package epoch

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus/persist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// epochLength is the number of road indices per epoch.
// TODO: move on-chain when epoch length becomes configurable.
const epochLength types.RoadIndex = 108_000

type registryState struct {
	persistedRegistry
	firstRoad      types.RoadIndex
	firstTimestamp time.Time
	// liveRange is the retained epoch range [First, Next) in live.
	liveRange types.EpochRange
}

// activate publishes the next epoch after validating its road range and committee continuity.
func (s *registryState) activate(epoch *types.Epoch) error {
	idx := epoch.EpochIndex()
	if idx != s.liveRange.Next {
		return fmt.Errorf("epoch %d cannot be activated, want %d", idx, s.liveRange.Next)
	}
	if got, want := epoch.RoadRange(), s.roadRange(idx); got != want {
		return fmt.Errorf("epoch %d roads %v, want %v", idx, got, want)
	}
	if idx >= 2 {
		prev, ok := s.live[idx-1]
		if !ok {
			// A predecessor dropped by PruneBefore cannot be checked and is
			// accepted; a predecessor missing for any other reason is an error.
			if idx-1 >= s.liveRange.First {
				return fmt.Errorf("missing predecessor epoch %d", idx-1)
			}
		} else if err := checkDerived(prev, epoch); err != nil {
			return fmt.Errorf("epoch %d: %w", idx, err)
		}
	}
	s.live[idx] = epoch
	s.liveRange.Next = idx + 1
	return nil
}

func (s *registryState) clone() *registryState {
	return &registryState{
		persistedRegistry: persistedRegistry{live: maps.Clone(s.live), pending: s.pending},
		firstRoad:         s.firstRoad,
		firstTimestamp:    s.firstTimestamp,
		liveRange:         s.liveRange,
	}
}

func newRegistryState(
	genesisCommittee *types.Committee,
	firstRoad types.RoadIndex,
	genesisTimestamp time.Time,
	persisted utils.Option[persistedRegistry],
) (*registryState, error) {
	initial, ok := persisted.Get()
	if !ok {
		return &registryState{
			persistedRegistry: persistedRegistry{live: map[types.EpochIndex]*types.Epoch{
				0: types.NewEpoch(0, types.RoadRange{First: firstRoad, Next: firstRoad + epochLength}, genesisCommittee),
				1: types.NewEpoch(1, types.RoadRange{First: firstRoad + epochLength, Next: firstRoad + 2*epochLength}, genesisCommittee),
			}},
			firstRoad:      firstRoad,
			firstTimestamp: genesisTimestamp,
			liveRange:      types.EpochRange{First: 0, Next: 2},
		}, nil
	}
	if len(initial.live) == 0 {
		return nil, fmt.Errorf("persisted registry has no live epochs")
	}
	s := &registryState{
		persistedRegistry: initial,
		firstRoad:         firstRoad,
		firstTimestamp:    genesisTimestamp,
	}
	for i, idx := range slices.Sorted(maps.Keys(s.live)) {
		epoch := s.live[idx]
		if i == 0 {
			s.liveRange.First = idx
			s.liveRange.Next = idx
		}
		if err := s.activate(epoch); err != nil {
			return nil, fmt.Errorf("live record %d: %w", i, err)
		}
	}
	// TODO: a missing pending is restaged on restart from GetValidators() at app
	// tip, not stake(LastRoad(E)). Use the LastRoad validator set once it is
	// persisted in app state.
	if pending, ok := s.pending.Get(); ok {
		idx := pending.EpochIndex()
		if idx != s.liveRange.Next {
			return nil, fmt.Errorf("pending epoch %d, want %d", idx, s.liveRange.Next)
		}
		if got, want := pending.RoadRange(), s.roadRange(idx); got != want {
			return nil, fmt.Errorf("pending epoch %d roads %v, want %v", idx, got, want)
		}
		if err := checkDerived(s.live[idx-1], pending); err != nil {
			return nil, fmt.Errorf("pending: %w", err)
		}
	}
	for idx := types.EpochIndex(0); idx < 2; idx++ {
		if epoch, ok := s.live[idx]; ok && !epoch.Committee().Equal(genesisCommittee) {
			return nil, fmt.Errorf("epoch %d does not match genesis", idx)
		}
	}
	return s, nil
}

// Registry stores retained epochs and committees staged for activation.
type Registry struct {
	state     utils.Watch[*registryState]
	persister persist.Persister[*pb.PersistedEpochRegistry]
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
	raw, _ := loaded.Get()
	persisted, err := persistedRegistryConv.DecodeOpt(raw)
	if err != nil {
		return nil, fmt.Errorf("restore epoch snapshot: %w", err)
	}
	state, err := newRegistryState(committee, firstRoad, genesisTimestamp, persisted)
	if err != nil {
		return nil, fmt.Errorf("restore epoch snapshot: %w", err)
	}
	return &Registry{
		state:     utils.NewWatch(state),
		persister: persister,
	}, nil
}

func (s *registryState) roadRange(idx types.EpochIndex) types.RoadRange {
	first := s.firstRoad + types.RoadIndex(idx)*epochLength
	return types.RoadRange{First: first, Next: first + epochLength}
}

func (s *registryState) indexForRoad(road types.RoadIndex) types.EpochIndex {
	return types.EpochIndex((road - s.firstRoad) / epochLength)
}

// FirstRoad returns the genesis epoch's first road index.
func (r *Registry) FirstRoad() types.RoadIndex {
	for s := range r.state.Lock() {
		return s.firstRoad
	}
	panic("unreachable")
}

// EpochFirstRoad returns the first road index of epoch idx.
func (r *Registry) EpochFirstRoad(idx types.EpochIndex) types.RoadIndex {
	for s := range r.state.Lock() {
		return s.roadRange(idx).First
	}
	panic("unreachable")
}

// LastRoad returns the last road index of epoch idx.
func (r *Registry) LastRoad(idx types.EpochIndex) types.RoadIndex {
	for s := range r.state.Lock() {
		return s.roadRange(idx).Next - 1
	}
	panic("unreachable")
}

// IndexForRoad returns the epoch index containing road. Road must not precede
// FirstRoad.
func (r *Registry) IndexForRoad(road types.RoadIndex) types.EpochIndex {
	for s := range r.state.Lock() {
		return s.indexForRoad(road)
	}
	panic("unreachable")
}

// ClosingEpoch returns the epoch that road ends, if road is that epoch's last
// road. Otherwise None.
func (r *Registry) ClosingEpoch(road types.RoadIndex) utils.Option[types.EpochIndex] {
	if road < r.FirstRoad() {
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
	for s := range r.state.Lock() {
		return s.firstTimestamp
	}
	panic("unreachable")
}

// EpochByIndex returns the registered epoch at idx.
// It returns ErrPruned if idx has been dropped by PruneBefore.
func (r *Registry) EpochByIndex(idx types.EpochIndex) (*types.Epoch, error) {
	for s := range r.state.Lock() {
		if idx < s.liveRange.First {
			return nil, fmt.Errorf("epoch %d: %w", idx, types.ErrPruned)
		}
		ep, ok := s.live[idx]
		if !ok {
			return nil, fmt.Errorf("epoch %d not registered", idx)
		}
		return ep, nil
	}
	panic("unreachable")
}

// EpochAt returns the registered epoch containing roadIndex.
func (r *Registry) EpochAt(roadIndex types.RoadIndex) (*types.Epoch, error) {
	for s := range r.state.Lock() {
		firstRoad := s.firstRoad
		if roadIndex < firstRoad {
			return nil, fmt.Errorf("road %d: %w", roadIndex, types.ErrPruned)
		}
		epochIdx := s.indexForRoad(roadIndex)
		if epochIdx < s.liveRange.First {
			return nil, fmt.Errorf("epoch %d (road %d): %w", epochIdx, roadIndex, types.ErrPruned)
		}
		if ep, ok := s.live[epochIdx]; ok && ep.RoadRange().Has(roadIndex) {
			return ep, nil
		}
		return nil, fmt.Errorf("epoch %d (road %d) not registered", epochIdx, roadIndex)
	}
	panic("unreachable")
}

// commit writes next to disk, then replaces s with next.
// s is left unchanged if Persist fails.
func (r *Registry) commit(s, next *registryState) error {
	if err := r.persister.Persist(persistedRegistryConv.Encode(next.persistedRegistry)); err != nil {
		return fmt.Errorf("persist epoch registry: %w", err)
	}
	*s = *next
	return nil
}

// StageEpoch derives C_{endEpoch+2} from weights and persists it as pending.
// A no-op if that epoch is already staged or live with the same committee.
// An error if it conflicts, if the target is not live.Next, or if endEpoch+1
// is not live.
func (r *Registry) StageEpoch(endEpoch types.EpochIndex, weights map[types.PublicKey]uint64) error {
	target := endEpoch + 2
	for s := range r.state.Lock() {
		if target < s.liveRange.First {
			return fmt.Errorf("epoch %d: %w", target, types.ErrPruned)
		}
		ep, registered := s.live[target]
		if !registered && target != s.liveRange.Next {
			return fmt.Errorf("epoch %d cannot be staged, want %d", target, s.liveRange.Next)
		}
		prev, ok := s.live[endEpoch+1]
		if !ok {
			return fmt.Errorf("epoch %d not registered", endEpoch+1)
		}
		committee, err := prev.Committee().DeriveNext(weights, target)
		if err != nil {
			return fmt.Errorf("DeriveNext(%d): %w", target, err)
		}
		if registered {
			if !ep.Committee().Equal(committee) {
				return fmt.Errorf("epoch %d already registered with a different committee", target)
			}
			return nil
		}
		if staged, ok := s.pending.Get(); ok {
			if !staged.Committee().Equal(committee) {
				return fmt.Errorf("epoch %d already staged with a different committee", target)
			}
			return nil
		}
		next := s.clone()
		next.pending = utils.Some(types.NewEpoch(target, s.roadRange(target), committee))
		return r.commit(s, next)
	}
	panic("unreachable")
}

// ActivateEpoch publishes the staged committee for idx.
// A no-op if idx is already live. An error if idx is not the staged epoch.
func (r *Registry) ActivateEpoch(idx types.EpochIndex) error {
	for s, ctrl := range r.state.Lock() {
		if _, ok := s.live[idx]; ok {
			return nil
		}
		if idx < s.liveRange.First {
			return fmt.Errorf("epoch %d: %w", idx, types.ErrPruned)
		}
		pending, ok := s.pending.Get()
		if !ok || pending.EpochIndex() != idx {
			return fmt.Errorf("epoch %d is not staged", idx)
		}
		next := s.clone()
		if err := next.activate(pending); err != nil {
			return err
		}
		next.pending = utils.None[*types.Epoch]()
		if err := r.commit(s, next); err != nil {
			return err
		}
		ctrl.Updated()
		return nil
	}
	panic("unreachable")
}

// Pending returns the staged epoch index, or None.
func (r *Registry) Pending() utils.Option[types.EpochIndex] {
	for s := range r.state.Lock() {
		if _, ok := s.pending.Get(); !ok {
			return utils.None[types.EpochIndex]()
		}
		return utils.Some(s.liveRange.Next)
	}
	panic("unreachable")
}

// PruneBefore drops persisted epochs below keep, retaining the latest live epoch.
// Pruning only moves forward and preserves staged epochs.
// TODO: archive nodes need special handling (never prune epochs).
func (r *Registry) PruneBefore(keep types.EpochIndex) error {
	for s, ctrl := range r.state.Lock() {
		if s.liveRange.First == s.liveRange.Next {
			return nil
		}
		keep = min(keep, s.liveRange.Next-1)
		if keep <= s.liveRange.First {
			return nil
		}
		next := s.clone()
		for idx := next.liveRange.First; idx < keep; idx++ {
			delete(next.live, idx)
		}
		next.liveRange.First = keep
		if err := r.commit(s, next); err != nil {
			return err
		}
		ctrl.Updated()
		return nil
	}
	panic("unreachable")
}

// WaitForEpoch blocks until epoch i is registered.
func (r *Registry) WaitForEpoch(ctx context.Context, i types.EpochIndex) (*types.Epoch, error) {
	for inner, ctrl := range r.state.Lock() {
		for {
			if i < inner.liveRange.First {
				return nil, fmt.Errorf("epoch %d: %w", i, types.ErrPruned)
			}
			if ep, ok := inner.live[i]; ok {
				return ep, nil
			}
			if err := ctrl.Wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	panic("unreachable")
}
