package giga

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestAvailClientServer(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	env := newTestEnv(registry)
	var nodes []*testNode
	activeKeys := keys[:3] // keys are sorted by weight, so that's ok.
	totalWeight := uint64(0)
	for _, k := range activeKeys {
		totalWeight += committee.Weight(k.Public())
	}
	require.True(t, totalWeight >= committee.CommitQuorum())
	t.Logf("Committee with %d nodes, running %d", len(keys), len(activeKeys))
	for _, key := range activeKeys {
		nodes = append(nodes, env.AddNode(key))
	}

	totalBlocks := 3 * avail.BlocksPerLane
	firstBlock := nodes[0].data.Registry().FirstBlock()
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Log("Spawn network.")
		s.SpawnBg(func() error { return env.Run(ctx) })
		t.Log("Spawn a fake unconnected node0 to generate some conflicting blocks and push them to node2.")
		fakeNode0, err := consensus.NewState(&consensus.Config{
			Key:                keys[0],
			ViewTimeout:        defaultViewTimeout,
			PersistentStateDir: utils.Some(t.TempDir()),
		}, nodes[0].data)
		if err != nil {
			return fmt.Errorf("consensus.NewState(): %w", err)
		}
		s.SpawnBgNamed("fakeNode0", func() error { return utils.IgnoreCancel(fakeNode0.Run(ctx)) })
		for range min(avail.BlocksPerLane, 4) {
			a := fakeNode0.Avail()
			b := utils.OrPanic1(a.ProduceLocalBlock(a.NextBlock(a.PublicKey()), types.GenPayload(rng)))
			utils.OrPanic(nodes[2].consensus.Avail().PushBlock(ctx, b))
		}
		t.Logf("Run block production")
		for _, node := range nodes {
			rng := rng.Split()
			s.Spawn(func() error {
				a := node.consensus.Avail()
				lane := a.PublicKey()
				for range totalBlocks {
					n := a.NextBlock(lane)
					if err := a.WaitForLocalCapacity(ctx, n); err != nil {
						return fmt.Errorf("waitForLocalCapacity(): %w", err)
					}
					if _, err := a.ProduceLocalBlock(n, types.GenPayload(rng)); err != nil {
						return fmt.Errorf("produceLocalBlock(): %w", err)
					}
				}
				return nil
			})
		}
		t.Logf("Await sequenced blocks")
		for offset := range types.GlobalBlockNumber(totalBlocks * len(nodes)) {
			n := firstBlock + offset
			want, err := nodes[0].data.GlobalBlock(ctx, n)
			if err != nil {
				return err
			}
			h := types.GenAppHash(rng)
			for _, node := range nodes {
				got, err := node.data.GlobalBlock(ctx, n)
				if err != nil {
					return err
				}
				if err := utils.TestDiff(want, got); err != nil {
					return err
				}
				if err := node.data.PushAppHash(ctx, n, h); err != nil {
					return fmt.Errorf("node.data.PushAppHash(): %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
