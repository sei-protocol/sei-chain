package epoch

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// LatestEpoch returns the highest-index registered epoch. For use in tests only.
// Do not use this to stamp CommitQC views for an arbitrary road — use
// EpochAtTip (or EpochAt) so View.EpochIndex matches the road's epoch when
// GenRegistry starts away from genesis.
func (r *Registry) LatestEpoch() *types.Epoch {
	for s := range r.state.RLock() {
		var best types.EpochIndex
		var ep *types.Epoch
		for idx, e := range s.m {
			if ep == nil || idx > best {
				best = idx
				ep = e
			}
		}
		if ep == nil {
			panic("registry has no epochs")
		}
		return ep
	}
	panic("unreachable")
}

// EpochAtTip is the epoch for the next CommitQC after prev (road 0 if none).
// Intended for test CommitQC chains when GenRegistry's start epoch may be ≫ 0.
func (r *Registry) EpochAtTip(prev utils.Option[*types.CommitQC]) *types.Epoch {
	return utils.OrPanic1(r.EpochAt(types.NextIndexOpt(prev)))
}

// GenRegistry generates a random Registry of the given committee size, starting
// at a random epoch N ∈ [0, 100]. Seeds only the duo at N ({N−1 if N>0, N}),
// plus genesis 0 from NewRegistry — not M+1/M+2 placeholders and not a dense
// 0..N fill. startEpoch is drawn from rng.Split() so it does not depend on how
// many draws committee construction consumes.
// Callers building CommitQC chains must use EpochAtTip / EpochAt(road), not
// LatestEpoch(), for View.EpochIndex. Tests that need N+1/N+2 must EnsureEpoch
// (or AdvanceIfNeeded) themselves.
// Returns the registry, secret keys, and N.
// Intended for use in tests only.
func GenRegistry(rng utils.Rng, size int) (*Registry, []types.SecretKey, types.EpochIndex) {
	startEpoch := types.EpochIndex(rng.Split().Intn(101)) //nolint:gosec
	r, sks := GenRegistryAt(rng, size, startEpoch)
	return r, sks, startEpoch
}

// GenRegistryAt generates a Registry of the given committee size centered on
// startEpoch. Seeds only {startEpoch−1 (if >0), startEpoch} via EnsureDuoAt —
// not startEpoch+1/+2 (callers add those when needed).
// Intended for use in tests only.
func GenRegistryAt(rng utils.Rng, size int, startEpoch types.EpochIndex) (*Registry, []types.SecretKey) {
	sks := utils.GenSliceN(rng, size, types.GenSecretKey)
	weights := map[types.PublicKey]uint64{}
	for _, sk := range sks {
		weights[sk.Public()] = 1000 + uint64(rng.Intn(1000)) //nolint:gosec
	}
	committee := utils.OrPanic1(types.NewCommittee(weights))
	// Production genesis starts at global block 0; randomizing this would detach
	// empty-store CommitQC road 0 from the registry's genesis epoch.
	const firstBlock types.GlobalBlockNumber = 0
	return makeRegistryAt(committee, firstBlock, startEpoch), sks
}

// GenRegistryTip is GenRegistryAt on a random M ∈ [1, 100] so Prev is always
// present ({M−1, M} only). Prefer this over GenRegistryAt(..., 0) for tests that
// need a non-genesis Current.
func GenRegistryTip(rng utils.Rng, size int) (*Registry, []types.SecretKey, types.EpochIndex) {
	m := types.EpochIndex(1 + rng.Split().Intn(100)) //nolint:gosec
	r, sks := GenRegistryAt(rng, size, m)
	return r, sks, m
}

func makeRegistryAt(committee *types.Committee, firstBlock types.GlobalBlockNumber, startEpoch types.EpochIndex) *Registry {
	registry := utils.OrPanic1(NewRegistry(committee, firstBlock, time.Now()))
	// Duo at startEpoch only; no placeholder +1/+2 (unlike SetupInitialDuo's
	// CommitQC-span path). Genesis 0 always exists from NewRegistry.
	registry.EnsureDuoAt(FirstRoad(startEpoch))
	return registry
}
