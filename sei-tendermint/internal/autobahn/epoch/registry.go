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
	// pending is the staged committee for live.Next.
	pending utils.Option[*types.Committee]
	// live is the supported execution-derived epoch indices [First, Next).
	// Epochs 0 and 1 are genesis and are not represented here.
	live types.EpochRange
}

// dropped reports whether idx is below live.First. Epochs 0 and 1 are never dropped.
func (s *registryState) dropped(idx types.EpochIndex) bool {
	return idx >= 2 && idx < s.live.First
}

// activate makes committee live for idx. It returns an error if idx is not live.Next.
func (s *registryState) activate(idx types.EpochIndex, committee *types.Committee) error {
	if idx != s.live.Next {
		return fmt.Errorf("epoch %d cannot be activated, want %d", idx, s.live.Next)
	}
	roads := types.RoadRange{First: FirstRoad(idx), Next: FirstRoad(idx + 1)}
	s.m[idx] = types.NewEpoch(idx, roads, s.m[0].FirstTimestamp(), committee, s.m[0].FirstBlock())
	s.live.Next = idx + 1
	return nil
}

func (s *registryState) snapshot() *pb.PersistedEpochRegistry {
	snapshot := &pb.PersistedEpochRegistry{
		Live: make([]*pb.EpochRecord, 0, utils.Clamp[int](s.live.Next-s.live.First)),
	}
	for idx := s.live.First; idx < s.live.Next; idx++ {
		snapshot.Live = append(snapshot.Live, encodeEpochRecord(idx, s.m[idx].Committee()))
	}
	if pending, ok := s.pending.Get(); ok {
		snapshot.Pending = encodeEpochRecord(s.live.Next, pending)
	}
	return snapshot
}

func (s *registryState) restore(snapshot *pb.PersistedEpochRegistry) error {
	if snapshot == nil {
		return fmt.Errorf("missing")
	}
	for pos, record := range snapshot.Live {
		idx, committee, err := decodeEpochRecord(record)
		if err != nil {
			return fmt.Errorf("live record %d: %w", pos, err)
		}
		if pos == 0 {
			s.live.First = idx
			s.live.Next = idx
		}
		if err := s.activate(idx, committee); err != nil {
			return fmt.Errorf("live record %d: %w", pos, err)
		}
		if err := s.checkDerivedFromPrev(idx, committee); err != nil {
			return fmt.Errorf("live record %d: %w", pos, err)
		}
	}
	if snapshot.Pending != nil {
		idx, committee, err := decodeEpochRecord(snapshot.Pending)
		if err != nil {
			return fmt.Errorf("pending: %w", err)
		}
		if idx != s.live.Next {
			return fmt.Errorf("pending epoch %d, want %d", idx, s.live.Next)
		}
		if err := s.checkDerivedFromPrev(idx, committee); err != nil {
			return fmt.Errorf("pending: %w", err)
		}
		s.pending = utils.Some(committee)
	}
	return nil
}

// Registry stores genesis epochs 0 and 1 plus execution-derived epochs
// published by ActivateEpoch.
type Registry struct {
	state     utils.Watch[*registryState]
	persister persist.Persister[*pb.PersistedEpochRegistry]
}

// NewRegistry returns a Registry with epochs 0 and 1 using committee, firstBlock,
// and genesisTimestamp. stateDir Some opens the epoch snapshot and restores its
// live and pending committees; None keeps the registry in memory only.
func NewRegistry(
	committee *types.Committee,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
	stateDir utils.Option[string],
) (*Registry, error) {
	ep0 := types.NewEpoch(0, types.RoadRange{First: 0, Next: FirstRoad(1)}, genesisTimestamp, committee, firstBlock)
	ep1 := types.NewEpoch(1, types.RoadRange{First: FirstRoad(1), Next: FirstRoad(2)}, genesisTimestamp, committee, firstBlock)
	state := &registryState{
		m:       map[types.EpochIndex]*types.Epoch{0: ep0, 1: ep1},
		pending: utils.None[*types.Committee](),
		live:    types.EpochRange{First: 2, Next: 2},
	}
	persister, loaded, err := openEpochSnapshot(stateDir)
	if err != nil {
		return nil, err
	}
	if snapshot, ok := loaded.Get(); ok {
		if err := state.restore(snapshot); err != nil {
			return nil, fmt.Errorf("restore epoch snapshot: %w", err)
		}
	}
	return &Registry{
		state:     utils.NewWatch(state),
		persister: persister,
	}, nil
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

// StageEpoch derives C_{endEpoch+2} from weights and persists it as pending.
// A no-op if that epoch is already staged or live with the same committee.
// An error if it conflicts, if the target is not live.Next, or if endEpoch+1
// is not live.
func (r *Registry) StageEpoch(endEpoch types.EpochIndex, weights map[types.PublicKey]uint64) error {
	target := endEpoch + 2
	for s := range r.state.Lock() {
		if s.dropped(target) {
			return fmt.Errorf("epoch %d: %w", target, types.ErrPruned)
		}
		ep, registered := s.m[target]
		if !registered && target != s.live.Next {
			return fmt.Errorf("epoch %d cannot be staged, want %d", target, s.live.Next)
		}
		prev, ok := s.m[endEpoch+1]
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
			if !staged.Equal(committee) {
				return fmt.Errorf("epoch %d already staged with a different committee", target)
			}
			return nil
		}
		s.pending = utils.Some(committee)
		if err := r.persister.Persist(s.snapshot()); err != nil {
			return fmt.Errorf("persist epoch registry: %w", err)
		}
		return nil
	}
	panic("unreachable")
}

// ActivateEpoch publishes the staged committee for idx.
// A no-op if idx is already live. An error if idx is not the staged epoch.
func (r *Registry) ActivateEpoch(idx types.EpochIndex) error {
	for s, ctrl := range r.state.Lock() {
		if _, ok := s.m[idx]; ok {
			return nil
		}
		if s.dropped(idx) {
			return fmt.Errorf("epoch %d: %w", idx, types.ErrPruned)
		}
		committee, ok := s.pending.Get()
		if !ok {
			return fmt.Errorf("epoch %d is not staged", idx)
		}
		if err := s.activate(idx, committee); err != nil {
			return err
		}
		s.pending = utils.None[*types.Committee]()
		if err := r.persister.Persist(s.snapshot()); err != nil {
			return fmt.Errorf("persist epoch registry: %w", err)
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
		return utils.Some(s.live.Next)
	}
	panic("unreachable")
}

// PruneBefore drops epochs in [live.First, keep). Epochs 0 and 1 are kept.
// keep is exclusive, clamped to live.Next, and only moves live.First forward.
// Staged committees are not dropped.
func (r *Registry) PruneBefore(keep types.EpochIndex) error {
	for s, ctrl := range r.state.Lock() {
		keep = min(keep, s.live.Next)
		if keep <= s.live.First {
			return nil
		}
		for idx := s.live.First; idx < keep; idx++ {
			delete(s.m, idx)
		}
		s.live.First = keep
		if err := r.persister.Persist(s.snapshot()); err != nil {
			return fmt.Errorf("persist epoch registry: %w", err)
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
