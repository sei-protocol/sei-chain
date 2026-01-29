package avail

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tendermint/tendermint/internal/autobahn/data"
	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
)

func TestClientServer(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	active := keys[:3]
	totalBlocks := 3 * blocksPerLane
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		t.Logf("Committee with 4 nodes, where 1 is down.")
		var peerCfgs []*config.PeerConfig
		availStates := map[types.PublicKey]*State{}

		t.Logf("Spawns servers")
		for _, key := range active {
			ds := data.NewState(&data.Config{Committee: committee}, utils.None[data.BlockStore]())
			s.SpawnBg(func() error { return ds.Run(ctx) })
			as := NewState(key, ds)
			s.SpawnBg(func() error { return as.Run(ctx) })
			availStates[key.Public()] = as

			addr := tcp.TestReserveAddr()
			peerCfgs = append(peerCfgs, &config.PeerConfig{
				Key:        utils.Some(key.Public()),
				Address:    addr.String(),
				RetryDelay: utils.Some(utils.Duration(time.Millisecond * 100)),
			})
			server := grpcutils.NewServer()
			ds.Register(server)
			as.Register(server)
			s.SpawnBg(func() error {
				return grpcutils.RunServer(ctx, server, addr)
			})
		}
		t.Logf("Spawn clients.")
		for _, client := range active {
			s.SpawnBg(func() error { return availStates[client.Public()].Data().RunClientPool(ctx, peerCfgs) })
			for _, peerCfg := range peerCfgs {
				s.SpawnBgNamed("client", func() error {
					return availStates[client.Public()].RunClient(ctx, peerCfg)
				})
			}
		}

		producerTask := func(rng utils.Rng, state *State, producer types.LaneID) func() error {
			return func() error {
				for range totalBlocks {
					if _, err := state.ProduceBlock(ctx, producer, types.GenPayload(rng)); err != nil {
						if utils.IgnoreCancel(err) == nil {
							return nil
						}
						return fmt.Errorf("produceBlock(): %w", err)
					}
				}
				return nil
			}
		}
		t.Logf("Spawn task sending corrupted data of node 0 to node 2.")
		s.SpawnBg(producerTask(rng.Split(), availStates[active[2].Public()], active[0].Public()))
		t.Logf("Spawn block producing tasks.")
		for _, producer := range active {
			s.SpawnBg(producerTask(rng.Split(), availStates[producer.Public()], producer.Public()))
		}
		as2 := availStates[active[2].Public()]
		for i := 0; ; i += 1 {
			t.Logf("iteration %v", i)
			prev := as2.LastCommitQC().Load()
			t.Logf("Check if enough blocks have been finalized.")
			done := true
			for _, producer := range active {
				next := types.LaneRangeOpt(prev, producer.Public()).Next()
				t.Logf("%v: %v blocks", producer, next)
				done = done && next >= types.BlockNumber(totalBlocks)
			}
			if done {
				return nil
			}

			t.Logf("Wait for new Proposal.")
			laneQCs, err := as2.WaitForLaneQCs(ctx, prev)
			if err != nil {
				return fmt.Errorf("state.WaitForNewLaneQCs(): %w", err)
			}
			qc := makeCommitQC(rng, committee, keys, prev, laneQCs, as2.LastAppQC())
			t.Logf("Push new commit.")
			if err := as2.PushCommitQC(ctx, qc); err != nil {
				return fmt.Errorf("state.PushCommitQC(): %w", err)
			}
			t.Logf("Mark as executed")
			hash := types.GenAppHash(rng)
			n := qc.GlobalRange().Next - 1
			for _, node := range active {
				ds := availStates[node.Public()].Data()
				if _, err := ds.GlobalBlock(ctx, n); err != nil {
					return fmt.Errorf("ds.GlobalBlock(): %w", err)
				}
				if err := ds.PushAppHash(n, hash); err != nil {
					return fmt.Errorf("ds.PushAppHash(): %w", err)
				}
			}

			t.Logf("Check that a CommitQC was successfully reconstructed.")
			fullQC, err := as2.fullCommitQC(ctx, qc.Proposal().Index())
			if err != nil {
				return fmt.Errorf("state.fullCommitQC(): %w", err)
			}

			t.Logf("Check that the blocks were successfully pushed or synced to data state.")
			gr := fullQC.QC().GlobalRange()
			for i := gr.First; i < gr.Next; i++ {
				b, err := as2.Data().Block(ctx, i)
				if err != nil {
					return fmt.Errorf("ds.Block(): %w", err)
				}
				if err := utils.TestDiff(b.Header(), fullQC.Headers()[i-gr.First]); err != nil {
					return fmt.Errorf("snapshot: %w", err)
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
}
