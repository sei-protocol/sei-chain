package reactor

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type testTx struct {
	tx types.Tx
}

type reactorTestSuite struct {
	network *p2p.TestNetwork

	reactors map[types.NodeID]*Reactor
	mempools map[types.NodeID]*mempool.TxMempool
	kvstores map[types.NodeID]*kvstore.Application

	nodes []types.NodeID
}

func setupMempool(t testing.TB, app *proxy.Proxy, cacheSize int, txConstraintsFetcher mempool.TxConstraintsFetcher) *mempool.TxMempool {
	t.Helper()

	cfg, err := config.ResetTestRoot(t.TempDir(), strings.ReplaceAll(t.Name(), "/", "|"))
	require.NoError(t, err)
	cfg.Mempool.CacheSize = cacheSize
	cfg.Mempool.DropUtilisationThreshold = 0.0

	t.Cleanup(func() { os.RemoveAll(cfg.RootDir) })

	return mempool.NewTxMempool(cfg.Mempool.ToMempoolConfig(), app, txConstraintsFetcher)
}

func checkTxs(ctx context.Context, t *testing.T, rng utils.Rng, txmp *mempool.TxMempool, numTxs int) []testTx {
	t.Helper()

	txs := make([]testTx, numTxs)

	for i := range numTxs {
		prefix := utils.GenBytes(rng, 20)
		txs[i] = testTx{
			tx: []byte(fmt.Sprintf("sender-%d=%X=%d", i, prefix, i+1000)),
		}
		_, err := txmp.CheckTx(ctx, txs[i].tx)
		require.NoError(t, err)
	}

	return txs
}

func convertTex(in []testTx) types.Txs {
	out := make([]types.Tx, len(in))
	for i := range in {
		out[i] = in[i].tx
	}
	return out
}

func setupReactors(ctx context.Context, t *testing.T, numNodes int) *reactorTestSuite {
	return setupReactorsWithConfig(ctx, t, numNodes, config.TestMempoolConfig(), mempool.NopTxConstraintsFetcher)
}

func setupReactorsWithConfig(
	ctx context.Context,
	t *testing.T,
	numNodes int,
	cfg *config.MempoolConfig,
	txConstraintsFetcher mempool.TxConstraintsFetcher,
) *reactorTestSuite {
	t.Helper()

	rts := &reactorTestSuite{
		network:  p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: numNodes}),
		reactors: make(map[types.NodeID]*Reactor, numNodes),
		mempools: make(map[types.NodeID]*mempool.TxMempool, numNodes),
		kvstores: make(map[types.NodeID]*kvstore.Application, numNodes),
	}

	for _, node := range rts.network.Nodes() {
		nodeID := node.NodeID
		rts.kvstores[nodeID] = kvstore.NewApplication()

		app := rts.kvstores[nodeID]
		proxyApp := proxy.New(app)
		txmp := setupMempool(t, proxyApp, 0, txConstraintsFetcher)
		rts.mempools[nodeID] = txmp

		reactor, err := NewReactor(cfg, txmp, node.Router)
		if err != nil {
			t.Fatalf("NewReactor(): %v", err)
		}
		rts.reactors[nodeID] = reactor
		rts.reactors[nodeID].MarkReadyToStart()
		rts.nodes = append(rts.nodes, nodeID)

		require.NoError(t, rts.reactors[nodeID].Start(ctx))
		require.True(t, rts.reactors[nodeID].IsRunning())
	}

	require.Len(t, rts.reactors, numNodes)

	t.Cleanup(func() {
		for _, reactor := range rts.reactors {
			reactor.Stop()
		}
	})
	return rts
}

func setupReactorForTest(t *testing.T, txConstraintsFetcher mempool.TxConstraintsFetcher) (*Reactor, *mempool.TxMempool) {
	t.Helper()

	cfg := config.TestConfig()
	cfg.SetRoot(t.TempDir())
	cfg.Mempool.DropUtilisationThreshold = 0.0
	cfg.Mempool.Broadcast = false

	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 1})
	node := network.Nodes()[0]

	txmp := mempool.NewTxMempool(cfg.Mempool.ToMempoolConfig(), kvstore.NewProxy(), txConstraintsFetcher)
	reactor, err := NewReactor(cfg.Mempool, txmp, node.Router)
	require.NoError(t, err)
	reactor.MarkReadyToStart()
	require.NoError(t, reactor.Start(t.Context()))
	require.True(t, reactor.IsRunning())
	t.Cleanup(reactor.Stop)

	return reactor, txmp
}

func (rts *reactorTestSuite) start(t *testing.T) {
	t.Helper()
	rts.network.Start(t)
}

func (rts *reactorTestSuite) waitForTxns(t *testing.T, txs []types.Tx, ids ...types.NodeID) {
	t.Helper()

	wg := &sync.WaitGroup{}
	for name, pool := range rts.mempools {
		if !p2p.NodeInSlice(name, ids) {
			continue
		}
		if len(txs) == pool.Size() {
			continue
		}

		wg.Add(1)
		go func(name types.NodeID, pool *mempool.TxMempool) {
			defer wg.Done()
			require.Eventually(t, func() bool { return len(txs) == pool.Size() },
				time.Minute,
				250*time.Millisecond,
				"node=%q, ntx=%d, size=%d", name, len(txs), pool.Size(),
			)
		}(name, pool)
	}
	wg.Wait()
}

func peerFailedCheckTxCount(reactor *Reactor, nodeID types.NodeID) utils.Option[int] {
	for counts := range reactor.failedCheckTxCounts.Lock() {
		if count, ok := counts[nodeID]; ok {
			return utils.Some(count)
		}
		return utils.None[int]()
	}
	panic("unreachable")
}

func TestReactorBroadcastTxs(t *testing.T) {
	numTxs := 512
	numNodes := 4
	ctx := t.Context()
	rng := utils.TestRng()

	rts := setupReactors(ctx, t, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondaries := rts.nodes[1:]

	txs := checkTxs(ctx, t, rng, rts.reactors[primary].mempool, numTxs)

	require.Equal(t, numTxs, rts.reactors[primary].mempool.Size())

	rts.start(t)
	rts.waitForTxns(t, convertTex(txs), secondaries...)
}

func TestReactorFailedCheckTxCountEvictsPeer(t *testing.T) {
	for _, broadcast := range []bool{true, false} {
		t.Run(fmt.Sprintf("broadcast=%v", broadcast), func(t *testing.T) {
			ctx := t.Context()

			cfg := config.TestMempoolConfig()
			cfg.Broadcast = broadcast
			cfg.CheckTxErrorBlacklistEnabled = true
			cfg.CheckTxErrorThreshold = 2

			rts := setupReactorsWithConfig(ctx, t, 2, cfg, func() (mempool.TxConstraints, error) {
				return mempool.TxConstraints{
					MaxDataBytes: 10,
					MaxGas:       -1,
				}, nil
			})
			t.Cleanup(leaktest.Check(t))

			sender := rts.nodes[0]
			receiver := rts.nodes[1]
			receiverReactor := rts.reactors[receiver]
			rts.start(t)
			conn := rts.network.Node(receiver).WaitForConnAndGet(ctx, sender)

			msgForTx := func(tx []byte) p2p.RecvMsg[*pb.Message] {
				return p2p.RecvMsg[*pb.Message]{
					From: sender,
					Message: &pb.Message{
						Sum: &pb.Message_Txs{
							Txs: &pb.Txs{Txs: [][]byte{tx}},
						},
					},
				}
			}

			require.Eventually(t, func() bool {
				return peerFailedCheckTxCount(receiverReactor, sender) == utils.Some(0)
			}, time.Second, 50*time.Millisecond)

			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx([]byte("good-1"))))
			require.Equal(t, utils.Some(0), peerFailedCheckTxCount(receiverReactor, sender))

			badTx := []byte("bad-transaction")
			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx(badTx)))
			require.Equal(t, utils.Some(1), peerFailedCheckTxCount(receiverReactor, sender))

			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx([]byte("good-2"))))
			require.Equal(t, utils.Some(1), peerFailedCheckTxCount(receiverReactor, sender))

			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx(badTx)))
			require.Equal(t, utils.Some(2), peerFailedCheckTxCount(receiverReactor, sender))

			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx(badTx)))
			rts.network.Node(receiver).WaitForDisconnect(ctx, conn)
		})
	}
}

func TestReactorPeerDownClearsFailedCheckTxCount(t *testing.T) {
	reactor, _ := setupReactorForTest(
		t,
		func() (mempool.TxConstraints, error) {
			return mempool.TxConstraints{
				MaxDataBytes: 10,
				MaxGas:       -1,
			}, nil
		},
	)
	for counts := range reactor.failedCheckTxCounts.Lock() {
		counts["other"] = 1
	}
	msg := p2p.RecvMsg[*pb.Message]{
		From: "sender",
		Message: &pb.Message{
			Sum: &pb.Message_Txs{
				Txs: &pb.Txs{Txs: [][]byte{[]byte("precheck-bad-transaction")}},
			},
		},
	}

	reactor.cfg.CheckTxErrorBlacklistEnabled = true
	for counts := range reactor.failedCheckTxCounts.Lock() {
		counts["sender"] = 0
	}
	require.Equal(t, utils.Some(0), peerFailedCheckTxCount(reactor, "sender"))

	require.NoError(t, reactor.handleMempoolMessage(t.Context(), msg))
	require.Equal(t, utils.Some(1), peerFailedCheckTxCount(reactor, "sender"))

	for counts := range reactor.failedCheckTxCounts.Lock() {
		delete(counts, "sender")
	}

	require.Equal(t, utils.None[int](), peerFailedCheckTxCount(reactor, "sender"))
	require.Equal(t, utils.Some(1), peerFailedCheckTxCount(reactor, "other"))
}

func TestReactorMissingFailedCheckTxCountIsNotRecreated(t *testing.T) {
	reactor, _ := setupReactorForTest(
		t,
		func() (mempool.TxConstraints, error) {
			return mempool.TxConstraints{
				MaxDataBytes: 10,
				MaxGas:       -1,
			}, nil
		},
	)
	msg := p2p.RecvMsg[*pb.Message]{
		From: "sender",
		Message: &pb.Message{
			Sum: &pb.Message_Txs{
				Txs: &pb.Txs{Txs: [][]byte{[]byte("precheck-bad-transaction")}},
			},
		},
	}

	reactor.cfg.CheckTxErrorBlacklistEnabled = true
	for counts := range reactor.failedCheckTxCounts.Lock() {
		counts["sender"] = 0
		delete(counts, "sender")
	}

	require.NoError(t, reactor.handleMempoolMessage(t.Context(), msg))
	require.Equal(t, utils.None[int](), peerFailedCheckTxCount(reactor, "sender"))
}

func TestReactorConcurrency(t *testing.T) {
	numTxs := 10
	numNodes := 2
	ctx := t.Context()
	rng := utils.TestRng()

	rts := setupReactors(ctx, t, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	rts.start(t)

	var wg sync.WaitGroup
	var primaryHeight int64
	var secondaryHeight int64

	for range runtime.NumCPU() * 2 {
		primaryRng := rng.Split()
		wg.Go(func() {
			txs := checkTxs(ctx, t, primaryRng, rts.reactors[primary].mempool, numTxs)
			txmp := rts.mempools[primary]

			txmp.Lock()
			defer txmp.Unlock()
			primaryHeight++
			height := primaryHeight

			deliverTxResponses := make([]*abci.ExecTxResult, len(txs))
			for i := range txs {
				deliverTxResponses[i] = &abci.ExecTxResult{Code: 0}
			}

			require.NoError(t, txmp.Update(ctx, height, convertTex(txs), deliverTxResponses, mempool.NopTxConstraints(), true))
		})

		secondaryRng := rng.Split()
		wg.Go(func() {
			_ = checkTxs(ctx, t, secondaryRng, rts.reactors[secondary].mempool, numTxs)
			txmp := rts.mempools[secondary]

			txmp.Lock()
			defer txmp.Unlock()
			secondaryHeight++
			height := secondaryHeight

			err := txmp.Update(ctx, height, []types.Tx{}, make([]*abci.ExecTxResult, 0), mempool.NopTxConstraints(), true)
			require.NoError(t, err)
		})
	}

	wg.Wait()
}

func TestReactor_MaxTxBytes(t *testing.T) {
	numNodes := 2
	cfg := config.TestConfig()
	ctx := t.Context()

	rts := setupReactors(ctx, t, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	tx1 := tmrand.Bytes(cfg.Mempool.MaxTxBytes)
	_, err := rts.reactors[primary].mempool.CheckTx(
		ctx,
		tx1,
	)
	require.NoError(t, err)

	rts.start(t)

	rts.reactors[primary].mempool.Flush()
	rts.reactors[secondary].mempool.Flush()

	tx2 := tmrand.Bytes(cfg.Mempool.MaxTxBytes + 1)
	_, err = rts.mempools[primary].CheckTx(ctx, tx2)
	require.Error(t, err)
}

func TestBroadcastTxForPeerStopsWhenPeerStops(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx := t.Context()
	rng := utils.TestRng()

	rts := setupReactors(ctx, t, 2)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	rts.start(t)
	rts.network.Remove(t, secondary)

	txs := checkTxs(ctx, t, rng, rts.reactors[primary].mempool, 4)
	require.Equal(t, 4, len(txs))
	require.Equal(t, 4, rts.mempools[primary].Size())
	require.Equal(t, 0, rts.mempools[secondary].Size())
}
