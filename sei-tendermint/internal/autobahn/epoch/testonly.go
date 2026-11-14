package epoch

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"google.golang.org/protobuf/proto"
)

// GenRegistry generates a random Registry of the given committee size.
// Returns the generated secret keys as well.
// Intended for use in tests only.
func GenRegistry(rng utils.Rng, size int) (*Registry, []types.SecretKey) {
	committee, sks, _ := genCommittee(rng, size)
	firstRoad := types.GenRoadIndex(rng)%1000000 + 1
	registry := utils.OrPanic1(NewRegistry(committee, firstRoad, time.Now(), utils.None[string]()))
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
	firstRoad := types.GenRoadIndex(rng)%1000000 + 1
	registry := utils.OrPanic1(NewRegistry(committee, firstRoad, time.Now(), utils.None[string]()))
	for target := types.EpochIndex(2); target <= last; target++ {
		utils.OrPanic(registry.TryPushEpoch(registry.LastRoad(target-2), weights))
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

// TestPushEpoch derives C_{endEpoch+2} from weights and publishes it.
func (r *Registry) TestPushEpoch(endEpoch types.EpochIndex, weights map[types.PublicKey]uint64) error {
	return r.TryPushEpoch(r.LastRoad(endEpoch), weights)
}

// MarshalSnapshot returns the protobuf encoding of the registry's persisted snapshot, the
// value that the epoch_registry state row holds.
func (r *Registry) MarshalSnapshot() []byte {
	return utils.OrPanic1(proto.Marshal(persistedRegistryConv.Encode(r.stateRecv.Load())))
}

// EpochEqual reports whether a and b have the same index, roads and committee.
func EpochEqual(a, b *types.Epoch) bool {
	return a.EpochIndex() == b.EpochIndex() && a.RoadRange() == b.RoadRange() && a.Committee().Equal(b.Committee())
}

// MustEpoch returns the registered epoch at i. Panics if it is missing.
func (r *Registry) MustEpoch(i types.EpochIndex) *types.Epoch {
	ep, err := r.Epoch(i)
	if err != nil {
		panic(err)
	}
	return ep
}
