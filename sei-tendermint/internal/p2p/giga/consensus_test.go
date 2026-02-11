package giga

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestConsensusClientServer(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 7)
	env := newTestEnv(committee)
	// Run only a subset of replicas, to enforce timeouts.
	var nodes []*testNode
	for _, key := range keys[:committee.CommitQuorum()] {
		nodes = append(nodes, env.AddNode(key))
	}
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		var wantAppProposal utils.Option[*types.AppProposal]
		for idx := range types.GlobalBlockNumber(20) {
			t.Logf("[%v] Push a block.", idx)
			b, err := nodes[rng.Intn(len(env.nodes))].consensus.ProduceBlock(ctx, types.GenPayload(rng))
			if err != nil {
				return fmt.Errorf("ds.ProduceBlock(): %w", err)
			}
			want := &types.GlobalBlock{
				Header:        b.Msg().Block().Header(),
				Payload:       b.Msg().Block().Payload(),
				GlobalNumber:  idx,
				FinalAppState: wantAppProposal,
			}
			p := types.NewAppProposal(
				idx,
				types.RoadIndex(idx),
				types.GenAppHash(rng),
			)
			wantAppProposal = utils.Some(p)
			for _, n := range nodes {
				t.Logf("[%v] Wait for it to be final.", idx)
				got, err := n.data.GlobalBlock(ctx, idx)
				if err != nil {
					return fmt.Errorf("ds.Block(): %w", err)
				}
				if err := utils.TestDiff(want, got); err != nil {
					return err
				}
				if err := n.data.PushAppHash(idx, p.AppHash()); err != nil {
					return fmt.Errorf("ds.PushAppProposal(): %w", err)
				}
			}
			for _, n := range nodes {
				t.Logf("[%v] Wait for AppHash consensus.", idx)
				got, _, err := n.consensus.Avail().WaitForAppQC(ctx, p.RoadIndex())
				if err != nil {
					return fmt.Errorf("cs.avail.WaitForAppQC(): %w", err)
				}
				if err := utils.TestDiff(p, got.Proposal()); err != nil {
					return err
				}
			}
		}
		t.Log("DONE")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
