package p2p

import (
	"context"
	"fmt"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// RegistryEpochHook is the epoch hook backed by the router's registry.
type RegistryEpochHook struct {
	registry *epoch.Registry
}

// NewRegistryEpochHook returns the epoch hook for registry.
func NewRegistryEpochHook(registry *epoch.Registry) RegistryEpochHook {
	return RegistryEpochHook{registry: registry}
}

// EpochState returns the epoch state carried by the first road of an epoch,
// and none for every other road. The first road blocks until the epoch the
// previous road derives is registered.
func (h RegistryEpochHook) EpochState(ctx context.Context, road atypes.RoadIndex) (utils.Option[proxy.EpochState], error) {
	if err := h.waitForOpeningEpoch(ctx, road); err != nil {
		return utils.None[proxy.EpochState](), err
	}
	carried, err := h.registry.FirstRoadState(road)
	if err != nil {
		return utils.None[proxy.EpochState](), err
	}
	state, ok := carried.Get()
	if !ok {
		return utils.None[proxy.EpochState](), nil
	}
	return utils.Some(proxy.EpochState{
		EpochRegistry:    state.Registry,
		CurrentCommittee: state.Committee,
	}), nil
}

// waitForOpeningEpoch blocks until the epoch derived by the previous road is
// registered, when road opens an epoch. Any other road returns immediately.
// A road can be finalized before the previous road has committed.
func (h RegistryEpochHook) waitForOpeningEpoch(ctx context.Context, road atypes.RoadIndex) error {
	if road == 0 {
		return nil
	}
	closing, ok := h.registry.ClosingEpoch(road - 1).Get()
	if !ok {
		return nil
	}
	if _, err := h.registry.WaitForEpoch(ctx, closing+2); err != nil {
		return fmt.Errorf("epoch %d: %w", closing+2, err)
	}
	return nil
}

// RoadCommitted derives the epoch two ahead when road closes an epoch, from the
// validator set committed with road. Any other road leaves the registry unchanged.
func (h RegistryEpochHook) RoadCommitted(road atypes.RoadIndex, vals []proxy.ValidatorUpdate) error {
	closing, ok := h.registry.ClosingEpoch(road).Get()
	if !ok {
		return nil
	}
	weights, err := committeeWeights(vals)
	if err != nil {
		return fmt.Errorf("weights of closing epoch %d: %w", closing, err)
	}
	if err := h.registry.TryPushEpoch(road, weights); err != nil {
		return fmt.Errorf("derive epoch %d: %w", closing+2, err)
	}
	return nil
}

// committeeWeights maps a bonded validator set to Autobahn voting power.
// Validators with non-positive power are left out.
func committeeWeights(vals []proxy.ValidatorUpdate) (map[atypes.PublicKey]uint64, error) {
	weights := make(map[atypes.PublicKey]uint64, len(vals))
	for _, v := range vals {
		if v.Power <= 0 {
			continue
		}
		apk := v.PubKey
		if _, dup := weights[apk]; dup {
			return nil, fmt.Errorf("duplicate public key %s", apk)
		}
		power, ok := utils.SafeCast[uint64](v.Power)
		if !ok {
			return nil, fmt.Errorf("validator power %d does not fit uint64", v.Power)
		}
		weights[apk] = power
	}
	return weights, nil
}
