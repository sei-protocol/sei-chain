package evmonlyapp

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// epochRegistryModule is the FlatKV module the epoch registry row is stored under.
const epochRegistryModule = "autobahn"

// epochRegistryKey is the FlatKV key of the epoch registry row.
const epochRegistryKey = "epoch_registry"

// EpochRegistry is the retained epoch window an application advances.
type EpochRegistry struct {
	state     utils.Mutex[*epochRegistryState]
	firstRoad atypes.RoadHeight
	weights   map[atypes.PublicKey]uint64
}

type epochRegistryState struct{ registry *atypes.EpochRegistry }

// OpenEpochRegistry returns the epoch registry storage holds, or the genesis registry for validators
// when storage holds none. The genesis registry starts epoch 0 at firstRoad and lasts length roads
// per epoch.
func OpenEpochRegistry(
	storage *bootstrap.GigaStorageManager,
	firstRoad, length atypes.RoadHeight,
	validators abevm.CommitteeWeights,
) (*EpochRegistry, error) {
	weights, err := epochWeights(validators)
	if err != nil {
		return nil, err
	}
	row, found := storage.StateDB().SC().Get(epochRegistryModule, []byte(epochRegistryKey))
	var registry *atypes.EpochRegistry
	if found {
		registry, err = epoch.Decode(row)
		if err != nil {
			return nil, fmt.Errorf("open epoch registry: %w", err)
		}
	} else {
		registry, err = atypes.GenesisRegistry(weights, firstRoad, length)
		if err != nil {
			return nil, err
		}
	}
	return &EpochRegistry{state: utils.NewMutex(&epochRegistryState{registry: registry}), firstRoad: firstRoad, weights: weights}, nil
}

// Registry returns the application's retained epoch window.
func (e *EpochRegistry) Registry() *atypes.EpochRegistry {
	for state := range e.state.Lock() {
		return state.registry
	}
	panic("unreachable")
}

// epochWeights maps each validator with positive power to its voting weight.
func epochWeights(validators abevm.CommitteeWeights) (map[atypes.PublicKey]uint64, error) {
	weights := make(map[atypes.PublicKey]uint64, len(validators))
	for key, weight := range validators {
		if weight > 0 {
			weights[key] = weight
		}
	}
	return weights, nil
}

// Changes returns the FlatKV write for road and advances the registry to the window that road
// produces. The genesis road and each road that closes an epoch write the registry row. A repeat
// of the road that produced the newest epoch discards that epoch and derives it again. Other roads
// write nothing.
func (e *EpochRegistry) Changes(road atypes.RoadHeight) ([]gigatypes.MiscChange, error) {
	for state := range e.state.Lock() {
		registry := state.registry
		if registry.DerivedOn(road) {
			var err error
			registry, err = registry.DiscardNewest()
			if err != nil {
				return nil, err
			}
		}
		advanced, err := registry.Advance(road, e.weights)
		if err != nil {
			return nil, fmt.Errorf("advance epoch registry at road %d: %w", road, err)
		}
		if next, ok := advanced.Get(); ok {
			state.registry = next
		} else if road != e.firstRoad {
			return nil, nil
		}
		return []gigatypes.MiscChange{{
			Module: epochRegistryModule,
			Key:    []byte(epochRegistryKey),
			Value:  epoch.Encode(state.registry),
		}}, nil
	}
	panic("unreachable")
}

func (a *evmOnlyApplication) EpochRegistry() (*atypes.EpochRegistry, error) {
	return a.epochRegistry.Registry(), nil
}
