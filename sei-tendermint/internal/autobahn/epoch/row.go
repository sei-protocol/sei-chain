package epoch

import (
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// DefaultEpochLength is the default number of road blocks per epoch.
const DefaultEpochLength types.RoadHeight = 108_000

// keptEpochs is the number of newest epochs a row retains.
const keptEpochs = 3

// GenesisSchedule is the genesis arithmetic of the epoch registry.
type GenesisSchedule struct {
	// FirstRoad is the first road of epoch 0.
	FirstRoad types.RoadHeight
	// Length is the number of roads in each epoch.
	Length types.RoadHeight
	// Timestamp is the genesis timestamp.
	Timestamp time.Time
}

// GenesisRow returns the row holding genesis epochs 0 and 1, both committed to weights.
func GenesisRow(weights map[types.PublicKey]uint64, s GenesisSchedule) ([]byte, error) {
	if s.Length == 0 {
		return nil, errors.New("epoch length must be greater than 0")
	}
	committee, err := types.NewCommittee(weights)
	if err != nil {
		return nil, fmt.Errorf("genesis committee: %w", err)
	}
	first := s.FirstRoad
	return Encode([]*types.Epoch{
		types.NewEpoch(0, types.RoadRange{First: first, Next: first + s.Length}, committee),
		types.NewEpoch(1, types.RoadRange{First: first + s.Length, Next: first + 2*s.Length}, committee),
	}), nil
}

// Advance returns the window after road executes, at most the three newest epochs. On the last road
// of the epoch before the newest, it appends the next epoch and drops any older one. It returns
// None for every other road, and an error when epochs is empty or road is at or past the newest
// epoch.
func Advance(epochs []*types.Epoch, road types.RoadHeight, weights map[types.PublicKey]uint64) (utils.Option[[]*types.Epoch], error) {
	if len(epochs) == 0 {
		return utils.None[[]*types.Epoch](), errors.New("epoch registry: no epochs")
	}
	newest := epochs[len(epochs)-1]
	boundary := newest.RoadRange().First
	if road >= boundary {
		return utils.None[[]*types.Epoch](), fmt.Errorf("road %d: epoch %d is already registered, need weights at %d", road, newest.EpochIndex(), boundary-1)
	}
	if road+1 != boundary {
		return utils.None[[]*types.Epoch](), nil
	}
	next := newest.EpochIndex() + 1
	committee, err := newest.Committee().DeriveNext(weights, next)
	if err != nil {
		return utils.None[[]*types.Epoch](), fmt.Errorf("DeriveNext(%d): %w", next, err)
	}
	length := newest.RoadRange().Next - newest.RoadRange().First
	first := newest.RoadRange().Next
	window := make([]*types.Epoch, len(epochs), len(epochs)+1)
	copy(window, epochs)
	window = append(window, types.NewEpoch(next, types.RoadRange{First: first, Next: first + length}, committee))
	// Closing epoch N-1 drops N-2. The back leash keeps the anchor epoch at N-1
	// while this road runs, so the dropped epoch is behind the anchor.
	if len(window) > keptEpochs {
		window = window[len(window)-keptEpochs:]
	}
	return utils.Some(window), nil
}

// Decode returns the epochs a row holds, oldest first. It returns an error unless the epochs have
// consecutive indices, contiguous road ranges, and committees that follow one another.
func Decode(row []byte) ([]*types.Epoch, error) {
	raw, err := protoutils.Unmarshal[*pb.PersistedEpochRegistry](row)
	if err != nil {
		return nil, fmt.Errorf("unmarshal epoch registry: %w", err)
	}
	epochs, err := types.EpochConv.DecodeSlice(raw.LiveV2)
	if err != nil {
		return nil, fmt.Errorf("epoch registry: %w", err)
	}
	if len(epochs) == 0 {
		return nil, errors.New("epoch registry: no epochs")
	}
	for i := 1; i < len(epochs); i++ {
		prev, cur := epochs[i-1], epochs[i]
		if cur.EpochIndex() != prev.EpochIndex()+1 {
			return nil, fmt.Errorf("epoch registry: epoch %d does not follow %d", cur.EpochIndex(), prev.EpochIndex())
		}
		if prev.RoadRange().Next != cur.RoadRange().First {
			return nil, fmt.Errorf("epoch registry: epoch %d roads are not contiguous", cur.EpochIndex())
		}
		if err := checkDerived(prev, cur); err != nil {
			return nil, fmt.Errorf("epoch registry: epoch %d: %w", cur.EpochIndex(), err)
		}
	}
	return epochs, nil
}

// Encode returns the canonical row of epochs.
func Encode(epochs []*types.Epoch) []byte {
	return hashable.MarshalCanonical(&pb.PersistedEpochRegistry{LiveV2: types.EpochConv.EncodeSlice(epochs)})
}

// checkDerived returns an error if b's committee is not derived from a's.
func checkDerived(a, b *types.Epoch) error {
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
func weightsOf(c *types.Committee) map[types.PublicKey]uint64 {
	weights := make(map[types.PublicKey]uint64, c.Lanes().Len())
	for lane := range c.Lanes().All() {
		weights[lane.Validator] = c.Weight(lane.Validator)
	}
	return weights
}
