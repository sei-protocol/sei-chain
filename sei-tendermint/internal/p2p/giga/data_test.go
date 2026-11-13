package giga

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"

	dto "github.com/prometheus/client_model/go"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmprometheus "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type testNode struct {
	data      *data.State
	consensus *consensus.State
	service   *Service
}

func defaultViewTimeout(view types.View) time.Duration { return time.Hour }

func newTestNode(t testing.TB, registry *epoch.Registry, cfg *consensus.Config) *testNode {
	cfg.PersistentStateDir = utils.None[string]()
	store := blockstore.NewTestStore(t)
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
func (e *testEnv) AddNode(t testing.TB, key types.SecretKey) *testNode {
	n := newTestNode(t, e.registry, &consensus.Config{
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
	server := env.AddNode(t, keys[0])
	client := env.AddNode(t, keys[1])
	ep0 := registry.MustEpoch(0)
	firstBlock := ep0.RoadRange().First
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
			qc, blocks := data.TestCommitQC(rng, ep0, keys, prev)
			if err := server.data.PushPartialBlock(ctx, qc, data.TestBlockMap(blocks)); err != nil {
				return fmt.Errorf("serverState.PushPartialBlock(): %w", err)
			}
			prev = utils.Some(qc.QC())
		}
		t.Logf("wait for replication")
		for n := firstBlock; n < server.data.NextQC(); n++ {
			want, err := server.data.SuperBlock(ctx, n)
			if err != nil {
				return fmt.Errorf("serverState.FinalBlock(): %w", err)
			}
			got, err := client.data.SuperBlock(ctx, n)
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

// runFullCommitQCClient runs the FullCommitQC stream client of node against a server that answers with qcs in order.
// It returns the error that ends the client.
func runFullCommitQCClient(ctx context.Context, node *testNode, qcs []*types.FullCommitQC) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		serverConn, clientConn := conn.NewTestConn()
		server := rpc.NewServer[API]()
		client := rpc.NewClient[API]()
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, server.Run(ctx, serverConn)) })
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, client.Run(ctx, clientConn)) })
		s.SpawnBg(func() error {
			return utils.IgnoreAfterCancel(ctx, StreamFullCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.FullCommitQC, *pb.StreamFullCommitQCsReq]) error {
				if _, err := stream.Recv(ctx); err != nil {
					return fmt.Errorf("stream.Recv(): %w", err)
				}
				for _, qc := range qcs {
					if err := stream.Send(ctx, types.FullCommitQCConv.Encode(qc)); err != nil {
						return fmt.Errorf("stream.Send(): %w", err)
					}
				}
				<-ctx.Done()
				return nil
			}))
		})
		return node.service.clientStreamFullCommitQCs(ctx, client)
	})
}

// testQCChain returns n consecutive FullCommitQCs of ep, starting at its first road index.
func testQCChain(rng utils.Rng, ep *types.Epoch, keys []types.SecretKey, n int) []*types.FullCommitQC {
	qcs := make([]*types.FullCommitQC, 0, n)
	prev := utils.None[*types.CommitQC]()
	for range n {
		qc, _ := data.TestCommitQC(rng, ep, keys, prev)
		qcs = append(qcs, qc)
		prev = utils.Some(qc.QC())
	}
	return qcs
}

func TestFullCommitQCStreamAcceptsIncreasingIndexesWithGaps(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	ep0 := registry.MustEpoch(0)
	node := newTestEnv(registry).AddNode(t, keys[0])
	qcs := testQCChain(rng, ep0, keys, 5)
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			return utils.IgnoreAfterCancel(ctx, runFullCommitQCClient(ctx, node, []*types.FullCommitQC{qcs[0], qcs[2], qcs[4]}))
		})
		// Another peer fills the gaps that the stream skips.
		for _, qc := range []*types.FullCommitQC{qcs[1], qcs[3]} {
			if err := node.data.PushPartialBlock(ctx, qc, nil); err != nil {
				return fmt.Errorf("PushPartialBlock(): %w", err)
			}
		}
		for _, want := range qcs {
			got, err := node.data.QC(ctx, want.Index())
			if err != nil {
				return fmt.Errorf("QC(%v): %w", want.Index(), err)
			}
			if err := utils.TestDiff(want, got); err != nil {
				return fmt.Errorf("QC mismatch at %v: %w", want.Index(), err)
			}
		}
		return nil
	})
	require.NoError(t, err)
}

func TestFullCommitQCStreamRejectsOutOfOrderIndexes(t *testing.T) {
	for _, tc := range []struct {
		name string
		// pushed are the chain positions the client holds before it opens the stream.
		pushed []int
		// sent are the chain positions the server sends, in order.
		sent []int
		// accepted is the number of sent QCs the client processes before it rejects one.
		accepted int64
	}{
		{name: "first below requested", pushed: []int{0, 1}, sent: []int{1}, accepted: 0},
		{name: "repeated", sent: []int{0, 0}, accepted: 1},
		{name: "decreasing", sent: []int{0, 1, 0}, accepted: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			rng := utils.TestRng()
			registry, keys := epoch.GenRegistry(rng, 2)
			node := newTestEnv(registry).AddNode(t, keys[0])
			qcs := testQCChain(rng, registry.MustEpoch(0), keys, 3)
			for _, i := range tc.pushed {
				require.NoError(t, node.data.PushPartialBlock(ctx, qcs[i], nil))
			}
			var sent []*types.FullCommitQC
			for _, i := range tc.sent {
				sent = append(sent, qcs[i])
			}
			ok := fetchCount(resFullCommitQC, "ok")
			outOfOrder := fetchCount(resFullCommitQC, "out_of_order")
			require.ErrorIs(t, runFullCommitQCClient(ctx, node, sent), types.ErrUnexpectedIndex)
			require.Equal(t, ok+tc.accepted, fetchCount(resFullCommitQC, "ok"))
			require.Equal(t, outOfOrder+1, fetchCount(resFullCommitQC, "out_of_order"))
		})
	}
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
