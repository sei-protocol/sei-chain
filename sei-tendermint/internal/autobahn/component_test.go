package stream

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sei-protocol/sei-stream/config"
	"github.com/sei-protocol/sei-stream/data"
	"github.com/sei-protocol/sei-stream/pkg/grpcutils"
	"github.com/sei-protocol/sei-stream/pkg/service"
	"github.com/sei-protocol/sei-stream/pkg/tcp"
	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/pkg/protocol"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

func retry[T any](ctx context.Context, f func() (T, error)) (T, error) {
	for {
		if res, err := f(); status.Code(err) != codes.Unavailable {
			return res, err
		}
		if err := utils.Sleep(ctx, 100*time.Millisecond); err != nil {
			return utils.Zero[T](), err
		}
	}
}

func connect(address string) (protocol.ProducerAPIClient, func(), error) {
	conn, err := grpcutils.NewClient(address)
	if err != nil {
		return nil, nil, err
	}
	return protocol.NewProducerAPIClient(conn), func() { _ = conn.Close() }, nil
}

func TestRun(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 7)
	active := committee.CommitQuorum()
	viewTimeout := func(view types.View) time.Duration {
		leader := committee.Leader(view)
		for _, k := range keys[:active] {
			if k.Public() == leader {
				return time.Hour
			}
		}
		return 0
	}

	var replicaCfgs []*config.PeerConfig
	var addrs []netip.AddrPort
	for i, k := range keys {
		addrs = append(addrs, tcp.TestReserveAddr())
		replicaCfgs = append(replicaCfgs, &config.PeerConfig{
			Key:        utils.Some(k.Public()),
			Address:    addrs[i].String(),
			RetryDelay: utils.Some(utils.Duration(time.Second)),
		})
	}

	const L = 10
	type TxPayload = [L]byte
	type TxSet = map[TxPayload]struct{}
	wantTxs := TxSet{}
	var txs []*protocol.Transaction

	txCount := 10
	for range txCount {
		tx := &protocol.Transaction{
			Hash:    utils.GenString(rng, 10),
			Payload: utils.GenBytes(rng, L),
			GasUsed: uint64(rng.Intn(1000)),
		}
		txs = append(txs, tx)
		wantTxs[TxPayload(tx.Payload)] = struct{}{}
	}

	if err := service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		for i, r := range replicaCfgs[:active] {
			streamCfg := &config.StreamConfig{
				ServerAddr:               addrs[i],
				Peers:                    replicaCfgs,
				MockExecutorTxsPerSecond: utils.Some(uint64(100000)),
			}
			committee, err := streamCfg.Committee()
			if err != nil {
				return fmt.Errorf("streamCfg.Committee(): %w", err)
			}

			// Spawn replica.
			s.SpawnBgNamed(fmt.Sprintf("replica[%d]", i), func() error {
				reg := prometheus.NewRegistry()
				cfg := &config.Config{
					StreamConfig: streamCfg,
				}
				return utils.IgnoreCancel(Run(ctx, cfg, keys[i], viewTimeout, reg))
			})

			// Send transactions.
			s.SpawnNamed(fmt.Sprintf("send[%d]", i), func() error {
				client, closeConn, err := connect(r.Address)
				if err != nil {
					return fmt.Errorf("connect(): %w", err)
				}
				defer closeConn()
				stream, err := retry(ctx, func() (protocol.ProducerAPI_MempoolClient, error) { return client.Mempool(ctx) })
				if err != nil {
					return fmt.Errorf("client.Mempool(): %w", err)
				}
				t.Logf("send[%d] sending transactions", i)
				total := 0
				for it, tx := range txs {
					if it%active == i {
						if err := stream.Send(tx); err != nil {
							return fmt.Errorf("stream.Send(): %w", err)
						}
						total += 1
					}
				}
				if _, err := stream.CloseAndRecv(); err != nil {
					return fmt.Errorf("mempool.CloseAndRecv(): %w", err)
				}
				t.Logf("send[%d] done, %d txs", i, total)
				return nil
			})

			dataState := data.NewState(&data.Config{
				Committee: committee,
			}, utils.None[data.BlockStore]())
			s.SpawnBgNamed(fmt.Sprintf("follower[%d].Client", i), func() error {
				return utils.IgnoreCancel(dataState.RunClientPool(ctx, utils.Slice(r)))
			})
			s.SpawnBgNamed(fmt.Sprintf("follower[%d].Data", i), func() error {
				return utils.IgnoreCancel(dataState.Run(ctx))
			})
			s.SpawnNamed(fmt.Sprintf("follower[%d].Recv", i), func() error {
				gotTxs := TxSet{}
				for n := types.GlobalBlockNumber(0); len(gotTxs) < len(wantTxs); n += 1 {
					block, err := dataState.GlobalBlock(ctx, n)
					if err != nil {
						return fmt.Errorf("dataState.Blocks(): %w", err)
					}
					newTxs := 0
					for _, p := range block.Payload.Txs() {
						gotTxs[TxPayload(p)] = struct{}{}
						newTxs += 1
					}
					t.Logf(
						"follower[%v] Received block: %d txs, %d total",
						r.Name, newTxs, len(gotTxs),
					)
				}
				t.Logf("follower[%v] done", r.Name)
				return utils.TestDiff(wantTxs, gotTxs)
			})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
