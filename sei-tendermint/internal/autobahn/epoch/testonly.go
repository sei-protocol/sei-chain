package epoch

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// LatestEpoch returns the most recently activated epoch. For use in tests only.
func (r *Registry) LatestEpoch() *types.Epoch {
	for s := range r.state.RLock() {
		return s.m[s.latest]
	}
	panic("unreachable")
}

// GenRegistry generates a random Registry of the given committee size,
// starting at a random epoch index (0–1). Seeds the neighboring epochs
// so the window covers [startEpoch-1, startEpoch].
// Returns the registry, secret keys, and the starting epoch index.
// Intended for use in tests only.
func GenRegistry(rng utils.Rng, size int) (*Registry, []types.SecretKey, types.EpochIndex) {
	sks := utils.GenSliceN(rng, size, types.GenSecretKey)
	weights := map[types.PublicKey]uint64{}
	for _, sk := range sks {
		weights[sk.Public()] = 1000 + uint64(rng.Intn(1000)) //nolint:gosec
	}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	firstBlock := types.GenGlobalBlockNumber(rng) % 1000000
	// Limit to {0, 1}: GenRegistryAt for either value always includes epoch 0
	// ([0] or [0,1]), so tests that build CommitQC chains from road index 0
	// can still look up epoch 0 in the window. Higher values would require all
	// such tests to anchor their chains at FirstRoad(startEpoch).
	startEpoch := types.EpochIndex(rng.Intn(2)) //nolint:gosec
	r := makeRegistryAt(committee, firstBlock, startEpoch)
	return r, sks, startEpoch
}

// GenRegistryAt generates a Registry of the given committee size centered on startEpoch.
// Seeds [startEpoch-1, startEpoch] so DuoAt(FirstRoad(startEpoch)) works.
// Intended for use in tests only.
func GenRegistryAt(rng utils.Rng, size int, startEpoch types.EpochIndex) (*Registry, []types.SecretKey) {
	sks := utils.GenSliceN(rng, size, types.GenSecretKey)
	weights := map[types.PublicKey]uint64{}
	for _, sk := range sks {
		weights[sk.Public()] = 1000 + uint64(rng.Intn(1000)) //nolint:gosec
	}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	firstBlock := types.GenGlobalBlockNumber(rng) % 1000000
	return makeRegistryAt(committee, firstBlock, startEpoch), sks
}

func makeRegistryAt(committee *types.Committee, firstBlock types.GlobalBlockNumber, startEpoch types.EpochIndex) *Registry {
	registry := utils.OrPanic1(NewRegistry(committee, firstBlock, time.Now()))
	for s := range registry.state.Lock() {
		if startEpoch > 0 {
			utils.OrPanic1(registry.makeEpoch(s, startEpoch-1))
		}
		// Always seed startEpoch itself (no-op when startEpoch==0, genesis already exists).
		utils.OrPanic1(registry.makeEpoch(s, startEpoch))
		s.latest = startEpoch
	}
	return registry
}
