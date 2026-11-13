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
		qcs, blocks := testQCChain(rng, ep0, keys, 3)
		for i, qc := range qcs {
			t.Logf("iteration %v", i)
			if err := server.data.PushPartialBlock(ctx, qc, data.TestBlockMap(blocks[i])); err != nil {
				return fmt.Errorf("serverState.PushPartialBlock(): %w", err)
			}
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

// runFullCommitQCClient runs the FullCommitQC stream client of node against a server that passes the request to onReq,
// when set, then sends qcs in order and closes the stream. It returns the error that ends the client.
func runFullCommitQCClient(ctx context.Context, node *testNode, onReq utils.Option[func(context.Context, *StreamFullCommitQCsReq) error], qcs []*types.FullCommitQC) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// An in-memory link, with the fake server on one end and the node's client on the other.
		serverConn, clientConn := conn.NewTestConn()
		server := rpc.NewServer[API]()
		client := rpc.NewClient[API]()
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, server.Run(ctx, serverConn)) })
		s.SpawnBg(func() error { return utils.IgnoreAfterCancel(ctx, client.Run(ctx, clientConn)) })
		s.SpawnBg(func() error {
			return utils.IgnoreAfterCancel(ctx, StreamFullCommitQCs.Serve(ctx, server, func(ctx context.Context, stream rpc.Stream[*apb.FullCommitQC, *pb.StreamFullCommitQCsReq]) error {
				rawReq, err := stream.Recv(ctx)
				if err != nil {
					return fmt.Errorf("stream.Recv(): %w", err)
				}
				req, err := StreamFullCommitQCsReqConv.Decode(rawReq)
				if err != nil {
					return fmt.Errorf("StreamFullCommitQCsReqConv.Decode(): %w", err)
				}
				// The hook runs before any QC is sent, so it can check the request and change the node's state.
				if onReq, ok := onReq.Get(); ok {
					if err := onReq(ctx, req); err != nil {
						return err
					}
				}
				// Sent exactly as given. Unlike a real server, this one ignores req.NextBlock and may send QCs out of order.
				for _, qc := range qcs {
					if err := stream.Send(ctx, types.FullCommitQCConv.Encode(qc)); err != nil {
						return fmt.Errorf("stream.Send(): %w", err)
					}
				}
				// Returning closes the stream, so the client ends with ErrRemoteClosed instead of waiting for more QCs.
				return nil
			}))
		})
		// The real client code under test. Its error is the result of this helper.
		return node.service.clientStreamFullCommitQCs(ctx, client)
	})
}

// testQCChain returns n consecutive FullCommitQCs of ep, starting at its first road index,
// and the lane blocks that each QC finalizes.
func testQCChain(rng utils.Rng, ep *types.Epoch, keys []types.SecretKey, n int) ([]*types.FullCommitQC, [][]*types.Block) {
	qcs := make([]*types.FullCommitQC, 0, n)
	blocks := make([][]*types.Block, 0, n)
	prev := utils.None[*types.CommitQC]()
	for range n {
		qc, bs := data.TestCommitQC(rng, ep, keys, prev)
		qcs = append(qcs, qc)
		blocks = append(blocks, bs)
		prev = utils.Some(qc.QC())
	}
	return qcs, blocks
}

func TestFullCommitQCStreamAcceptsIncreasingIndexesWithGaps(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 1)
	// One real node with an empty data state. The test uses only its data state and its QC stream client.
	node := newTestEnv(registry).AddNode(t, keys[0])
	// qcs[0] to qcs[3] are consecutive, validly signed QCs.
	qcs, _ := testQCChain(rng, registry.MustEpoch(0), keys, 4)
	// Another peer pushes qcs[0] after the client requests it, so the stream starts above the requested index.
	pushRequested := func(ctx context.Context, req *StreamFullCommitQCsReq) error {
		// The node is empty, so the client must ask for qcs[0].
		if got, want := req.NextBlock, qcs[0].Index(); got != want {
			return fmt.Errorf("req.NextBlock = %v, want %v", got, want)
		}
		return node.data.PushPartialBlock(ctx, qcs[0], nil)
	}
	ok := fetchCount(resFullCommitQC, "ok")
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Another peer fills the gap that the stream skips. The push waits until qcs[1] is stored.
		s.Spawn(func() error { return node.data.PushPartialBlock(ctx, qcs[2], nil) })
		// The server sends qcs[1], above the requested qcs[0], then qcs[3], which skips qcs[2].
		return runFullCommitQCClient(ctx, node, utils.Some(pushRequested), []*types.FullCommitQC{qcs[1], qcs[3]})
	})
	// The stream ended because the server closed it, not because the client rejected a QC.
	require.ErrorIs(t, err, mux.ErrRemoteClosed)
	// The client stored both QCs it received on the stream.
	require.Equal(t, ok+2, fetchCount(resFullCommitQC, "ok"))
	// The node holds qcs[0] to qcs[3]. NextQC does not block, so a missing QC fails here instead of hanging below.
	require.Equal(t, qcs[3].Index()+1, node.data.NextQC())
	// Each stored QC is the one that was sent.
	for _, want := range qcs {
		got, err := node.data.QC(ctx, want.Index())
		require.NoError(t, err)
		require.NoError(t, utils.TestDiff(want, got))
	}
}

func TestFullCommitQCStreamRejectsOutOfOrderIndexes(t *testing.T) {
	for _, tc := range []struct {
		name string
		// pushed are the chain positions the client holds before it opens the stream.
		pushed []int
		// sent are the chain positions the server sends, in order.
		sent []int
		// accepted is the number of sent QCs the client stores before it rejects one.
		accepted int
	}{
		// The client holds qcs[0] and qcs[1], so it asks for qcs[2]. qcs[1] is below that.
		{name: "first below requested", pushed: []int{0, 1}, sent: []int{1}, accepted: 0},
		// qcs[0] is accepted. The second qcs[0] is not above it.
		{name: "repeated", sent: []int{0, 0}, accepted: 1},
		// qcs[0] and qcs[1] are accepted. Then qcs[0] is below qcs[1].
		{name: "decreasing", sent: []int{0, 1, 0}, accepted: 2},
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
			// The counter is global to the test process, so the check below compares the change, not the value.
			outOfOrder := fetchCount(resFullCommitQC, "out_of_order")
			// The client rejects one QC and ends the stream.
			require.ErrorIs(t, runFullCommitQCClient(ctx, node, utils.None[func(context.Context, *StreamFullCommitQCsReq) error](), sent), types.ErrUnexpectedIndex)
			// The node stored the pushed QCs and the QCs accepted before the reject, and nothing after them.
			require.Equal(t, qcs[0].Index()+types.RoadIndex(len(tc.pushed)+tc.accepted), node.data.NextQC())
			// The client counted exactly one reject.
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
