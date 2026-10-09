package giga

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmprometheus "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type testNode struct {
	data      *data.State
	consensus *consensus.State
	service   *Service
}

func defaultViewTimeout(view types.View) time.Duration { return time.Hour }

func newTestNode(registry *epoch.Registry, cfg *consensus.Config) *testNode {
	cfg.PersistentStateDir = utils.None[string]()
	store, err := blockstore.New(memblock.NewBlockDB())
	if err != nil {
		panic(fmt.Sprintf("blockstore.New: %v", err))
	}
	dataState, err := data.NewState(&data.Config{Registry: registry}, store)
	if err != nil {
		panic(fmt.Sprintf("data.NewState: %v", err))
	}
	consensusState, err := consensus.NewState(cfg, dataState)
	if err != nil {
		panic(fmt.Sprintf("consensus.NewState(): %v", err))
	}
	return &testNode{
		data:      dataState,
		consensus: consensusState,
		service:   NewService(consensusState),
	}
}

func (n *testNode) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return n.data.Run(ctx) })
		s.Spawn(func() error { return n.consensus.Run(ctx) })
		s.Spawn(func() error { return n.service.Run(ctx) })
		return nil
	})
}

type testEnv struct {
	registry  *epoch.Registry
	committee *types.Committee
	nodes     map[types.PublicKey]*testNode
}

func newTestEnv(registry *epoch.Registry) *testEnv {
	return &testEnv{registry, registry.MustEpoch(0).Committee(), map[types.PublicKey]*testNode{}}
}

// Call AddNode BEFORE Run.
func (e *testEnv) AddNode(key types.SecretKey) *testNode {
	n := newTestNode(e.registry, &consensus.Config{
		Key: key,
		ViewTimeout: func(view types.View) time.Duration {
			if _, ok := e.nodes[e.committee.Leader(view)]; ok {
				return time.Hour
			}
			return 0
		},
		ProposalTimeout: time.Hour,
	})
	e.nodes[key.Public()] = n
	return n
}

func (e *testEnv) Run(ctx context.Context) error {
	return utils.IgnoreAfterCancel(ctx, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for xKey, x := range e.nodes {
			s.SpawnNamed("node", func() error { return x.Run(ctx) })
			for _, y := range e.nodes {
				xConn, yConn := conn.NewTestConn()
				server := rpc.NewServer[API]()
				client := rpc.NewClient[API]()
				s.SpawnNamed("mux server", func() error { return server.Run(ctx, xConn) })
				s.SpawnNamed("mux client", func() error { return client.Run(ctx, yConn) })
				s.SpawnNamed("RunServer", func() error { return x.service.RunServer(ctx, server, true) })
				s.SpawnNamed("RunClient", func() error { return y.service.RunClient(ctx, client, xKey, true) })
			}
		}
		return nil
	}))
}

func TestDataClientServer(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	env := newTestEnv(registry)
	server := env.AddNode(keys[0])
	client := env.AddNode(keys[1])
	firstBlock := server.data.Registry().FirstBlock()
	blockOK := fetchCount(resBlock, "ok")
	qcOK := fetchCount(resFullCommitQC, "ok")
	notFound := serveCount(resBlock, "not_found")
	unavailable := fetchCount(resBlock, "unavailable")
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })

		t.Logf("push data")
		prev := utils.None[*types.CommitQC]()
		for i := range 3 {
			t.Logf("iteration %v", i)
			qc, blocks := data.TestCommitQC(rng, server.data.Registry().MustEpoch(0), keys, prev)
			if err := server.data.PushQC(ctx, qc, blocks); err != nil {
				return fmt.Errorf("serverState.PushQC(): %w", err)
			}
			prev = utils.Some(qc.QC())
		}
		t.Logf("wait for replication")
		for n := firstBlock; n < server.data.NextBlock(); n++ {
			want, err := server.data.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("serverState.FinalBlock(): %w", err)
			}
			got, err := client.data.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("clientState.FinalBlock(): %w", err)
			}
			if err := utils.TestDiff(want, got); err != nil {
				return err
			}

			wantQC, err := server.data.QC(ctx, n)
			if err != nil {
				return fmt.Errorf("serverState.CommitQC(): %w", err)
			}
			gotQC, err := client.data.QC(ctx, n)
			if err != nil {
				return fmt.Errorf("clientState.CommitQC(): %w", err)
			}
			if err := utils.TestDiff(wantQC, gotQC); err != nil {
				return fmt.Errorf("QC mismatch at block %d: %w", n, err)
			}
		}
		if fetchCount(resBlock, "ok") <= blockOK {
			return fmt.Errorf("fetch{block,ok} did not increase")
		}
		if fetchCount(resFullCommitQC, "ok") <= qcOK {
			return fmt.Errorf("fetch{full_commit_qc,ok} did not increase")
		}
		if fetchCount(resBlock, "unavailable") <= unavailable {
			return fmt.Errorf("fetch{block,unavailable} did not increase")
		}
		if serveCount(resBlock, "not_found") <= notFound {
			return fmt.Errorf("serve{block,not_found} did not increase")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestClientGetBlockSpreadsAcrossPeers runs one fullnode service with a
// GetBlock client on each of several peers, the way a fullnode block-syncs
// from every committee member. Every peer must serve some heights: the shared
// fetch queue spreads them, so throughput is not capped by one connection's
// GetBlock rate limit.
func TestClientGetBlockSpreadsAcrossPeers(t *testing.T) {
	const peers = 3
	const qcs = 6
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	newDataState := func() *data.State {
		store := utils.OrPanic1(blockstore.New(memblock.NewBlockDB()))
		return utils.OrPanic1(data.NewState(&data.Config{Registry: registry}, store))
	}
	client := NewFullNodeService(newDataState())
	servers := make([]*Service, peers)
	served := make([]atomic.Int64, peers)
	for i := range servers {
		servers[i] = NewFullNodeService(newDataState())
	}
	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(client.data.Run(ctx)) })
		s.SpawnBg(func() error { return utils.IgnoreCancel(client.Run(ctx)) })
		for i, server := range servers {
			s.SpawnBg(func() error { return utils.IgnoreCancel(server.data.Run(ctx)) })
			xConn, yConn := conn.NewTestConn()
			rpcServer := rpc.NewServer[API]()
			rpcClient := rpc.NewClient[API]()
			s.SpawnBg(func() error { return utils.IgnoreCancel(rpcServer.Run(ctx, xConn)) })
			s.SpawnBg(func() error { return utils.IgnoreCancel(rpcClient.Run(ctx, yConn)) })
			s.SpawnBg(func() error { return utils.IgnoreCancel(server.serverPing(ctx, rpcServer)) })
			s.SpawnBg(func() error { return utils.IgnoreCancel(server.serverStreamFullCommitQCs(ctx, rpcServer)) })
			s.SpawnBg(func() error { return utils.IgnoreCancel(server.serverStreamAppQCs(ctx, rpcServer)) })
			s.SpawnBg(func() error { return utils.IgnoreCancel(serveCountedGetBlock(ctx, server, rpcServer, &served[i])) })
			s.SpawnBg(func() error {
				return utils.IgnoreCancel(client.RunClient(ctx, rpcClient, keys[i].Public(), true))
			})
		}

		prev := utils.None[*types.CommitQC]()
		for range qcs {
			qc, blocks := data.TestCommitQC(rng, registry.MustEpoch(0), keys, prev)
			for _, server := range servers {
				if err := server.data.PushQC(ctx, qc, blocks); err != nil {
					return fmt.Errorf("server.data.PushQC(): %w", err)
				}
			}
			prev = utils.Some(qc.QC())
		}
		start := time.Now()
		first := registry.FirstBlock()
		next := servers[0].data.NextBlock()
		for n := first; n < next; n++ {
			want, err := servers[0].data.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("server.data.GlobalBlock(%v): %w", n, err)
			}
			got, err := client.data.GlobalBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("client.data.GlobalBlock(%v): %w", n, err)
			}
			if err := utils.TestDiff(want, got); err != nil {
				return fmt.Errorf("block %v: %w", n, err)
			}
		}
		elapsed := time.Since(start)
		oneConnMax := float64(GetBlock.Limit.Concurrent) + float64(GetBlock.Limit.Rate)*elapsed.Seconds()
		t.Logf("synced %v blocks in %v; one connection serves at most %.0f in that time", next-first, elapsed, oneConnMax)
		for i := range served {
			t.Logf("peer %v served %v blocks", i, served[i].Load())
			if served[i].Load() == 0 {
				return fmt.Errorf("peer %v served no blocks", i)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// serveCountedGetBlock serves GetBlock from x's data state and counts every
// block it returns. A height the peer does not hold yet gets an empty reply.
func serveCountedGetBlock(ctx context.Context, x *Service, server rpc.Server[API], served *atomic.Int64) error {
	return GetBlock.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*pb.GetBlockResp, *pb.GetBlockReq]) error {
		reqRaw, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		req, err := GetBlockReqConv.Decode(reqRaw)
		if err != nil {
			return fmt.Errorf("GetBlockReqConv.Decode(): %w", err)
		}
		block, err := x.data.TryBlock(req.GlobalNumber)
		if errors.Is(err, types.ErrNotFound) {
			return stream.Send(ctx, GetBlockRespConv.Encode(utils.None[*types.Block]()))
		}
		if err != nil {
			return fmt.Errorf("TryBlock(%d): %w", req.GlobalNumber, err)
		}
		served.Add(1)
		return stream.Send(ctx, GetBlockRespConv.Encode(utils.Some(block)))
	})
}

func fetchCount(resource, reason string) int64 {
	return readCounter(Global.fetchAt(resource, reason))
}

func serveCount(resource, reason string) int64 {
	return readCounter(Global.serveAt(resource, reason))
}

func readCounter(c *tmprometheus.CounterInt) int64 {
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		panic(err)
	}
	return int64(m.GetCounter().GetValue())
}
