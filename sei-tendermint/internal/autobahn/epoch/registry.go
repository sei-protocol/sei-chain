package epoch

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// Index is the epoch number.
type Index uint64

// Registry is the authoritative source of committee and stake information.
// All layers (consensus, data, avail) read from it.
//
// Currently the committee is fixed at genesis. Dynamic committee support
// will be wired up when the execution layer is ready.
type Registry struct {
	genesis          *types.Committee
	firstBlock       types.GlobalBlockNumber
	genesisTimestamp time.Time
}

// NewRegistry creates a Registry with the genesis committee.
func NewRegistry(
	weights map[types.PublicKey]uint64,
	firstBlock types.GlobalBlockNumber,
	genesisTimestamp time.Time,
) (*Registry, error) {
	genesis, err := types.NewCommittee(weights)
	if err != nil {
		return nil, fmt.Errorf("genesis committee: %w", err)
	}
	return &Registry{
		genesis:          genesis,
		firstBlock:       firstBlock,
		genesisTimestamp: genesisTimestamp,
	}, nil
}

// FirstBlock returns the first global block number of the chain.
func (r *Registry) FirstBlock() types.GlobalBlockNumber {
	return r.firstBlock
}

// GenesisTimestamp returns the genesis timestamp of the chain.
func (r *Registry) GenesisTimestamp() time.Time {
	return r.genesisTimestamp
}

// EpochFor returns the epoch index for the given RoadIndex.
// Currently always returns 0 (genesis epoch); dynamic lookup will be added
// with the execution layer.
func (r *Registry) EpochFor(_ types.RoadIndex) Index {
	return 0
}

// CommitteeFor returns the committee active at the given RoadIndex.
// Currently always returns the genesis committee; dynamic lookup will be
// added with the execution layer.
func (r *Registry) CommitteeFor(_ types.RoadIndex) *types.Committee {
	return r.genesis
}

// LatestCommittee returns the genesis committee.
func (r *Registry) LatestCommittee() *types.Committee {
	return r.genesis
}

// EpochWindow returns the epoch→committee map for message acceptance across
// epoch transitions. With a fixed genesis committee it always returns a
// single entry.
func (r *Registry) EpochWindow() map[Index]*types.Committee {
	return map[Index]*types.Committee{0: r.genesis}
}

// VerifyInWindow calls fn with each committee in the epoch window and returns
// nil as soon as any call succeeds. Returns the last error if all fail.
func (r *Registry) VerifyInWindow(fn func(*types.Committee) error) error {
	var lastErr error
	for _, c := range r.EpochWindow() {
		if err := fn(c); err == nil {
			return nil
		} else { //nolint:revive
			lastErr = err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("empty epoch window")
}
