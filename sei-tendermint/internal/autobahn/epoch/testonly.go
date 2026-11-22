package epoch

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// TestRegistry holds application epoch snapshots for tests.
// Intended for use in tests only.
type TestRegistry struct {
	state utils.Mutex[*testRegistryState]
}

type testRegistryState struct{ registry *types.EpochRegistry }

func newTestRegistry(registry *types.EpochRegistry) *TestRegistry {
	return &TestRegistry{state: utils.NewMutex(&testRegistryState{registry: registry})}
}

// Snapshot returns the current immutable registry. Intended for use in tests only.
func (r *TestRegistry) Snapshot() *types.EpochRegistry {
	for state := range r.state.Lock() {
		return state.registry
	}
	panic("unreachable")
}

// Epochs returns the retained epochs, oldest first. Intended for use in tests only.
func (r *TestRegistry) Epochs() []*types.Epoch { return r.Snapshot().Epochs() }

// GenRegistry generates a random Registry of the given committee size.
// Returns the generated secret keys as well.
// Intended for use in tests only.
func GenRegistry(rng utils.Rng, size int) (*TestRegistry, []types.SecretKey) {
	r, sks, _ := genRegistry(rng, size)
	return r, sks
}

// GenRegistryThrough returns a Registry with genesis epochs 0 and 1 plus
// execution-derived committees through last.
// Intended for use in tests only.
func GenRegistryThrough(rng utils.Rng, size int, last types.EpochIndex) (*TestRegistry, []types.SecretKey) {
	registry, sks, weights := genRegistry(rng, size)
	for target := types.EpochIndex(2); target <= last; target++ {
		utils.OrPanic(registry.TestTryPushEpoch(registry.LastRoad(target-2), weights))
	}
	return registry, sks
}

// TestGenesisRegistry returns a registry holding genesis epochs 0 and 1, both committed to
// committee, with epoch 0 starting at firstRoad and each epoch lasting length roads.
// Intended for use in tests only.
func TestGenesisRegistry(committee *types.Committee, firstRoad, length types.RoadHeight) (*TestRegistry, error) {
	weights := make(map[types.PublicKey]uint64, committee.Lanes().Len())
	for lane := range committee.Lanes().All() {
		weights[lane.Validator] = committee.Weight(lane.Validator)
	}
	registry, err := types.GenesisRegistry(weights, firstRoad, length)
	if err != nil {
		return nil, err
	}
	return newTestRegistry(registry), nil
}

func genRegistry(rng utils.Rng, size int) (*TestRegistry, []types.SecretKey, map[types.PublicKey]uint64) {
	sks := utils.GenSliceN(rng, size, types.GenSecretKey)
	weights := map[types.PublicKey]uint64{}
	for _, sk := range sks {
		weights[sk.Public()] = 1000 + uint64(rng.Intn(1000)) //nolint:gosec
	}
	firstHeight := types.GenRoadHeight(rng)%1000000 + 1
	snapshot, err := types.GenesisRegistry(weights, firstHeight, DefaultEpochLength)
	utils.OrPanic(err)
	registry := newTestRegistry(snapshot)
	return registry, sks, weights
}

// TestTryPushEpoch derives the next epoch when road is the road before the boundary of the newest
// epoch, and publishes it. Other roads are ignored. Intended for use in tests only.
func (r *TestRegistry) TestTryPushEpoch(road types.RoadHeight, weights map[types.PublicKey]uint64) error {
	for state := range r.state.Lock() {
		next, err := state.registry.Advance(road, weights)
		if err != nil {
			return err
		}
		registry, ok := next.Get()
		if !ok {
			return nil
		}
		state.registry = registry
		return nil
	}
	panic("unreachable")
}

// TestPushEpoch derives C_{endEpoch+2} from weights and publishes it.
// Intended for use in tests only.
func (r *TestRegistry) TestPushEpoch(endEpoch types.EpochIndex, weights map[types.PublicKey]uint64) error {
	return r.TestTryPushEpoch(r.LastRoad(endEpoch), weights)
}

// LastRoad returns the last road height of epoch idx, which may be past the retained window.
// Intended for use in tests only.
func (r *TestRegistry) LastRoad(idx types.EpochIndex) types.RoadHeight {
	return r.roadRange(idx).Next - 1
}

// roadRange returns the road range of epoch idx, extending the newest retained epoch by its length.
func (r *TestRegistry) roadRange(idx types.EpochIndex) types.RoadRange {
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
func (r *TestRegistry) Epoch(idx types.EpochIndex) (*types.Epoch, error) {
	return r.Snapshot().Epoch(idx)
}

// EpochAt returns the retained epoch containing road.
// Intended for use in tests only.
func (r *TestRegistry) EpochAt(road types.RoadHeight) (*types.Epoch, error) {
	for _, e := range r.Epochs() {
		if e.RoadRange().Has(road) {
			return e, nil
		}
	}
	return nil, fmt.Errorf("road %d: %w", road, types.ErrNotFound)
}

// MustEpoch returns the retained epoch at i. Panics if it is missing.
// Intended for use in tests only.
func (r *TestRegistry) MustEpoch(i types.EpochIndex) *types.Epoch {
	ep, err := r.Epoch(i)
	if err != nil {
		panic(err)
	}
	return ep
}
