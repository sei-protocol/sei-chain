package giga

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestConsensusClientServer(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys, _ := epoch.GenRegistry(rng, 7)
	committee := registry.LatestEpoch().Committee()
	env := newTestEnv(registry)
	// Run only a subset of replicas, to enforce timeouts.
	var nodes []*testNode
	for _, key := range types.TestKeysWithWeight(committee, keys, committee.CommitQuorum()) {
		nodes = append(nodes, env.AddNode(key))
	}
	firstBlock := nodes[0].data.Registry().FirstBlock()
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		var wantAppProposal utils.Option[*types.AppProposal]
		for offset := range types.GlobalBlockNumber(20) {
			idx := firstBlock + offset
			t.Logf("[%v] Push a block.", idx)
			node := nodes[rng.Intn(len(env.nodes))]
			a := node.consensus.Avail()
			b, err := a.ProduceLocalBlock(a.NextBlock(a.PublicKey()), types.GenPayload(rng))
			if err != nil {
				return fmt.Errorf("ds.ProduceLocalBlock(): %w", err)
			}
			want := &types.GlobalBlock{
				Header:        b.Msg().Block().Header(),
				Payload:       b.Msg().Block().Payload(),
				GlobalNumber:  idx,
				FinalAppState: wantAppProposal,
			}
			p := types.NewAppProposal(
				idx,
				types.RoadIndex(offset),
				types.GenAppHash(rng),
				registry.LatestEpoch().EpochIndex(),
			)
			wantAppProposal = utils.Some(p)
			for _, n := range nodes {
				t.Logf("[%v] Wait for it to be final.", idx)
				got, err := n.data.GlobalBlock(ctx, idx)
				if err != nil {
					return fmt.Errorf("ds.Block(): %w", err)
				}
				qc, err := n.data.QC(ctx, idx)
				if err != nil {
					return fmt.Errorf("ds.QC(): %w", err)
				}
				want.Timestamp = qc.QC().Proposal().BlockTimestamp(idx).OrPanic("global block not in QC")
				if err := utils.TestDiff(want, got); err != nil {
					return err
				}
				if err := n.data.PushAppHash(ctx, idx, p.AppHash()); err != nil {
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
