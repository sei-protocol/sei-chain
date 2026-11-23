package types

import (
	"errors"
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// keptEpochs is the number of newest epochs a registry retains.
const keptEpochs = 3

// EpochRegistry is the retained epoch window, oldest first.
type EpochRegistry struct {
	utils.ReadOnly
	epochs []*Epoch
}

// Epochs returns the retained epochs, oldest first.
func (r *EpochRegistry) Epochs() []*Epoch { return r.epochs }

// Epoch returns the retained epoch at idx.
// ErrPruned means idx is older than the window. ErrNotFound means idx is newer.
func (r *EpochRegistry) Epoch(idx EpochIndex) (*Epoch, error) {
	if idx < r.epochs[0].EpochIndex() {
		return nil, fmt.Errorf("epoch %d: %w", idx, ErrPruned)
	}
	for _, e := range r.epochs {
		if e.EpochIndex() == idx {
			return e, nil
		}
	}
	return nil, fmt.Errorf("epoch %d: %w", idx, ErrNotFound)
}

// ForAnchor returns the epoch containing road and the epoch after it.
// The bool is false when the window does not hold both.
func (r *EpochRegistry) ForAnchor(road RoadHeight) ([2]*Epoch, bool) {
	if road < r.epochs[0].RoadRange().First {
		return [2]*Epoch{}, false
	}
	for i, e := range r.epochs {
		if !e.RoadRange().Has(road) {
			continue
		}
		if i+1 == len(r.epochs) {
			return [2]*Epoch{}, false
		}
		return [2]*Epoch{e, r.epochs[i+1]}, true
	}
	return [2]*Epoch{}, false
}

// GenesisRegistry returns a registry holding genesis epochs 0 and 1, both committed to weights.
// Epoch 0 starts at firstRoad and each epoch lasts length roads.
func GenesisRegistry(weights map[PublicKey]uint64, firstRoad, length RoadHeight) (*EpochRegistry, error) {
	if length == 0 {
		return nil, errors.New("epoch length must be greater than 0")
	}
	committee, err := NewCommittee(weights)
	if err != nil {
		return nil, fmt.Errorf("genesis committee: %w", err)
	}
	return newEpochRegistry([]*Epoch{
		NewEpoch(0, RoadRange{First: firstRoad, Next: firstRoad + length}, committee),
		NewEpoch(1, RoadRange{First: firstRoad + length, Next: firstRoad + 2*length}, committee),
	})
}

// DerivedOn reports whether road is the closing road that produced the newest epoch.
// It reports false when the window does not retain that epoch.
func (r *EpochRegistry) DerivedOn(road RoadHeight) bool {
	if len(r.epochs) < keptEpochs {
		return false
	}
	return road+1 == r.epochs[len(r.epochs)-keptEpochs].RoadRange().Next
}

// DiscardNewest returns the window without its newest epoch.
func (r *EpochRegistry) DiscardNewest() (*EpochRegistry, error) {
	if len(r.epochs) < 2 {
		return nil, errors.New("epoch registry: no epoch to discard")
	}
	return newEpochRegistry(slices.Clone(r.epochs[:len(r.epochs)-1]))
}

// Advance returns the registry after road executes.
// On the last road of the epoch before the newest, it appends the next epoch and keeps the three
// newest. It returns None for every other road, and an error when road is at or past the newest epoch.
func (r *EpochRegistry) Advance(road RoadHeight, weights map[PublicKey]uint64) (utils.Option[*EpochRegistry], error) {
	newest := r.epochs[len(r.epochs)-1]
	boundary := newest.RoadRange().First
	if road >= boundary {
		return utils.None[*EpochRegistry](), fmt.Errorf("road %d: epoch %d is already registered, need weights at %d", road, newest.EpochIndex(), boundary-1)
	}
	if road+1 != boundary {
		return utils.None[*EpochRegistry](), nil
	}
	next := newest.EpochIndex() + 1
	committee, err := newest.Committee().DeriveNext(weights, next)
	if err != nil {
		return utils.None[*EpochRegistry](), fmt.Errorf("DeriveNext(%d): %w", next, err)
	}
	length := newest.RoadRange().Next - newest.RoadRange().First
	first := newest.RoadRange().Next
	window := make([]*Epoch, len(r.epochs), len(r.epochs)+1)
	copy(window, r.epochs)
	window = append(window, NewEpoch(next, RoadRange{First: first, Next: first + length}, committee))
	registry, err := newEpochRegistry(retainNewest(window))
	if err != nil {
		return utils.None[*EpochRegistry](), err
	}
	return utils.Some(registry), nil
}

// EpochRegistryConv converts an epoch registry to and from its protobuf record.
var EpochRegistryConv = protoutils.Conv[*EpochRegistry, *pb.EpochRegistry]{
	Encode: func(r *EpochRegistry) *pb.EpochRegistry {
		return &pb.EpochRegistry{Epochs: EpochConv.EncodeSlice(r.epochs)}
	},
	Decode: func(p *pb.EpochRegistry) (*EpochRegistry, error) {
		epochs, err := EpochConv.DecodeSlice(p.Epochs)
		if err != nil {
			return nil, err
		}
		return newEpochRegistry(epochs)
	},
}

// newEpochRegistry returns a registry holding epochs.
// epochs must be non-empty, consecutive, contiguous, and derived from one another.
func newEpochRegistry(epochs []*Epoch) (*EpochRegistry, error) {
	if err := checkEpochs(epochs); err != nil {
		return nil, err
	}
	return &EpochRegistry{epochs: epochs}, nil
}

// retainNewest returns the newest epochs of window, at most keptEpochs.
// Closing epoch N-1 drops N-2. The anchor stays at N-1 while that road runs, so the dropped epoch
// is behind the anchor.
func retainNewest(window []*Epoch) []*Epoch {
	if len(window) > keptEpochs {
		return window[len(window)-keptEpochs:]
	}
	return window
}

// checkEpochs returns an error when epochs is empty, or when the epochs are not consecutive,
// contiguous, and derived from one another.
func checkEpochs(epochs []*Epoch) error {
	if len(epochs) == 0 {
		return errors.New("no epochs")
	}
	for i := 1; i < len(epochs); i++ {
		prev, cur := epochs[i-1], epochs[i]
		if cur.EpochIndex() != prev.EpochIndex()+1 {
			return fmt.Errorf("epoch %d does not follow %d", cur.EpochIndex(), prev.EpochIndex())
		}
		if prev.RoadRange().Next != cur.RoadRange().First {
			return fmt.Errorf("epoch %d roads are not contiguous", cur.EpochIndex())
		}
		if err := checkDerived(prev, cur); err != nil {
			return fmt.Errorf("epoch %d: %w", cur.EpochIndex(), err)
		}
	}
	return nil
}

// checkDerived returns an error if b's committee is not derived from a's.
func checkDerived(a, b *Epoch) error {
	want, err := a.Committee().DeriveNext(weightsOf(b.Committee()), b.EpochIndex())
	if err != nil {
		return err
	}
	if !want.Equal(b.Committee()) {
		return fmt.Errorf("does not follow epoch %d", a.EpochIndex())
	}
	return nil
}

// weightsOf returns the voting weights of c.
func weightsOf(c *Committee) map[PublicKey]uint64 {
	weights := make(map[PublicKey]uint64, c.Lanes().Len())
	for lane := range c.Lanes().All() {
		weights[lane.Validator] = c.Weight(lane.Validator)
	}
	return weights
}
