package types

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Immutable slice.
type ImSlice[T any] struct{ s []T }

func (s ImSlice[T]) Len() int         { return len(s.s) }
func (s ImSlice[T]) At(i int) T       { return s.s[i] }
func (s ImSlice[T]) All() iter.Seq[T] { return slices.Values(s.s) }

// Committee represents the consensus committee.
// Membership is the lane list (validator + e_join); voting stake is weights.
type Committee struct {
	lanes       ImSlice[LaneID] // sorted; one lane per member
	byValidator map[PublicKey]LaneID
	weights     map[PublicKey]uint64
	totalWeight uint64
}

const MaxValidators = 100

func (c *Committee) HasReplica(k PublicKey) bool {
	_, ok := c.weights[k]
	return ok
}

func (c *Committee) HasLane(l LaneID) bool {
	got, ok := c.byValidator[l.validator]
	return ok && got == l
}

// Lane returns the LaneID for validator v in this committee, if present.
func (c *Committee) Lane(v PublicKey) utils.Option[LaneID] {
	lane, ok := c.byValidator[v]
	if !ok {
		return utils.None[LaneID]()
	}
	return utils.Some(lane)
}

// Lanes is the list of nodes which are eligible to produce blocks.
func (c *Committee) Lanes() ImSlice[LaneID] { return c.lanes }

// Deterministic random oracle selecting a replica with probability proportional to the weight.
func (c *Committee) randomReplica(seed []byte) PublicKey {
	h := sha256.Sum256(seed[:])
	var x, total uint256.Int
	x.SetBytes32(h[:])
	total.SetUint64(c.totalWeight)
	y := x.Mod(&x, &total).Uint64()
	// TODO(gprusak): this can be optimized to O(1) lookup
	for lane := range c.lanes.All() {
		w := c.weights[lane.validator]
		if y < w {
			return lane.validator
		}
		y -= w
	}
	panic("unreachable")
}

// Weight of validator k.
func (c *Committee) Weight(k PublicKey) uint64 { return c.weights[k] }

// Replica which is responsible for sequencing transactions from this addr.
func (c *Committee) EvmShard(addr common.Address) PublicKey {
	// TODO(gprusak): given that we currently do not have censorship-resistance,
	// from correctness perspective if doesn't matter if shards are proportional to weights.
	// For private testnet we need the load on each validator to be the same.
	// For mainnet we need to resolve this issue somehow differently.
	return c.randomReplica(addr[:])
}

// Leader for the consensus round with the given index.
func (c *Committee) Leader(view View) PublicKey {
	// TODO(gprusak): this needs domain separation.
	d := binary.BigEndian.AppendUint64(nil, uint64(view.Index))
	d = binary.BigEndian.AppendUint64(d, uint64(view.Number))
	return c.randomReplica(d)
}

// Faulty is the maximal total weight of faulty replicas that consensus can tolerate.
func (c *Committee) Faulty() uint64 {
	// 3f < N
	return (c.totalWeight - 1) / 3
}

// CommitQuorum is the weight of the quorum required for CommitQC.
func (c *Committee) CommitQuorum() uint64 {
	return c.totalWeight - c.Faulty()
}

// AppQuorum is the weight of the quorum required for AppQC.
func (c *Committee) AppQuorum() uint64 {
	// This needs to be in range (c.Faulty(), c.CommitQuorum()]
	return c.CommitQuorum()
}

// PrepareQuorum is the weight of the quorum required for PrepareQC.
func (c *Committee) PrepareQuorum() uint64 {
	return c.CommitQuorum()
}

// TimeoutQuorum is the size of the quorum required for TimeoutQC.
func (c *Committee) TimeoutQuorum() uint64 {
	return c.CommitQuorum()
}

// LaneQuorum is the weight of the quorum required for LaneQC.
func (c *Committee) LaneQuorum() uint64 {
	return c.Faulty() + 1
}

// NewCommittee builds a genesis committee: every member gets e_join = 0.
func NewCommittee(weights map[PublicKey]uint64) (*Committee, error) {
	weights, totalWeight, err := normalizeWeights(weights)
	if err != nil {
		return nil, err
	}
	lanes := make([]LaneID, 0, len(weights))
	for v := range weights {
		lanes = append(lanes, NewLaneID(v, 0))
	}
	return finalizeCommittee(lanes, weights, totalWeight)
}

// ActivateCommittee builds the committee for epoch e from weights, copying
// e_join from prev for continuous members and stamping e_join = e for joiners.
// prev is required; e must be > 0.
func ActivateCommittee(prev *Committee, weights map[PublicKey]uint64, e EpochIndex) (*Committee, error) {
	if prev == nil {
		return nil, errors.New("ActivateCommittee: prev is required")
	}
	if e == 0 {
		return nil, errors.New("ActivateCommittee: epoch must be > 0")
	}
	weights, totalWeight, err := normalizeWeights(weights)
	if err != nil {
		return nil, err
	}
	lanes := make([]LaneID, 0, len(weights))
	for v := range weights {
		eJoin := e
		if prevLane, ok := prev.Lane(v).Get(); ok {
			eJoin = prevLane.eJoin
		}
		lanes = append(lanes, NewLaneID(v, eJoin))
	}
	return finalizeCommittee(lanes, weights, totalWeight)
}

func normalizeWeights(weights map[PublicKey]uint64) (map[PublicKey]uint64, uint64, error) {
	weights = maps.Clone(weights)
	totalWeight := uint64(0)
	for k, w := range weights {
		if w == 0 {
			delete(weights, k)
			continue
		}
		if utils.Max[uint64]()-totalWeight < w {
			return nil, 0, fmt.Errorf("total weight overflow")
		}
		totalWeight += w
	}
	if totalWeight == 0 {
		return nil, 0, errors.New("total weight is 0")
	}
	if len(weights) > MaxValidators {
		return nil, 0, fmt.Errorf("too many validators: got %d, want <= %d", len(weights), MaxValidators)
	}
	return weights, totalWeight, nil
}

// finalizeCommittee sorts by pubkey only and rejects duplicate validators
// (one pubkey must not appear with multiple e_join values).
func finalizeCommittee(lanes []LaneID, weights map[PublicKey]uint64, totalWeight uint64) (*Committee, error) {
	slices.SortFunc(lanes, func(a, b LaneID) int { return a.validator.Compare(b.validator) })
	for i := 1; i < len(lanes); i++ {
		if lanes[i].validator == lanes[i-1].validator {
			return nil, fmt.Errorf(
				"duplicate validator in committee lanes: %q with e_join %d and %d",
				lanes[i].validator, lanes[i-1].eJoin, lanes[i].eJoin,
			)
		}
	}
	byValidator := make(map[PublicKey]LaneID, len(lanes))
	for _, lane := range lanes {
		byValidator[lane.validator] = lane
	}
	return &Committee{
		lanes:       ImSlice[LaneID]{lanes},
		byValidator: byValidator,
		weights:     weights,
		totalWeight: totalWeight,
	}, nil
}

// NewRoundRobinElection creates a Committee with equal weights for each replica (e_join = 0).
func NewRoundRobinElection(replicas []PublicKey) (*Committee, error) {
	weights := map[PublicKey]uint64{}
	for _, k := range replicas {
		weights[k] = 1
	}
	return NewCommittee(weights)
}
