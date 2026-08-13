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
// Lanes carry membership (validator + joined); weights are voting stake.
type Committee struct {
	validators  ImSlice[PublicKey] // ordered list
	laneIDs     ImSlice[LaneID]    // same order as validators
	lanes       map[PublicKey]LaneID
	weights     map[PublicKey]uint64
	totalWeight uint64
}

const MaxValidators = 100

func (c *Committee) HasReplica(k PublicKey) bool {
	_, ok := c.weights[k]
	return ok
}

func (c *Committee) HasLane(l LaneID) bool {
	got, ok := c.lanes[l.Validator]
	return ok && got.Joined == l.Joined
}

func (c *Committee) Lane(v PublicKey) utils.Option[LaneID] {
	lane, ok := c.lanes[v]
	if !ok {
		return utils.None[LaneID]()
	}
	return utils.Some(lane)
}

func (c *Committee) Lanes() ImSlice[LaneID] {
	return c.laneIDs
}

// Deterministic random oracle selecting a replica with probability proportional to the weight.
// Walks validators membership order so seed → PublicKey is network-wide deterministic.
func (c *Committee) randomReplica(seed []byte) PublicKey {
	h := sha256.Sum256(seed[:])
	var x, total uint256.Int
	x.SetBytes32(h[:])
	total.SetUint64(c.totalWeight)
	y := x.Mod(&x, &total).Uint64()
	// TODO(gprusak): this can be optimized to O(1) lookup
	for k := range c.validators.All() {
		w := c.weights[k]
		if y < w {
			return k
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

// NewCommittee is genesis: Joined = 0 for every member.
func NewCommittee(weights map[PublicKey]uint64) (*Committee, error) {
	return newCommittee(nil, weights, 0)
}

// DeriveNext builds the committee for epoch e>0 from this committee:
// validators that remain keep Joined; new members get Joined = e.
func (c *Committee) DeriveNext(weights map[PublicKey]uint64, e EpochIndex) (*Committee, error) {
	if e == 0 {
		return nil, errors.New("DeriveNext: epoch must be > 0")
	}
	return newCommittee(c.lanes, weights, e)
}

// newCommittee builds a committee from weights for epoch e.
// prev is the prior epoch's lanes (nil for genesis).
//
// Weight handling: drop zero-weights, sum stake (error on overflow or empty total),
// reject more than MaxValidators members.
func newCommittee(prev map[PublicKey]LaneID, weights map[PublicKey]uint64, e EpochIndex) (*Committee, error) {
	weights = maps.Clone(weights)
	totalWeight := uint64(0)
	for k, w := range weights {
		if w == 0 {
			delete(weights, k)
			continue
		}
		if utils.Max[uint64]()-totalWeight < w {
			return nil, fmt.Errorf("total weight overflow")
		}
		totalWeight += w
	}
	if totalWeight == 0 {
		return nil, errors.New("total weight is 0")
	}
	if len(weights) > MaxValidators {
		return nil, fmt.Errorf("too many validators: got %d, want <= %d", len(weights), MaxValidators)
	}

	lanes := make(map[PublicKey]LaneID, len(weights))
	for v := range weights {
		if old, ok := prev[v]; ok {
			lanes[v] = old
		} else {
			lanes[v] = LaneID{Validator: v, Joined: e}
		}
	}
	validators := slices.Collect(maps.Keys(lanes))
	slices.SortFunc(validators, PublicKey.Compare)
	laneIDs := make([]LaneID, len(validators))
	for i, v := range validators {
		laneIDs[i] = lanes[v]
	}
	return &Committee{
		validators:  ImSlice[PublicKey]{validators},
		laneIDs:     ImSlice[LaneID]{laneIDs},
		lanes:       lanes,
		weights:     weights,
		totalWeight: totalWeight,
	}, nil
}
