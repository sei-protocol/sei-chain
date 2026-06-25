package epoch

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// Index is the epoch number.
type Index uint64

// Epoch is a self-contained description of one epoch: its identity, its
// RoadIndex bounds, and the committee active during it.
type Epoch struct {
	EpochIndex Index
	Start      types.RoadIndex // first RoadIndex of this epoch (inclusive)
	End        types.RoadIndex // last RoadIndex of this epoch (inclusive)
	Timestamp  time.Time       // start time of this epoch
	Committee  *types.Committee
}

// Registry is the authoritative source of committee and stake information.
// All layers (consensus, data, avail) read from it.
//
// Epochs are stored in ascending order of Start. The latest epoch always has
// End = math.MaxUint64. AddEpoch closes off the current latest epoch and
// appends the new one atomically under a write lock.
type Registry struct {
	mu         sync.RWMutex
	epochs     []*Epoch // sorted by Start ascending
	firstBlock types.GlobalBlockNumber
}

// NewRegistry creates a Registry with the genesis committee.
func NewRegistry(
	weights map[types.PublicKey]uint64,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	committee, err := types.NewCommittee(weights)
	if err != nil {
		return nil, fmt.Errorf("genesis committee: %w", err)
	}
	return &Registry{
		epochs: []*Epoch{{
			EpochIndex: 0,
			Start:      0,
			End:        math.MaxUint64,
			Timestamp:  genesisTimestamp,
			Committee:  committee,
		}},
		firstBlock: firstBlock,
	}, nil
}

// FirstBlock returns the first global block number of the chain.
func (r *Registry) FirstBlock() types.GlobalBlockNumber {
	return r.firstBlock
}

// GenesisTimestamp returns the timestamp of the genesis epoch.
func (r *Registry) GenesisTimestamp() time.Time {
	return r.epochs[0].Timestamp
}

// EpochFor returns the epoch active at the given RoadIndex.
func (r *Registry) EpochFor(road types.RoadIndex) *Epoch {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.epochForLocked(road)
}

func (r *Registry) epochForLocked(road types.RoadIndex) *Epoch {
	// find first epoch whose Start > road; the one before it is active
	idx := sort.Search(len(r.epochs), func(i int) bool {
		return r.epochs[i].Start > road
	})
	if idx == 0 {
		return r.epochs[0]
	}
	return r.epochs[idx-1]
}

// CommitteeFor returns the committee active at the given RoadIndex.
func (r *Registry) CommitteeFor(road types.RoadIndex) *types.Committee {
	return r.EpochFor(road).Committee
}

// EpochByIndex returns the epoch with the given index, if it exists.
func (r *Registry) EpochByIndex(idx Index) (*Epoch, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.epochs {
		if e.EpochIndex == idx {
			return e, true
		}
	}
	return nil, false
}

// LatestEpoch returns the most recently activated epoch.
func (r *Registry) LatestEpoch() *Epoch {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.epochs[len(r.epochs)-1]
}

// AddEpoch registers a new epoch starting at startRoad with the given committee
// weights. The current latest epoch's End is closed off at startRoad-1.
// Called by the execution bridge when a new committee is finalized.
func (r *Registry) AddEpoch(weights map[types.PublicKey]uint64, startRoad types.RoadIndex, timestamp time.Time) error {
	committee, err := types.NewCommittee(weights)
	if err != nil {
		return fmt.Errorf("epoch committee: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	latest := r.epochs[len(r.epochs)-1]
	if startRoad <= latest.Start {
		return fmt.Errorf("new epoch start %d must be after current epoch start %d", startRoad, latest.Start)
	}
	latest.End = startRoad - 1
	r.epochs = append(r.epochs, &Epoch{
		EpochIndex: latest.EpochIndex + 1,
		Start:      startRoad,
		End:        math.MaxUint64,
		Timestamp:  timestamp,
		Committee:  committee,
	})
	return nil
}

// VerifyInWindow calls fn with the latest epoch's committee.
// TODO: expand to a two-epoch window once multi-epoch transitions are wired up.
func (r *Registry) VerifyInWindow(fn func(*types.Committee) error) error {
	return fn(r.LatestEpoch().Committee)
}
