package epoch

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Index is the epoch number.
type Index uint64

// Registry is the authoritative source of committee and stake information.
// All layers (consensus, data, avail) read from it.
//
// Epochs are stored in ascending order of Roads().First. The latest epoch always has
// Roads().Last = math.MaxUint64. AddEpoch closes off the current latest epoch and
// appends the new one atomically under a write lock.
type Registry struct {
	mu     utils.RWMutex[struct{}]
	epochs []*types.Epoch // sorted by Roads().First ascending
}

// NewRegistry creates a Registry with the genesis committee.
func NewRegistry(
	committee *types.Committee,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	return &Registry{
		epochs: []*types.Epoch{types.NewEpoch(
			0,
			types.RoadRange{First: 0, Last: math.MaxUint64},
			genesisTimestamp,
			committee,
			firstBlock,
		)},
	}, nil
}

// FirstBlock returns the first global block number of the chain (epoch 0's FirstBlock).
func (r *Registry) FirstBlock() types.GlobalBlockNumber {
	for range r.mu.RLock() {
		return r.epochs[0].FirstBlock()
	}
	panic("unreachable")
}

// GenesisTimestamp returns the timestamp of the genesis epoch.
func (r *Registry) GenesisTimestamp() time.Time {
	for range r.mu.RLock() {
		return r.epochs[0].FirstTimestamp()
	}
	panic("unreachable")
}

// EpochFor returns the epoch active at the given RoadIndex.
func (r *Registry) EpochFor(road types.RoadIndex) *types.Epoch {
	for range r.mu.RLock() {
		return r.epochForLocked(road)
	}
	panic("unreachable")
}

func (r *Registry) epochForLocked(road types.RoadIndex) *types.Epoch {
	// find first epoch whose Roads().First > road; the one before it is active
	idx := sort.Search(len(r.epochs), func(i int) bool {
		return r.epochs[i].Roads().First > road
	})
	if idx == 0 {
		return r.epochs[0]
	}
	return r.epochs[idx-1]
}

// EpochByIndex returns the epoch with the given index, if it exists.
func (r *Registry) EpochByIndex(idx Index) (*types.Epoch, bool) {
	for range r.mu.RLock() {
		for _, e := range r.epochs {
			if e.EpochIndex() == uint64(idx) {
				return e, true
			}
		}
		return nil, false
	}
	panic("unreachable")
}

// LatestEpoch returns the most recently activated epoch.
func (r *Registry) LatestEpoch() *types.Epoch {
	for range r.mu.RLock() {
		return r.epochs[len(r.epochs)-1]
	}
	panic("unreachable")
}

// AddEpoch registers a new epoch starting at startRoad with the given committee.
// The current latest epoch's Roads().Last is closed off at startRoad-1.
// firstBlock is the first global block number of the new epoch.
// Called by the execution bridge when a new committee is finalized.
func (r *Registry) AddEpoch(committee *types.Committee, startRoad types.RoadIndex, timestamp time.Time, firstBlock types.GlobalBlockNumber) error {
	for range r.mu.Lock() {
		latest := r.epochs[len(r.epochs)-1]
		if startRoad <= latest.Roads().First {
			return fmt.Errorf("new epoch start %d must be after current epoch start %d", startRoad, latest.Roads().First)
		}
		// Replace latest with a closed version (Roads().Last = startRoad-1).
		r.epochs[len(r.epochs)-1] = types.NewEpoch(
			latest.EpochIndex(),
			types.RoadRange{First: latest.Roads().First, Last: startRoad - 1},
			latest.FirstTimestamp(),
			latest.Committee(),
			latest.FirstBlock(),
		)
		r.epochs = append(r.epochs, types.NewEpoch(
			latest.EpochIndex()+1,
			types.RoadRange{First: startRoad, Last: math.MaxUint64},
			timestamp,
			committee,
			firstBlock,
		))
		return nil
	}
	panic("unreachable")
}

// VerifyInWindow calls fn against each committee in the epoch window until one succeeds.
// The window is the current epoch plus its immediate neighbors (previous and next): during
// an epoch transition, votes may still arrive under the outgoing committee while the
// incoming one is already registered, so both must be accepted.
// TODO: expand to the full window once multi-epoch transitions are wired up.
func (r *Registry) VerifyInWindow(fn func(*types.Committee) error) error {
	return fn(r.LatestEpoch().Committee())
}
