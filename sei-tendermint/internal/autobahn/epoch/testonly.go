package epoch

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// GenRegistry generates a random Registry of the given committee size.
// Returns the generated secret keys as well.
// Intended for use in tests only.
func GenRegistry(rng utils.Rng, size int) (*Registry, []types.SecretKey) {
	committee, sks, _ := genCommittee(rng, size)
	firstBlock := types.GenGlobalBlockNumber(rng) % 1000000
	registry := utils.OrPanic1(NewRegistry(committee, firstBlock, time.Now(), utils.None[string]()))
	return registry, sks
}

// GenRegistryThrough returns a Registry with genesis epochs 0 and 1 plus
// live execution-derived committees through last.
func GenRegistryThrough(
	rng utils.Rng,
	size int,
	last types.EpochIndex,
) (*Registry, []types.SecretKey) {
	committee, sks, weights := genCommittee(rng, size)
	firstBlock := types.GenGlobalBlockNumber(rng) % 1000000
	registry := utils.OrPanic1(NewRegistry(committee, firstBlock, time.Now(), utils.None[string]()))
	for target := types.EpochIndex(2); target <= last; target++ {
		utils.OrPanic(registry.StageEpoch(target-2, weights))
		utils.OrPanic(registry.ActivateEpoch(target))
	}
	return registry, sks
}

func genCommittee(rng utils.Rng, size int) (*types.Committee, []types.SecretKey, map[types.PublicKey]uint64) {
	sks := utils.GenSliceN(rng, size, types.GenSecretKey)
	weights := map[types.PublicKey]uint64{}
	for _, sk := range sks {
		weights[sk.Public()] = 1000 + uint64(rng.Intn(1000)) //nolint:gosec
	}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	return committee, sks, weights
}

// StageAndActivate derives C_{endEpoch+2} from weights and publishes it.
func (r *Registry) StageAndActivate(endEpoch types.EpochIndex, weights map[types.PublicKey]uint64) error {
	if err := r.StageEpoch(endEpoch, weights); err != nil {
		return err
	}
	return r.ActivateEpoch(endEpoch + 2)
}

// MustEpoch returns the registered epoch at i. Panics if it is missing.
func (r *Registry) MustEpoch(i types.EpochIndex) *types.Epoch {
	ep, err := r.EpochByIndex(i)
	if err != nil {
		panic(err)
	}
	return ep
}
