package giga

import (
	"context"
	"errors"
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
	committee := registry.MustEpoch(0).Committee()
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

		// Equivocating peer: same key as node0, its own data/avail (not shared with
		// nodes[0]). Not joined to env.Run's mesh (testEnv keys nodes by PublicKey, so
		// a second node0 cannot be AddNode'd). Instead the test calls PushBlock on
		// node2 directly — same admission API gossip would use, without a real peer
		// connection. Do not Run a second consensus.State on nodes[0].data (that raced
		// and hung under -race).
		//
		// Wait until node2 already has an honest tip before pushing: under parent-hash
		// checks a corrupt genesis tip strands the lane (PushBlock drops forever).
		// After an honest tip, corrupt PushBlocks are stale or parent-mismatch drops.
		t.Log("Spawn task sending corrupted data of node 0 to node 2.")
		corrupt := newTestNode(registry, &consensus.Config{
			Key:         activeKeys[0],
			ViewTimeout: defaultViewTimeout,
		})
		corruptAvail := corrupt.consensus.Avail()
		a2 := nodes[2].consensus.Avail()
		lane0 := types.LaneID{Validator: activeKeys[0].Public(), Joined: 0}
		corruptRng := rng.Split()
		s.SpawnBg(func() error {
			if _, err := a2.Block(ctx, lane0, 0); err != nil && !errors.Is(err, types.ErrPruned) {
				return utils.IgnoreCancel(fmt.Errorf("a2.Block(lane0, 0): %w", err))
			}
			// Corrupt is never Run, so lane capacity stays at the initial window —
			// ProduceLocalBlock will fail loudly if this bound is raised past it.
			for range avail.BlocksPerLane {
				n := corruptAvail.NextBlock(lane0)
				b, err := corruptAvail.ProduceLocalBlock(lane0, n, types.GenPayload(corruptRng))
				if err != nil {
					return utils.IgnoreCancel(fmt.Errorf("corrupt.ProduceLocalBlock(%d): %w", n, err))
				}
				// PushBlock may drop (stale / parent-hash mismatch); that is fine.
				if err := a2.PushBlock(ctx, b); err != nil {
					return utils.IgnoreCancel(fmt.Errorf("a2.PushBlock(%d): %w", n, err))
				}
			}
			return nil
		})

		t.Logf("Run block production")
		for _, node := range nodes {
			rng := rng.Split()
			s.Spawn(func() error {
				a := node.consensus.Avail()
				lane := a.LocalLane().OrPanic("local")
				for range totalBlocks {
					n := a.NextBlock(lane)
					if err := a.WaitForCapacity(ctx, lane, n); err != nil {
						return fmt.Errorf("waitForLocalCapacity(): %w", err)
					}
					if _, err := a.ProduceLocalBlock(lane, n, types.GenPayload(rng)); err != nil {
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
				if err := node.data.PushAppHash(ctx, n, h, nil); err != nil {
					return fmt.Errorf("node.data.PushAppHash(): %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
