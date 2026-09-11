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
	return setupMempoolTweaked(t, app, cacheSize, txConstraintsFetcher, nil)
}

func setupMempoolTweaked(
	t testing.TB,
	app *proxy.Proxy,
	cacheSize int,
	txConstraintsFetcher mempool.TxConstraintsFetcher,
	tweak func(*config.MempoolConfig),
) *mempool.TxMempool {
	t.Helper()

	cfg, err := config.ResetTestRoot(t.TempDir(), strings.ReplaceAll(t.Name(), "/", "|"))
	require.NoError(t, err)
	cfg.Mempool.CacheSize = cacheSize
	cfg.Mempool.DropUtilisationThreshold = 0.0
	if tweak != nil {
		tweak(cfg.Mempool)
	}

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
	return setupReactorsWithNodeMempool(ctx, t, numNodes, cfg, txConstraintsFetcher, nil)
}

func setupReactorsWithNodeMempool(
	ctx context.Context,
	t *testing.T,
	numNodes int,
	cfg *config.MempoolConfig,
	txConstraintsFetcher mempool.TxConstraintsFetcher,
	tweakNodeMempool func(int, *config.MempoolConfig),
) *reactorTestSuite {
	t.Helper()

	rts := &reactorTestSuite{
		network:  p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: numNodes}),
		reactors: make(map[types.NodeID]*Reactor, numNodes),
		mempools: make(map[types.NodeID]*mempool.TxMempool, numNodes),
		kvstores: make(map[types.NodeID]*kvstore.Application, numNodes),
	}

	for i, node := range rts.network.Nodes() {
		nodeID := node.NodeID
		rts.kvstores[nodeID] = kvstore.NewApplication()

		app := rts.kvstores[nodeID]
		proxyApp := proxy.New(app)
		var tweak func(*config.MempoolConfig)
		if tweakNodeMempool != nil {
			tweak = func(mc *config.MempoolConfig) { tweakNodeMempool(i, mc) }
		}
		txmp := setupMempoolTweaked(t, proxyApp, 0, txConstraintsFetcher, tweak)
		rts.mempools[nodeID] = txmp

		nodeCfg := *cfg
		reactor, err := NewReactor(&nodeCfg, txmp, node.Router)
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

func txConstraintsWithMaxDataBytes(maxDataBytes int64) mempool.TxConstraintsFetcher {
	return func() (mempool.TxConstraints, error) {
		return mempool.TxConstraints{MaxDataBytes: maxDataBytes, MaxGas: -1}, nil
	}
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

			good1 := []byte("good-1")
			good2 := []byte("good-2")
			maxDataBytes := types.ComputeProtoSizeForTxs([]types.Tx{good1})
			if n := types.ComputeProtoSizeForTxs([]types.Tx{good2}); n > maxDataBytes {
				maxDataBytes = n
			}
			badTx := []byte("bad=" + strings.Repeat("x", 64))
			require.Greater(t, types.ComputeProtoSizeForTxs([]types.Tx{badTx}), maxDataBytes)

			rts := setupReactorsWithConfig(ctx, t, 2, cfg, txConstraintsWithMaxDataBytes(maxDataBytes))
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

			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx(good1)))
			require.Equal(t, utils.Some(0), peerFailedCheckTxCount(receiverReactor, sender))

			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx(badTx)))
			require.Equal(t, utils.Some(1), peerFailedCheckTxCount(receiverReactor, sender))

			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx(good2)))
			require.Equal(t, utils.Some(1), peerFailedCheckTxCount(receiverReactor, sender))

			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx(badTx)))
			require.Equal(t, utils.Some(2), peerFailedCheckTxCount(receiverReactor, sender))

			require.NoError(t, receiverReactor.handleMempoolMessage(ctx, msgForTx(badTx)))
			rts.network.Node(receiver).WaitForDisconnect(ctx, conn)
		})
	}
}

func TestReactorPeerDownClearsFailedCheckTxCount(t *testing.T) {
	reactor, _ := setupReactorForTest(t, txConstraintsWithMaxDataBytes(1))
	for counts := range reactor.failedCheckTxCounts.Lock() {
		counts["other"] = 1
	}
	msg := p2p.RecvMsg[*pb.Message]{
		From: "sender",
		Message: &pb.Message{
			Sum: &pb.Message_Txs{
				Txs: &pb.Txs{Txs: [][]byte{[]byte("x")}},
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
	reactor, _ := setupReactorForTest(t, txConstraintsWithMaxDataBytes(1))
	msg := p2p.RecvMsg[*pb.Message]{
		From: "sender",
		Message: &pb.Message{
			Sum: &pb.Message_Txs{
				Txs: &pb.Txs{Txs: [][]byte{[]byte("x")}},
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
	ctx := t.Context()

	rts := setupReactors(ctx, t, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	tx1 := tmrand.Bytes(types.MaxGossipTxBytes)
	_, err := rts.reactors[primary].mempool.CheckTx(
		ctx,
		tx1,
	)
	require.NoError(t, err)

	rts.start(t)

	rts.reactors[primary].mempool.Flush()
	rts.reactors[secondary].mempool.Flush()

	tx2 := tmrand.Bytes(types.MaxGossipTxBytes + 1)
	_, err = rts.mempools[primary].CheckTx(ctx, tx2)
	require.Error(t, err)
}

func TestGetChannelDescriptorProtocolRecvCapacity(t *testing.T) {
	// Setup: mempool channel descriptor used by every node.
	desc := GetChannelDescriptor()

	// Test: receive capacity is the protocol gossip envelope, not local max-tx-bytes.
	// Verify: capacity covers MaxGossipTxBytes and oversized messages are discarded.
	require.Equal(t, mempoolRecvMessageCapacity(), desc.RecvMessageCapacity)
	require.GreaterOrEqual(t, desc.RecvMessageCapacity, types.MaxGossipTxBytes)
	require.True(t, desc.DiscardOversized)
}

func TestReactorMismatchedMaxTxBytesKeepsConnection(t *testing.T) {
	ctx := t.Context()

	// Setup: nodes with different max-tx-bytes toml; admission is the protocol cap.
	senderMaxTxBytes := 512
	receiverMaxTxBytes := 64
	limits := []int{senderMaxTxBytes, receiverMaxTxBytes}
	rts := setupReactorsWithNodeMempool(ctx, t, 2, config.TestMempoolConfig(), mempool.NopTxConstraintsFetcher,
		func(i int, mempoolCfg *config.MempoolConfig) {
			mempoolCfg.MaxTxBytes = limits[i]
		})
	t.Cleanup(leaktest.Check(t))

	sender := rts.nodes[0]
	receiver := rts.nodes[1]
	rts.start(t)
	rts.network.Node(receiver).WaitForConnAndGet(ctx, sender)
	require.Eventually(t, func() bool {
		return peerFailedCheckTxCount(rts.reactors[receiver], sender) == utils.Some(0)
	}, time.Second, 50*time.Millisecond)

	tx := []byte("large=" + strings.Repeat("x", 200))
	require.Greater(t, len(tx), receiverMaxTxBytes)
	require.LessOrEqual(t, len(tx), senderMaxTxBytes)
	require.LessOrEqual(t, len(tx), types.MaxGossipTxBytes)

	// Test: gossip a tx above the receiver's toml max-tx-bytes and below the protocol cap.
	_, err := rts.mempools[sender].CheckTx(ctx, tx)
	require.NoError(t, err)

	// Verify: the receiver admits it, the connection survived, and the sender is not blacklisted.
	rts.waitForTxns(t, []types.Tx{tx}, receiver)
	require.Equal(t, 1, rts.mempools[receiver].Size())
	require.Equal(t, utils.Some(0), peerFailedCheckTxCount(rts.reactors[receiver], sender))
}

func TestBroadcastSkipsProtocolOversizedTx(t *testing.T) {
	ctx := t.Context()

	// Setup: two connected reactors. CheckTx will not admit a protocol-oversized
	// tx, so the oversized bytes are placed on the gossip list directly.
	rts := setupReactors(ctx, t, 2)
	t.Cleanup(leaktest.Check(t))

	sender := rts.nodes[0]
	receiver := rts.nodes[1]
	rts.reactors[receiver].cfg.Broadcast = false
	rts.start(t)
	rts.network.Node(receiver).WaitForConnAndGet(ctx, sender)
	sentBefore := p2p.ChannelOutMsgs(MempoolChannel)

	oversized := types.Tx(make([]byte, types.MaxGossipTxBytes+1))
	require.NoError(t, rts.mempools[sender].InsertReadyTxForTest(oversized))

	okTx := types.Tx("gossip-ok=1")
	_, err := rts.mempools[sender].CheckTx(ctx, okTx)
	require.NoError(t, err)

	// Test: broadcast walks an oversized tx then a gossip-legal tx.
	// Verify: only the legal tx is sent.
	require.Eventually(t, func() bool {
		found, missing := rts.mempools[receiver].SafeGetTxsForHashes([]types.TxHash{okTx.Hash()})
		return len(missing) == 0 && len(found) == 1
	}, time.Minute, 50*time.Millisecond)
	require.Equal(t, sentBefore+1, p2p.ChannelOutMsgs(MempoolChannel))
	require.Equal(t, 1, rts.mempools[receiver].Size())
	_, missing := rts.mempools[receiver].SafeGetTxsForHashes([]types.TxHash{oversized.Hash()})
	require.Equal(t, []types.TxHash{oversized.Hash()}, missing)
}

func TestReactorProtocolOversizedTxIsCounted(t *testing.T) {
	// Setup: peer is already tracked so protocol oversize can increment the blacklist.
	reactor, _ := setupReactorForTest(t, mempool.NopTxConstraintsFetcher)
	reactor.cfg.CheckTxErrorBlacklistEnabled = true
	for counts := range reactor.failedCheckTxCounts.Lock() {
		counts["sender"] = 0
	}

	tx := make([]byte, types.MaxGossipTxBytes+1)
	msg := p2p.RecvMsg[*pb.Message]{
		From: "sender",
		Message: &pb.Message{
			Sum: &pb.Message_Txs{Txs: &pb.Txs{Txs: [][]byte{tx}}},
		},
	}

	// Test: a tx above the protocol gossip size is skipped and counted.
	require.NoError(t, reactor.handleMempoolMessage(t.Context(), msg))

	// Verify: the sender is charged one protocol violation and the tx is not in the mempool.
	require.Equal(t, utils.Some(1), peerFailedCheckTxCount(reactor, "sender"))
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
