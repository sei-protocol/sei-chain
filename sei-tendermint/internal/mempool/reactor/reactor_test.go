package reactor

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
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

func setupMempool(t testing.TB, app abci.Application, cacheSize int, txConstraintsFetcher mempool.TxConstraintsFetcher) *mempool.TxMempool {
	t.Helper()

	cfg, err := config.ResetTestRoot(t.TempDir(), strings.ReplaceAll(t.Name(), "/", "|"))
	require.NoError(t, err)
	cfg.Mempool.CacheSize = cacheSize
	cfg.Mempool.DropUtilisationThreshold = 0.0

	t.Cleanup(func() { os.RemoveAll(cfg.RootDir) })

	return mempool.NewTxMempool(cfg.Mempool, app, mempool.NopMetrics(), txConstraintsFetcher)
}

func checkTxs(ctx context.Context, t *testing.T, txmp *mempool.TxMempool, numTxs int, peerID uint16) []testTx {
	t.Helper()

	txs := make([]testTx, numTxs)
	txInfo := mempool.TxInfo{SenderID: peerID}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := range numTxs {
		prefix := make([]byte, 20)
		_, err := rng.Read(prefix)
		require.NoError(t, err)

		txs[i] = testTx{
			tx: []byte(fmt.Sprintf("sender-%d-%d=%X=%d", i, peerID, prefix, i+1000)),
		}
		require.NoError(t, txmp.CheckTx(ctx, txs[i].tx, nil, txInfo))
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
	return setupReactorsWithTxConstraintsFetchers(ctx, t, numNodes, nil)
}

func setupReactorsWithTxConstraintsFetchers(
	ctx context.Context,
	t *testing.T,
	numNodes int,
	txConstraintsFetchers map[int]mempool.TxConstraintsFetcher,
) *reactorTestSuite {
	t.Helper()

	cfg, err := config.ResetTestRoot(t.TempDir(), strings.ReplaceAll(t.Name(), "/", "|"))
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(cfg.RootDir) })

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
		txConstraintsFetcher := mempool.NopTxConstraintsFetcher
		if customFetcher, ok := txConstraintsFetchers[i]; ok {
			txConstraintsFetcher = customFetcher
		}
		txmp := setupMempool(t, app, 0, txConstraintsFetcher)
		rts.mempools[nodeID] = txmp

		reactor, err := NewReactor(txmp, node.Router)
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

	txmp := mempool.NewTxMempool(cfg.Mempool, kvstore.NewApplication(), mempool.NopMetrics(), txConstraintsFetcher)
	reactor, err := NewReactor(txmp, node.Router)
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

	rts := setupReactors(ctx, t, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondaries := rts.nodes[1:]

	txs := checkTxs(ctx, t, rts.reactors[primary].mempool, numTxs, mempool.UnknownPeerID)

	require.Equal(t, numTxs, rts.reactors[primary].mempool.Size())

	rts.start(t)
	rts.waitForTxns(t, convertTex(txs), secondaries...)
}

func TestReactorFailedCheckTxCountEvictsPeer(t *testing.T) {
	ctx := t.Context()

	rts := setupReactorsWithTxConstraintsFetchers(ctx, t, 2, map[int]mempool.TxConstraintsFetcher{
		1: func() (mempool.TxConstraints, error) {
			return mempool.TxConstraints{
				MaxDataBytes: 10,
				MaxGas:       -1,
			}, nil
		},
	})
	t.Cleanup(leaktest.Check(t))

	sender := rts.nodes[0]
	receiver := rts.nodes[1]

	receiverReactor := rts.reactors[receiver]
	receiverReactor.cfg.CheckTxErrorBlacklistEnabled = true
	receiverReactor.cfg.CheckTxErrorThreshold = 2

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

	reactor.ids.Reclaim("sender")
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
	reactor.ids.Reclaim("sender")

	require.NoError(t, reactor.handleMempoolMessage(t.Context(), msg))
	require.Equal(t, utils.None[int](), peerFailedCheckTxCount(reactor, "sender"))
}

func TestReactorConcurrency(t *testing.T) {
	numTxs := 10
	numNodes := 2
	ctx := t.Context()

	rts := setupReactors(ctx, t, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	rts.start(t)

	var wg sync.WaitGroup

	for range runtime.NumCPU() * 2 {
		wg.Add(2)

		txs := checkTxs(ctx, t, rts.reactors[primary].mempool, numTxs, mempool.UnknownPeerID)
		go func() {
			defer wg.Done()

			txmp := rts.mempools[primary]

			txmp.Lock()
			defer txmp.Unlock()

			deliverTxResponses := make([]*abci.ExecTxResult, len(txs))
			for i := range txs {
				deliverTxResponses[i] = &abci.ExecTxResult{Code: 0}
			}

			require.NoError(t, txmp.Update(ctx, 1, convertTex(txs), deliverTxResponses, mempool.NopTxConstraintsFetcher, true))
		}()

		_ = checkTxs(ctx, t, rts.reactors[secondary].mempool, numTxs, mempool.UnknownPeerID)
		go func() {
			defer wg.Done()

			txmp := rts.mempools[secondary]

			txmp.Lock()
			defer txmp.Unlock()

			err := txmp.Update(ctx, 1, []types.Tx{}, make([]*abci.ExecTxResult, 0), mempool.NopTxConstraintsFetcher, true)
			require.NoError(t, err)
		}()
	}

	wg.Wait()
}

func TestReactorNoBroadcastToSender(t *testing.T) {
	numTxs := 1000
	numNodes := 2
	ctx := t.Context()

	rts := setupReactors(ctx, t, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	peerID := uint16(1)
	_ = checkTxs(ctx, t, rts.mempools[primary], numTxs, peerID)

	rts.start(t)
	time.Sleep(100 * time.Millisecond)

	require.Eventually(t, func() bool {
		return rts.mempools[secondary].Size() == 0
	}, time.Minute, 100*time.Millisecond)
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
	err := rts.reactors[primary].mempool.CheckTx(
		ctx,
		tx1,
		nil,
		mempool.TxInfo{SenderID: mempool.UnknownPeerID},
	)
	require.NoError(t, err)

	rts.start(t)

	rts.reactors[primary].mempool.Flush()
	rts.reactors[secondary].mempool.Flush()

	tx2 := tmrand.Bytes(cfg.Mempool.MaxTxBytes + 1)
	err = rts.mempools[primary].CheckTx(ctx, tx2, nil, mempool.TxInfo{SenderID: mempool.UnknownPeerID})
	require.Error(t, err)
}

func TestDontExhaustMaxActiveIDs(t *testing.T) {
	t.Skip("this test fails, but the property it tests is not very useful")

	ctx := t.Context()
	rts := setupReactors(ctx, t, 1)
	t.Cleanup(leaktest.Check(t))

	nodeID := rts.nodes[0]

	for range MaxActiveIDs + 1 {
		privKey := ed25519.GenerateSecretKey()
		peerID := types.NodeIDFromPubKey(privKey.Public())
		rts.reactors[nodeID].ids.ReserveForPeer(peerID)
	}
}

func TestMempoolIDsPanicsIfNodeRequestsOvermaxActiveIDs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ids := NewMempoolIDs()

	for i := range MaxActiveIDs - 1 {
		peerID, err := types.NewNodeID(fmt.Sprintf("%040d", i))
		require.NoError(t, err)
		ids.ReserveForPeer(peerID)
	}

	peerID, err := types.NewNodeID(fmt.Sprintf("%040d", MaxActiveIDs-1))
	require.NoError(t, err)
	require.Panics(t, func() {
		ids.ReserveForPeer(peerID)
	})
}

func TestBroadcastTxForPeerStopsWhenPeerStops(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx := t.Context()

	rts := setupReactors(ctx, t, 2)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	rts.start(t)
	rts.network.Remove(t, secondary)

	txs := checkTxs(ctx, t, rts.reactors[primary].mempool, 4, mempool.UnknownPeerID)
	require.Equal(t, 4, len(txs))
	require.Equal(t, 4, rts.mempools[primary].Size())
	require.Equal(t, 0, rts.mempools[secondary].Size())
}
