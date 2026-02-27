package giga

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestAvailClientServer(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	t.Logf("Committee with 4 nodes, where 1 is down.")
	committee, keys := types.GenCommittee(rng, 4)
	env := newTestEnv(committee)
	var nodes []*testNode
	for _, key := range keys[:3] {
		nodes = append(nodes, env.AddNode(key))
	}

	totalBlocks := 3 * avail.BlocksPerLane
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Log("Spawn network.")
		s.SpawnBg(func() error { return env.Run(ctx) })
		t.Log("Spawn a fake unconnected node0 to generate some conflicting blocks and push them to node2.")
		fakeNode0, err := consensus.NewState(&consensus.Config{
			Key:         keys[0],
			ViewTimeout: defaultViewTimeout,
		}, nodes[0].data)
		if err != nil {
			return fmt.Errorf("consensus.NewState(): %w", err)
		}
		s.SpawnBgNamed("fakeNode0", func() error { return utils.IgnoreCancel(fakeNode0.Run(ctx)) })
		for range min(avail.BlocksPerLane, 4) {
			b := utils.OrPanic1(fakeNode0.ProduceBlock(ctx, types.GenPayload(rng)))
			utils.OrPanic(nodes[2].consensus.Avail().PushBlock(ctx, b))
		}
		t.Logf("Run block production")
		for _, node := range nodes {
			rng := rng.Split()
			s.Spawn(func() error {
				for range totalBlocks {
					if _, err := node.consensus.ProduceBlock(ctx, types.GenPayload(rng)); err != nil {
						return fmt.Errorf("produceBlock(): %w", err)
					}
				}
				return nil
			})
		}
		t.Logf("Await sequenced blocks")
		for n := range types.GlobalBlockNumber(totalBlocks * len(nodes)) {
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
				if err := node.data.PushAppHash(n, h); err != nil {
					return fmt.Errorf("node.data.PushAppHash(): %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
