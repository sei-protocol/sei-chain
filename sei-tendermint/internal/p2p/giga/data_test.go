package giga

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tendermint/tendermint/internal/autobahn/data"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

func TestDataClientServer(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		committee, keys := types.GenCommittee(rng, 3)
		cfg := &data.Config{
			Committee: committee,
		}
		serverState := data.NewState(cfg, utils.None[data.BlockStore]())
		s.SpawnBgNamed("serverState.Run()", func() error { return utils.IgnoreCancel(serverState.Run(ctx)) })
		clientState := data.NewState(cfg, utils.None[data.BlockStore]())
		s.SpawnBgNamed("clientState.Run()", func() error { return utils.IgnoreCancel(clientState.Run(ctx)) })
		addr := tcp.TestReserveAddr()

		// Connect client to the server.
		s.SpawnBgNamed("server", func() error {
			server := grpcutils.NewServer()
			serverState.Register(server)
			return grpcutils.RunServer(ctx, server, addr)
		})
		s.SpawnBgNamed("client", func() error {
			return clientState.RunClientPool(ctx, []*config.PeerConfig{
				{
					Address:    addr.String(),
					RetryDelay: utils.Some(utils.Duration(100 * time.Millisecond)),
				},
			})
		})

		t.Logf("push data")
		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := makeCommitQC(rng, committee, keys, prev)
			if err := serverState.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("serverState.PushQC(): %w", err)
			}
			prev = utils.Some(qc.QC())
		}
		t.Logf("wait for replication")
		for n := range serverState.NextBlock() {
			want, err := serverState.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("serverState.FinalBlock(): %w", err)
			}
			got, err := clientState.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("clientState.FinalBlock(): %w", err)
			}
			if err := utils.TestDiff(want, got); err != nil {
				return err
			}

			wantQC, err := serverState.QC(ctx, n)
			if err != nil {
				return fmt.Errorf("serverState.CommitQC(): %w", err)
			}
			gotQC, err := clientState.QC(ctx, n)
			if err != nil {
				return fmt.Errorf("clientState.CommitQC(): %w", err)
			}
			if err := utils.TestDiff(wantQC, gotQC); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
