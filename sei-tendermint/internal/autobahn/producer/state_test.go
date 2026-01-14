package producer

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/sei-protocol/sei-stream/config"
	"github.com/sei-protocol/sei-stream/data"
	"github.com/sei-protocol/sei-stream/pkg/grpcutils"
	"github.com/sei-protocol/sei-stream/pkg/service"
	"github.com/sei-protocol/sei-stream/pkg/tcp"
	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/consensus"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

func Map[T, U any](s []T, f func(T) U) []U {
	r := make([]U, len(s))
	for i, v := range s {
		r[i] = f(v)
	}
	return r
}

func TestState(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 1)
	cfg := &Config{
		MaxTxsPerBlock: 5,
		// Rest of the parameters are set to not interfere with the test.
		MempoolSize:      1000,
		MaxGasPerBlock:   1000000,
		BlockInterval:    time.Hour,
		AllowEmptyBlocks: false,
	}
	wantBlocks := 4
	var wantTxs []*protocol.Transaction
	var gotBlockTxs [][][]byte
	if err := service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		ds := data.NewState(&data.Config{
			Committee: committee,
		}, utils.None[data.BlockStore]())
		s.SpawnBgNamed("data", func() error { return ds.Run(ctx) })
		cs := consensus.NewState(&consensus.Config{
			Key: keys[0],
			ViewTimeout: func(view types.View) time.Duration {
				return time.Hour
			},
		}, ds)
		s.SpawnBgNamed("consensus", func() error { return cs.Run(ctx) })
		addr := tcp.TestReserveAddr()
		s.SpawnBgNamed("server", func() error {
			server := grpcutils.NewServer()
			cs.Register(server)
			return grpcutils.RunServer(ctx, server, addr)
		})
		s.SpawnBgNamed("client", func() error {
			return cs.RunClientPool(ctx, []*config.PeerConfig{
				{
					Key:        utils.Some(keys[0].Public()),
					Address:    addr.String(),
					RetryDelay: utils.Some(utils.Duration(100 * time.Millisecond)),
				},
			})
		})

		state := NewState(cfg, cs)
		s.SpawnBgNamed("producer", func() error { return state.Run(ctx) })
		s.SpawnNamed("edge", func() error {
			for range wantBlocks * int(cfg.MaxTxsPerBlock) {
				tx := &protocol.Transaction{
					Hash:    utils.GenString(rng, 10),
					Payload: utils.GenBytes(rng, 10),
					GasUsed: uint64(rng.Intn(1000)),
				}
				wantTxs = append(wantTxs, tx)
				if err := state.PushToMempool(ctx, tx); err != nil {
					return err
				}
			}
			t.Logf("pushed %d transactions", len(wantTxs))
			return nil
		})
		for i := range types.GlobalBlockNumber(wantBlocks) {
			b, err := ds.GlobalBlock(ctx, i)
			if err != nil {
				return fmt.Errorf("block(%v): %w", i, err)
			}
			gotBlockTxs = append(gotBlockTxs, b.Payload.Txs())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Transactions should be included in blocks in order.
	wantBlockTxs := slices.Collect(slices.Chunk(
		Map(wantTxs, func(tx *protocol.Transaction) []byte { return tx.Payload }),
		int(cfg.MaxTxsPerBlock),
	))
	if err := utils.TestDiff(wantBlockTxs, gotBlockTxs); err != nil {
		t.Fatal(err)
	}
}
