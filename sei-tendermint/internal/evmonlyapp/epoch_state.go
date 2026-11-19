package evmonlyapp

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// epochRegistryModule is the FlatKV module the epoch registry row is stored under.
const epochRegistryModule = "autobahn"

// epochRegistryKey is the FlatKV key of the epoch registry row.
const epochRegistryKey = "epoch_registry"

// EpochRegistry is the epoch registry an application writes. Its window is the one consensus
// waits on.
type EpochRegistry struct {
	registry *epoch.Registry
	schedule epoch.GenesisSchedule
	weights  map[atypes.PublicKey]uint64
}

// OpenEpochRegistry returns the epoch registry storage holds, or the genesis registry for validators
// when storage holds none.
func OpenEpochRegistry(
	storage *bootstrap.GigaStorageManager,
	schedule epoch.GenesisSchedule,
	validators []proxy.ValidatorUpdate,
) (*EpochRegistry, error) {
	weights, err := epochWeights(validators)
	if err != nil {
		return nil, err
	}
	row, found := storage.StateDB().SC().Get(epochRegistryModule, []byte(epochRegistryKey))
	if !found {
		row, err = epoch.GenesisRow(weights, schedule)
		if err != nil {
			return nil, err
		}
	}
	epochs, err := epoch.Decode(row)
	if err != nil {
		return nil, fmt.Errorf("open epoch registry: %w", err)
	}
	registry, err := epoch.NewRegistry(epochs)
	if err != nil {
		return nil, err
	}
	return &EpochRegistry{registry: registry, schedule: schedule, weights: weights}, nil
}

// Registry returns the window consensus reads.
func (e *EpochRegistry) Registry() *epoch.Registry { return e.registry }

// epochWeights maps each validator with positive power to its voting weight.
func epochWeights(validators []proxy.ValidatorUpdate) (map[atypes.PublicKey]uint64, error) {
	weights := make(map[atypes.PublicKey]uint64, len(validators))
	for _, v := range validators {
		if v.Power <= 0 {
			continue
		}
		if _, dup := weights[v.PubKey]; dup {
			return nil, fmt.Errorf("duplicate validator public key %s", v.PubKey)
		}
		weight, ok := utils.SafeCast[uint64](v.Power)
		if !ok {
			return nil, fmt.Errorf("validator power %d does not fit uint64", v.Power)
		}
		weights[v.PubKey] = weight
	}
	return weights, nil
}

// epochRegistryChanges returns the FlatKV write for road and advances the registry to the window
// that road produces. The genesis road and each road that closes an epoch write the registry row.
// Other roads write nothing.
//
// The registry window is stored here, before the block commits. The process exits when the commit
// fails, so a restart rebuilds the window from the row that did commit.
func (e *EpochRegistry) epochRegistryChanges(road atypes.RoadHeight) ([]gigatypes.MiscChange, error) {
	window := e.registry.Epochs()
	advanced, err := epoch.Advance(window, road, e.weights)
	if err != nil {
		return nil, fmt.Errorf("advance epoch registry at road %d: %w", road, err)
	}
	if next, ok := advanced.Get(); ok {
		e.registry.Store(next)
		window = next
	} else if road != e.schedule.FirstRoad {
		return nil, nil
	}
	return []gigatypes.MiscChange{{
		Module: epochRegistryModule,
		Key:    []byte(epochRegistryKey),
		Value:  epoch.Encode(window),
	}}, nil
}
