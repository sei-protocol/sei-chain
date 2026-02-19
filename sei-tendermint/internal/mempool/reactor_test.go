package mempool

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
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type reactorTestSuite struct {
	network *p2p.TestNetwork
	logger  log.Logger

	reactors map[types.NodeID]*Reactor
	mempools map[types.NodeID]*TxMempool
	kvstores map[types.NodeID]*kvstore.Application

	nodes []types.NodeID
}

func setupReactors(ctx context.Context, t *testing.T, logger log.Logger, numNodes int) *reactorTestSuite {
	t.Helper()

	cfg, err := config.ResetTestRoot(t.TempDir(), strings.ReplaceAll(t.Name(), "/", "|"))
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(cfg.RootDir) })

	rts := &reactorTestSuite{
		logger:   log.NewNopLogger().With("testCase", t.Name()),
		network:  p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: numNodes}),
		reactors: make(map[types.NodeID]*Reactor, numNodes),
		mempools: make(map[types.NodeID]*TxMempool, numNodes),
		kvstores: make(map[types.NodeID]*kvstore.Application, numNodes),
	}

	for _, node := range rts.network.Nodes() {
		nodeID := node.NodeID
		rts.kvstores[nodeID] = kvstore.NewApplication()

		app := rts.kvstores[nodeID]
		mempool := setup(t, app, 0)
		rts.mempools[nodeID] = mempool

		reactor, err := NewReactor(
			rts.logger.With("nodeID", nodeID),
			cfg.Mempool,
			mempool,
			node.Router,
		)
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

func (rts *reactorTestSuite) start(t *testing.T) {
	t.Helper()
	rts.network.Start(t)
}

func (rts *reactorTestSuite) waitForTxns(t *testing.T, txs []types.Tx, ids ...types.NodeID) {
	t.Helper()

	// ensure that the transactions get fully broadcast to the
	// rest of the network
	wg := &sync.WaitGroup{}
	for name, pool := range rts.mempools {
		if !p2p.NodeInSlice(name, ids) {
			continue
		}
		if len(txs) == pool.Size() {
			continue
		}

		wg.Add(1)
		go func(name types.NodeID, pool *TxMempool) {
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

func TestReactorBroadcastDoesNotPanic(t *testing.T) {
	ctx := t.Context()

	const numNodes = 2

	logger := log.NewNopLogger()
	rts := setupReactors(ctx, t, logger, numNodes)
	t.Cleanup(leaktest.Check(t))

	observePanic := func(r any) {
		t.Fatal("panic detected in reactor")
	}

	primary := rts.nodes[0]
	secondary := rts.nodes[1]
	primaryReactor := rts.reactors[primary]
	primaryMempool := primaryReactor.mempool
	secondaryReactor := rts.reactors[secondary]

	primaryReactor.observePanic = observePanic
	secondaryReactor.observePanic = observePanic

	firstTx := &WrappedTx{}
	primaryMempool.insertTx(firstTx)

	// run the router
	rts.start(t)

	go primaryReactor.broadcastTxRoutine(ctx, secondary)

	wg := &sync.WaitGroup{}
	for range 50 {
		next := &WrappedTx{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			primaryMempool.insertTx(next)
		}()
	}

	primaryReactor.Stop()
	wg.Wait()
}

func TestReactorBroadcastTxs(t *testing.T) {
	numTxs := 512
	numNodes := 4
	ctx := t.Context()

	logger := log.NewNopLogger()

	rts := setupReactors(ctx, t, logger, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondaries := rts.nodes[1:]

	txs := checkTxs(ctx, t, rts.reactors[primary].mempool, numTxs, UnknownPeerID)

	require.Equal(t, numTxs, rts.reactors[primary].mempool.Size())

	rts.start(t)

	// Wait till all secondary suites (reactor) received all mempool txs from the
	// primary suite (node).
	rts.waitForTxns(t, convertTex(txs), secondaries...)
}

// regression test for https://github.com/tendermint/tendermint/issues/5408
func TestReactorConcurrency(t *testing.T) {
	numTxs := 10
	numNodes := 2

	ctx := t.Context()

	logger := log.NewNopLogger()
	rts := setupReactors(ctx, t, logger, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	rts.start(t)

	var wg sync.WaitGroup

	for i := 0; i < runtime.NumCPU()*2; i++ {
		wg.Add(2)

		// 1. submit a bunch of txs
		// 2. update the whole mempool

		txs := checkTxs(ctx, t, rts.reactors[primary].mempool, numTxs, UnknownPeerID)
		go func() {
			defer wg.Done()

			mempool := rts.mempools[primary]

			mempool.Lock()
			defer mempool.Unlock()

			deliverTxResponses := make([]*abci.ExecTxResult, len(txs))
			for i := range txs {
				deliverTxResponses[i] = &abci.ExecTxResult{Code: 0}
			}

			require.NoError(t, mempool.Update(ctx, 1, convertTex(txs), deliverTxResponses, nil, nil, true))
		}()

		// 1. submit a bunch of txs
		// 2. update none
		_ = checkTxs(ctx, t, rts.reactors[secondary].mempool, numTxs, UnknownPeerID)
		go func() {
			defer wg.Done()

			mempool := rts.mempools[secondary]

			mempool.Lock()
			defer mempool.Unlock()

			err := mempool.Update(ctx, 1, []types.Tx{}, make([]*abci.ExecTxResult, 0), nil, nil, true)
			require.NoError(t, err)
		}()
	}

	wg.Wait()
}

func TestReactorNoBroadcastToSender(t *testing.T) {
	numTxs := 1000
	numNodes := 2

	ctx := t.Context()

	logger := log.NewNopLogger()
	rts := setupReactors(ctx, t, logger, numNodes)
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

	logger := log.NewNopLogger()

	rts := setupReactors(ctx, t, logger, numNodes)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	// Broadcast a tx, which has the max size and ensure it's received by the
	// second reactor.
	tx1 := tmrand.Bytes(cfg.Mempool.MaxTxBytes)
	err := rts.reactors[primary].mempool.CheckTx(
		ctx,
		tx1,
		nil,
		TxInfo{
			SenderID: UnknownPeerID,
		},
	)
	require.NoError(t, err)

	rts.start(t)

	rts.reactors[primary].mempool.Flush()
	rts.reactors[secondary].mempool.Flush()

	// broadcast a tx, which is beyond the max size and ensure it's not sent
	tx2 := tmrand.Bytes(cfg.Mempool.MaxTxBytes + 1)
	err = rts.mempools[primary].CheckTx(ctx, tx2, nil, TxInfo{SenderID: UnknownPeerID})
	require.Error(t, err)
}

func TestDontExhaustMaxActiveIDs(t *testing.T) {
	t.Skip("this test fails, but the property it tests is not very useful")
	// we're creating a single node network, but not starting the
	// network.

	ctx := t.Context()

	logger := log.NewNopLogger()
	rts := setupReactors(ctx, t, logger, 1)
	t.Cleanup(leaktest.Check(t))

	nodeID := rts.nodes[0]

	// ensure the reactor does not panic (i.e. exhaust active IDs)
	for range MaxActiveIDs + 1 {
		privKey := ed25519.GenerateSecretKey()
		peerID := types.NodeIDFromPubKey(privKey.Public())
		rts.reactors[nodeID].processPeerUpdate(ctx, p2p.PeerUpdate{
			Status: p2p.PeerStatusUp,
			NodeID: peerID,
		})
	}
}

func TestMempoolIDsPanicsIfNodeRequestsOvermaxActiveIDs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	// 0 is already reserved for UnknownPeerID
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

	logger := log.NewNopLogger()

	rts := setupReactors(ctx, t, logger, 2)
	t.Cleanup(leaktest.Check(t))

	primary := rts.nodes[0]
	secondary := rts.nodes[1]

	rts.start(t)

	// disconnect peer
	rts.network.Remove(t, secondary)

	txs := checkTxs(ctx, t, rts.reactors[primary].mempool, 4, UnknownPeerID)
	require.Equal(t, 4, len(txs))
	require.Equal(t, 4, rts.mempools[primary].Size())
	require.Equal(t, 0, rts.mempools[secondary].Size())
}
