package epoch

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// GenRegistry generates a random Registry of the given committee size.
// Returns the generated secret keys as well.
// Intended for use in tests only.
func GenRegistry(rng utils.Rng, size int) (*Registry, []types.SecretKey) {
	r, sks, _ := genRegistry(rng, size)
	return r, sks
}

// GenRegistryThrough returns a Registry with genesis epochs 0 and 1 plus
// execution-derived committees through last.
// Intended for use in tests only.
func GenRegistryThrough(rng utils.Rng, size int, last types.EpochIndex) (*Registry, []types.SecretKey) {
	registry, sks, weights := genRegistry(rng, size)
	for target := types.EpochIndex(2); target <= last; target++ {
		utils.OrPanic(registry.TestTryPushEpoch(registry.LastRoad(target-2), weights))
	}
	return registry, sks
}

// TestGenesisRegistry returns a registry holding genesis epochs 0 and 1, both committed to
// committee, with epoch 0 starting at firstRoad and each epoch lasting length roads.
// Intended for use in tests only.
func TestGenesisRegistry(committee *types.Committee, firstRoad, length types.RoadHeight) (*Registry, error) {
	return NewRegistry([]*types.Epoch{
		types.NewEpoch(0, types.RoadRange{First: firstRoad, Next: firstRoad + length}, committee),
		types.NewEpoch(1, types.RoadRange{First: firstRoad + length, Next: firstRoad + 2*length}, committee),
	})
}

func genRegistry(rng utils.Rng, size int) (*Registry, []types.SecretKey, map[types.PublicKey]uint64) {
	sks := utils.GenSliceN(rng, size, types.GenSecretKey)
	weights := map[types.PublicKey]uint64{}
	for _, sk := range sks {
		weights[sk.Public()] = 1000 + uint64(rng.Intn(1000)) //nolint:gosec
	}
	firstHeight := types.GenRoadHeight(rng)%1000000 + 1
	row, err := GenesisRow(weights, GenesisSchedule{FirstRoad: firstHeight, Length: DefaultEpochLength, Timestamp: time.Now()})
	utils.OrPanic(err)
	epochs, err := Decode(row)
	utils.OrPanic(err)
	registry, err := NewRegistry(epochs)
	utils.OrPanic(err)
	return registry, sks, weights
}

// TestTryPushEpoch derives the next epoch when road is the road before the boundary of the newest
// epoch, and publishes it. Other roads are ignored. Intended for use in tests only.
func (r *Registry) TestTryPushEpoch(road types.RoadHeight, weights map[types.PublicKey]uint64) error {
	next, err := Advance(r.Epochs(), road, weights)
	if err != nil {
		return err
	}
	epochs, ok := next.Get()
	if !ok {
		return nil
	}
	r.Store(epochs)
	return nil
}

// TestPushEpoch derives C_{endEpoch+2} from weights and publishes it.
// Intended for use in tests only.
func (r *Registry) TestPushEpoch(endEpoch types.EpochIndex, weights map[types.PublicKey]uint64) error {
	return r.TestTryPushEpoch(r.LastRoad(endEpoch), weights)
}

// LastRoad returns the last road height of epoch idx, which may be past the retained window.
// Intended for use in tests only.
func (r *Registry) LastRoad(idx types.EpochIndex) types.RoadHeight {
	return r.roadRange(idx).Next - 1
}

// roadRange returns the road range of epoch idx, extending the newest retained epoch by its length.
func (r *Registry) roadRange(idx types.EpochIndex) types.RoadRange {
	epochs := r.Epochs()
	for _, e := range epochs {
		if e.EpochIndex() == idx {
			return e.RoadRange()
		}
	}
	newest := epochs[len(epochs)-1]
	if idx < newest.EpochIndex() {
		panic(fmt.Sprintf("epoch %d is not retained", idx))
	}
	length := newest.RoadRange().Next - newest.RoadRange().First
	first := newest.RoadRange().Next + types.RoadHeight(idx-newest.EpochIndex()-1)*length
	return types.RoadRange{First: first, Next: first + length}
}

// Epoch returns the retained epoch at idx. It returns ErrPruned if idx has been dropped, and
// ErrNotFound if idx is past the newest retained epoch.
// Intended for use in tests only.
func (r *Registry) Epoch(idx types.EpochIndex) (*types.Epoch, error) {
	epochs := r.Epochs()
	if idx < epochs[0].EpochIndex() {
		return nil, fmt.Errorf("epoch %d: %w", idx, types.ErrPruned)
	}
	for _, e := range epochs {
		if e.EpochIndex() == idx {
			return e, nil
		}
	}
	return nil, fmt.Errorf("epoch %d: %w", idx, types.ErrNotFound)
}

// EpochAt returns the retained epoch containing road.
// Intended for use in tests only.
func (r *Registry) EpochAt(road types.RoadHeight) (*types.Epoch, error) {
	for _, e := range r.Epochs() {
		if e.RoadRange().Has(road) {
			return e, nil
		}
	}
	return nil, fmt.Errorf("road %d: %w", road, types.ErrNotFound)
}

// MustEpoch returns the retained epoch at i. Panics if it is missing.
// Intended for use in tests only.
func (r *Registry) MustEpoch(i types.EpochIndex) *types.Epoch {
	ep, err := r.Epoch(i)
	if err != nil {
		panic(err)
	}
	return ep
}
