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
	sks := utils.GenSliceN(rng, size, types.GenSecretKey)
	weights := map[types.PublicKey]uint64{}
	for _, sk := range sks {
		weights[sk.Public()] = 1000 + uint64(rng.Intn(1000)) //nolint:gosec
	}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	firstBlock := types.GenGlobalBlockNumber(rng) % 1000000
	registry := utils.OrPanic1(NewRegistry(committee, firstBlock, time.Now()))
	return registry, sks
}

// MustFillFromEnd registers C_{end+2} from genesis weights. Tests that need an
// epoch above 1 without running execution use this.
func (r *Registry) MustFillFromEnd(end types.EpochIndex, keys []types.SecretKey) *types.Epoch {
	genesis := r.MustEpoch(0).Committee()
	weights := make(map[types.PublicKey]uint64, len(keys))
	for _, k := range keys {
		if w := genesis.Weight(k.Public()); w > 0 {
			weights[k.Public()] = w
		}
	}
	utils.OrPanic(r.AddEpoch(end, weights))
	return r.MustEpoch(end + 2)
}

// MustEpoch returns the registered epoch at i. Panics if it is missing.
func (r *Registry) MustEpoch(i types.EpochIndex) *types.Epoch {
	ep, err := r.EpochByIndex(i)
	if err != nil {
		panic(err)
	}
	return ep
}
