package consensus

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestConsensusMaxRate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 4)
		committee := registry.MustEpoch(0).Committee()
		states := make([]*State, len(keys))
		for i, key := range keys {
			state, err := NewState(&Config{
				Key:             key,
				ViewTimeout:     hourTimeout,
				ProposalTimeout: time.Hour,
				MaxRate:         1,
			}, newTestDataState(t, registry))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, state.Close()) })
			states[i] = state
		}

		var emptiedWeight uint64
		err := scope.Run(t.Context(), func(ctx context.Context, sc scope.Scope) error {
			sc.SpawnBg(func() error { return utils.IgnoreCancel(RunTestNetwork(ctx, states)) })
			for _, state := range states {
				sc.SpawnBg(func() error { return utils.IgnoreCancel(state.Data().Run(ctx)) })
				sc.SpawnBg(func() error { return utils.IgnoreCancel(state.Run(ctx)) })
				productionRng := rng.Split()
				sc.SpawnBg(func() error {
					a := state.Avail()
					lane := a.LocalLane().OrPanic("local lane")
					for {
						n := a.NextBlock(lane)
						if err := a.WaitForCapacity(ctx, lane, n); err != nil {
							return utils.IgnoreCancel(err)
						}
						if _, err := a.ProduceLocalBlock(lane, n, types.GenPayload(productionRng)); err != nil {
							return err
						}
					}
				})
				sc.SpawnBg(func() error {
					for n := state.Data().First(); ; n++ {
						if _, err := state.Data().SuperBlock(ctx, n); err != nil {
							return utils.IgnoreCancel(err)
						}
						if err := state.Data().PushAppHash(ctx, n, types.AppHash{}, nil); err != nil {
							return utils.IgnoreCancel(err)
						}
					}
				})
			}
			synctest.Wait()
			for _, state := range states {
				if state.specLimiter.Tokens() <= 0 {
					emptiedWeight += committee.Weight(state.Avail().PublicKey())
				}
			}
			return nil
		})
		require.NoError(t, err)
		require.Greater(t, emptiedWeight, committee.Faulty())
	})
}
