package evmonlyapp

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
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
	validators []proxy.ValidatorUpdate,
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

// ClosingEpoch returns the epoch road is the last road of, when the retained window holds it.
func (e *EpochRegistry) ClosingEpoch(road atypes.RoadHeight) utils.Option[atypes.EpochIndex] {
	for state := range e.state.Lock() {
		return closingEpoch(state.registry, road)
	}
	panic("unreachable")
}

// closingEpoch returns the epoch of registry that road is the last road of, if registry holds it.
func closingEpoch(registry *atypes.EpochRegistry, road atypes.RoadHeight) utils.Option[atypes.EpochIndex] {
	for _, epoch := range registry.Epochs() {
		if epoch.RoadRange().Next == road+1 {
			return utils.Some(epoch.EpochIndex())
		}
	}
	return utils.None[atypes.EpochIndex]()
}

// Changes returns the FlatKV write for road and advances the registry to the window that road
// produces. The genesis road and each road that closes an epoch write the registry row; the epoch
// a closing road derives is committed to the weights staked returns, or to the configured
// validators when it returns none. A repeat of the road that produced the newest epoch discards
// that epoch and derives it again. Other roads write nothing.
func (e *EpochRegistry) Changes(
	road atypes.RoadHeight,
	staked func() (map[atypes.PublicKey]uint64, error),
) ([]gigatypes.MiscChange, error) {
	for state := range e.state.Lock() {
		registry := state.registry
		if registry.DerivedOn(road) {
			var err error
			registry, err = registry.DiscardNewest()
			if err != nil {
				return nil, err
			}
		}
		weights, err := e.committeeWeights(registry, road, staked)
		if err != nil {
			return nil, err
		}
		advanced, err := registry.Advance(road, weights)
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

// committeeWeights returns the weights the epoch road derives is committed to: staked's when road
// closes an epoch of registry and staked returns some, and the configured validators' otherwise.
// staked is read only on a closing road.
func (e *EpochRegistry) committeeWeights(
	registry *atypes.EpochRegistry,
	road atypes.RoadHeight,
	staked func() (map[atypes.PublicKey]uint64, error),
) (map[atypes.PublicKey]uint64, error) {
	if !closingEpoch(registry, road).IsPresent() {
		return e.weights, nil
	}
	weights, err := staked()
	if err != nil {
		return nil, fmt.Errorf("read staking's committee at road %d: %w", road, err)
	}
	if len(weights) == 0 {
		return e.weights, nil
	}
	return weights, nil
}

// stakingWeights returns the voting weight of each member of the bonded set staking snapshotted
// at the end of the latest epoch in state, or none before its first snapshot.
func stakingWeights(state precompiles.State) (map[atypes.PublicKey]uint64, error) {
	members, err := staking.EpochValidators(state)
	if err != nil {
		return nil, err
	}
	weights := make(map[atypes.PublicKey]uint64, len(members))
	for _, member := range members {
		// PublicKeyFromBytes checks only the length, and a common.Hash is always 32 bytes.
		key := utils.OrPanic1(atypes.PublicKeyFromBytes(member.ConsensusPubkey[:]))
		if _, dup := weights[key]; dup {
			return nil, fmt.Errorf("duplicate consensus key %s in staking's epoch snapshot", key)
		}
		weights[key] = member.Power
	}
	return weights, nil
}

func (a *evmOnlyApplication) EpochRegistry() (*atypes.EpochRegistry, error) {
	return a.epochRegistry.Registry(), nil
}
