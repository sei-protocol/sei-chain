package giga

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"

	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"

	dto "github.com/prometheus/client_model/go"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/giga/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/mux"
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

func newTestNode(t testing.TB, registry *epoch.TestRegistry, cfg *consensus.Config) *testNode {
	cfg.PersistentStateDir = utils.None[string]()
	store := blockstore.NewTestStore(t)
	dataState, err := data.NewState(&data.Config{App: abevm.TestEpochApp(registry)}, store)
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
	registry  *epoch.TestRegistry
	committee *types.Committee
	nodes     map[types.PublicKey]*testNode
}

func newTestEnv(registry *epoch.TestRegistry) *testEnv {
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
		MaxRate:         rate.Inf,
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

// serveFullCommitQCs serves one FullCommitQC stream on server. It reads the request, sends qcs exactly as given,
// and closes the stream. Unlike a real server, it ignores the requested index and may send QCs out of order.
func serveFullCommitQCs(ctx context.Context, server rpc.Server[API], qcs []*types.FullCommitQC) error {
	return utils.IgnoreAfterCancel(ctx, StreamFullCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.FullCommitQC, *pb.StreamFullCommitQCsReq]) error {
		if _, err := stream.Recv(ctx); err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		for _, qc := range qcs {
			if err := stream.Send(ctx, types.FullCommitQCConv.Encode(qc)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
		// Returning closes the stream, so the client ends with ErrRemoteClosed instead of waiting for more QCs.
		return nil
	}))
}

// testQCChain returns n consecutive FullCommitQCs of ep, starting at its first road index,
// and the lane blocks that each QC finalizes.
func testQCChain(rng utils.Rng, ep *types.Epoch, keys []types.SecretKey, n int) ([]*types.FullCommitQC, [][]*types.LaneBlock) {
	qcs := make([]*types.FullCommitQC, 0, n)
	blocks := make([][]*types.LaneBlock, 0, n)
	prev := utils.None[*types.CommitQC]()
	for range n {
		qc, bs := data.TestCommitQC(rng, ep, keys, prev)
		qcs = append(qcs, qc)
		blocks = append(blocks, bs)
		prev = utils.Some(qc.QC())
	}
	return qcs, blocks
}

func TestFullCommitQCStreamAcceptsSkippedIndexes(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 1)
	// One real node. The test uses only its data state and its QC stream client.
	node := newTestEnv(registry).AddNode(t, keys[0])
	// qcs[0] to qcs[4] are consecutive, validly signed QCs.
	qcs, _ := testQCChain(rng, registry.MustEpoch(0), keys, 5)
	// The node holds qcs[0], so the client requests qcs[1].
	require.NoError(t, node.data.PushPartialBlock(ctx, qcs[0], nil))
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		serverConn, clientConn := conn.NewTestConn()
		server := rpc.NewServer[API]()
		client := rpc.NewClient[API]()
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, server.Run(ctx, serverConn)) })
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, client.Run(ctx, clientConn)) })
		// The server sends qcs[1], the requested index, then qcs[2], the next index,
		// then qcs[4], which skips qcs[3].
		s.SpawnBg(func() error { return serveFullCommitQCs(ctx, server, []*types.FullCommitQC{qcs[1], qcs[2], qcs[4]}) })
		// Another peer fills the index that the stream skips. The push waits until qcs[2] is stored.
		s.Spawn(func() error { return node.data.PushPartialBlock(ctx, qcs[3], nil) })
		// The client under test.
		return node.service.clientStreamFullCommitQCs(ctx, client, keys[0].Public())
	})
	// The stream ended because the server closed it, not because the client rejected a QC.
	require.ErrorIs(t, err, mux.ErrRemoteClosed)
	// The node holds qcs[0] to qcs[4]. NextQC does not block, so a missing QC fails here instead of hanging below.
	require.Equal(t, qcs[4].Height()+1, node.data.NextQC())
	// Each stored QC is the one that was sent or pushed.
	for _, want := range qcs {
		got, err := node.data.QC(ctx, want.Height())
		require.NoError(t, err)
		require.NoError(t, utils.TestDiff(want, got))
	}
}

func TestFullCommitQCStreamReportsSkippedFullCommitQCsThatTheNodeNeeds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		rng := utils.TestRng()
		registry, keys := epoch.GenRegistry(rng, 1)
		node := newTestEnv(registry).AddNode(t, keys[0])
		qcs, _ := testQCChain(rng, registry.MustEpoch(0), keys, 3)
		// The node holds qcs[0], so the client requests qcs[1].
		require.NoError(t, node.data.PushPartialBlock(ctx, qcs[0], nil))
		skipped := fetchCount(resFullCommitQC, "skipped")
		require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			serverConn, clientConn := conn.NewTestConn()
			server := rpc.NewServer[API]()
			client := rpc.NewClient[API]()
			s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, server.Run(ctx, serverConn)) })
			s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, client.Run(ctx, clientConn)) })
			// The server sends qcs[2], as a peer that pruned qcs[1] does. No other peer sends qcs[1].
			s.SpawnBg(func() error { return serveFullCommitQCs(ctx, server, []*types.FullCommitQC{qcs[2]}) })
			// The client under test.
			s.SpawnBg(func() error {
				return utils.IgnoreAfterCancel(ctx, node.service.clientStreamFullCommitQCs(ctx, client, keys[0].Public()))
			})
			// Every task is blocked now, and the client is blocked on qcs[1].
			synctest.Wait()
			if got, want := fetchCount(resFullCommitQC, "skipped"), skipped+1; got != want {
				return fmt.Errorf("fetch{full_commit_qc,skipped} = %d, want %d", got, want)
			}
			// The node did not store qcs[2], because it still needs qcs[1].
			if got, want := node.data.NextQC(), qcs[1].Height(); got != want {
				return fmt.Errorf("NextQC() = %d, want %d", got, want)
			}
			return nil
		}))
	})
}

func TestFullCommitQCStreamDoesNotReportSkippedFullCommitQCsThatTheNodeHolds(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 1)
	node := newTestEnv(registry).AddNode(t, keys[0])
	qcs, _ := testQCChain(rng, registry.MustEpoch(0), keys, 3)
	// The node holds qcs[0], so the client requests qcs[1].
	require.NoError(t, node.data.PushPartialBlock(ctx, qcs[0], nil))
	skipped := fetchCount(resFullCommitQC, "skipped")
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		serverConn, clientConn := conn.NewTestConn()
		server := rpc.NewServer[API]()
		client := rpc.NewClient[API]()
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, server.Run(ctx, serverConn)) })
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, client.Run(ctx, clientConn)) })
		s.SpawnBg(func() error {
			return utils.IgnoreAfterCancel(ctx, StreamFullCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.FullCommitQC, *pb.StreamFullCommitQCsReq]) error {
				// The request shows that the client fixed qcs[1] as the next QC of this stream.
				if _, err := stream.Recv(ctx); err != nil {
					return fmt.Errorf("stream.Recv(): %w", err)
				}
				// Another peer sends qcs[1] before this stream sends a QC.
				if err := node.data.PushPartialBlock(ctx, qcs[1], nil); err != nil {
					return fmt.Errorf("PushPartialBlock(): %w", err)
				}
				// The stream skips qcs[1], which the node now holds. Returning closes the stream.
				return stream.Send(ctx, types.FullCommitQCConv.Encode(qcs[2]))
			}))
		})
		// The client under test.
		return node.service.clientStreamFullCommitQCs(ctx, client, keys[0].Public())
	})
	require.ErrorIs(t, err, mux.ErrRemoteClosed)
	require.Equal(t, qcs[2].Height()+1, node.data.NextQC())
	require.Equal(t, skipped, fetchCount(resFullCommitQC, "skipped"))
}

func TestFullCommitQCStreamRejectsOutOfOrderIndexes(t *testing.T) {
	for _, tc := range []struct {
		name string
		// pushed are the chain positions the client holds before it opens the stream.
		pushed []int
		// sent are the chain positions the server sends, in order.
		sent []int
		// wantStored is the number of chain positions, counted from qcs[0], that the node holds at the end.
		wantStored int
	}{
		// The client holds qcs[0] and qcs[1], so it asks for qcs[2]. qcs[1] is below that.
		{name: "first below requested", pushed: []int{0, 1}, sent: []int{1}, wantStored: 2},
		// qcs[0] is accepted. The second qcs[0] is not above it.
		{name: "repeated", sent: []int{0, 0}, wantStored: 1},
		// qcs[0] and qcs[1] are accepted. Then qcs[0] is below qcs[1].
		{name: "decreasing", sent: []int{0, 1, 0}, wantStored: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			rng := utils.TestRng()
			registry, keys := epoch.GenRegistry(rng, 1)
			node := newTestEnv(registry).AddNode(t, keys[0])
			qcs, _ := testQCChain(rng, registry.MustEpoch(0), keys, 3)
			// These QCs move the node's NextQC(), so they set the index that the client requests.
			for _, i := range tc.pushed {
				require.NoError(t, node.data.PushPartialBlock(ctx, qcs[i], nil))
			}
			var sent []*types.FullCommitQC
			for _, i := range tc.sent {
				sent = append(sent, qcs[i])
			}
			err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				serverConn, clientConn := conn.NewTestConn()
				server := rpc.NewServer[API]()
				client := rpc.NewClient[API]()
				s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, server.Run(ctx, serverConn)) })
				s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, client.Run(ctx, clientConn)) })
				s.SpawnBg(func() error { return serveFullCommitQCs(ctx, server, sent) })
				// The client under test.
				return node.service.clientStreamFullCommitQCs(ctx, client, keys[0].Public())
			})
			// The client rejects one QC and ends the stream.
			require.ErrorIs(t, err, types.ErrUnexpectedHeight)
			// The node stored the pushed QCs and the QCs accepted before the reject, and nothing after them.
			require.Equal(t, qcs[0].Height()+types.RoadHeight(tc.wantStored), node.data.NextQC())
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
