package consensus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tendermint/tendermint/internal/autobahn/config"
	"github.com/tendermint/tendermint/internal/autobahn/data"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/grpcutils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/service"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/tcp"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

func TestClientServer(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 7)
	active := committee.CommitQuorum()
	viewTimeout := func(view types.View) time.Duration {
		leader := committee.Leader(view)
		for _, id := range keys[:active] {
			if id.Public() == leader {
				return time.Hour
			}
		}
		return 0
	}

	if err := service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		ds := data.NewState(&data.Config{
			Committee: committee,
		}, utils.None[data.BlockStore]())
		s.SpawnBgNamed("data.Run()", func() error { return ds.Run(ctx) })

		var states []*State
		var peerCfgs []*config.PeerConfig
		// Run only a subset of replicas, to enforce timeouts.
		for i, key := range keys[:committee.CommitQuorum()] {
			cs := NewState(&Config{
				Key:         key,
				ViewTimeout: viewTimeout,
			}, ds)
			s.SpawnBgNamed(fmt.Sprintf("consensus[%d]", i), func() error { return cs.Run(ctx) })
			states = append(states, cs)

			addr := tcp.TestReserveAddr()
			peerCfgs = append(peerCfgs, &config.PeerConfig{
				Key:        utils.Some(key.Public()),
				Address:    addr.String(),
				RetryDelay: utils.Some(utils.Duration(100 * time.Millisecond)),
			})
			server := grpcutils.NewServer()
			cs.Register(server)
			s.SpawnBgNamed(fmt.Sprintf("server[%d]", i), func() error { return grpcutils.RunServer(ctx, server, addr) })
		}
		for i, cs := range states {
			s.SpawnBgNamed(fmt.Sprintf("client[%d]", i), func() error { return cs.RunClientPool(ctx, peerCfgs) })
		}
		var wantAppProposal utils.Option[*types.AppProposal]
		for idx := range types.GlobalBlockNumber(20) {
			t.Logf("[%v] Push a block.", idx)
			s := states[rng.Intn(len(states))]
			b, err := s.ProduceBlock(ctx, types.GenPayload(rng))
			if err != nil {
				return fmt.Errorf("ds.ProduceBlock(): %w", err)
			}

			t.Logf("[%v] Wait for it to be final.", idx)
			got, err := ds.GlobalBlock(ctx, idx)
			if err != nil {
				return fmt.Errorf("ds.Block(): %w", err)
			}
			want := &types.GlobalBlock{
				Header:        b.Header(),
				Payload:       b.Payload(),
				GlobalNumber:  idx,
				FinalAppState: wantAppProposal,
			}
			if err := utils.TestDiff(want, got); err != nil {
				return err
			}

			t.Logf("[%v] Wait for AppHash consensus.", idx)
			appHash := types.GenAppHash(rng)
			p := types.NewAppProposal(
				idx,
				types.RoadIndex(idx),
				appHash,
			)
			wantAppProposal = utils.Some(p)
			if err := ds.PushAppHash(idx, appHash); err != nil {
				return fmt.Errorf("ds.PushAppProposal(): %w", err)
			}
			for _, cs := range states {
				got, _, err := cs.avail.WaitForAppQC(ctx, p.RoadIndex())
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
