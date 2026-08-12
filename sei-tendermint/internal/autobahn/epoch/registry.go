package epoch

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type registryState struct {
	m      map[types.EpochIndex]*types.Epoch
	latest types.EpochIndex
}

// Registry is the authoritative source of epoch and committee information.
// All layers (consensus, data, avail) read from it.
type Registry struct {
	state utils.RWMutex[*registryState]
}

// NewRegistry creates a Registry with the genesis committee.
func NewRegistry(
	committee *types.Committee,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	ep := types.NewEpoch(0, types.OpenRoadRange(), genesisTimestamp, committee, firstBlock)
	return &Registry{
		state: utils.NewRWMutex(&registryState{
			m:      map[types.EpochIndex]*types.Epoch{0: ep},
			latest: 0,
		}),
	}, nil
}

// FirstBlock returns the first global block number of the genesis epoch.
// Used as the cold-start default (no WAL, no snapshot); WAL overrides this on restart.
func (r *Registry) FirstBlock() types.GlobalBlockNumber {
	for s := range r.state.RLock() {
		return s.m[0].FirstBlock()
	}
	panic("unreachable")
}

// GenesisTimestamp returns the timestamp of the genesis epoch.
func (r *Registry) GenesisTimestamp() time.Time {
	for s := range r.state.RLock() {
		return s.m[0].FirstTimestamp()
	}
	panic("unreachable")
}

// EpochByIndex returns the epoch with the given index, if it exists.
func (r *Registry) EpochByIndex(idx types.EpochIndex) (*types.Epoch, bool) {
	for s := range r.state.RLock() {
		ep, ok := s.m[idx]
		return ep, ok
	}
	panic("unreachable")
}

// LatestEpoch returns the most recently activated epoch.
func (r *Registry) LatestEpoch() *types.Epoch {
	for s := range r.state.RLock() {
		return s.m[s.latest]
	}
	panic("unreachable")
}

// ActivateEpoch appends latest+1 via Committee.DeriveNext.
//
// Scaffolding for #3736: does not validate roads.First vs prior RoadRange; prior
// range left as stored. Tests may pass OpenRoadRange() until multi-epoch roads wire up.
func (r *Registry) ActivateEpoch(
	weights map[types.PublicKey]uint64,
	roads types.RoadRange,
	firstTimestamp time.Time,
	firstBlock types.GlobalBlockNumber,
) (*types.Epoch, error) {
	for s := range r.state.Lock() {
		prev := s.m[s.latest]
		next := s.latest + 1
		if _, exists := s.m[next]; exists {
			return nil, fmt.Errorf("epoch %d already exists", next)
		}
		committee, err := prev.Committee().DeriveNext(weights, next)
		if err != nil {
			return nil, err
		}
		ep := types.NewEpoch(next, roads, firstTimestamp, committee, firstBlock)
		s.m[next] = ep
		s.latest = next
		return ep, nil
	}
	panic("unreachable")
}

// VerifyInWindow calls fn against the latest epoch's committee and returns it if accepted.
// Returns a slice of all matching epochs so callers can skip re-verification for any
// epoch already checked here.
// TODO(#3736): expand to neighbor epochs (previous and next) once multi-epoch transitions are wired up.
func (r *Registry) VerifyInWindow(fn func(*types.Committee) error) ([]*types.Epoch, error) {
	for s := range r.state.RLock() {
		ep := s.m[s.latest]
		if err := fn(ep.Committee()); err != nil {
			return nil, err
		}
		return []*types.Epoch{ep}, nil
	}
	panic("unreachable")
}
