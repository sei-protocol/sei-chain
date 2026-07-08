package blocksync

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	sf "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type reactorTestSuite struct {
	network *p2p.TestNetwork
	nodes   []types.NodeID

	reactors map[types.NodeID]*Reactor
}

func setup(
	ctx context.Context,
	t *testing.T,
	genDoc *types.GenesisDoc,
	privVal types.PrivValidator,
	maxBlockHeights []int64,
) *reactorTestSuite {
	t.Helper()

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	numNodes := len(maxBlockHeights)
	require.True(t, numNodes >= 1,
		"must specify at least one block height (nodes)")

	rts := &reactorTestSuite{
		network:  p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: numNodes}),
		nodes:    make([]types.NodeID, 0, numNodes),
		reactors: make(map[types.NodeID]*Reactor, numNodes),
	}

	for i, nodeID := range rts.network.NodeIDs() {
		rts.addNode(ctx, t, nodeID, genDoc, privVal, maxBlockHeights[i])
	}

	t.Cleanup(func() {
		cancel()
		for _, nodeID := range rts.nodes {
			if rts.reactors[nodeID].IsRunning() {
				rts.reactors[nodeID].Wait()

				require.False(t, rts.reactors[nodeID].IsRunning())
			}
		}
	})
	t.Cleanup(leaktest.Check(t))

	return rts
}

func makeReactor(
	ctx context.Context,
	t *testing.T,
	genDoc *types.GenesisDoc,
	router *p2p.Router,
	blockSync bool,
	restartEvent func(),
	selfRemediationConfig *config.SelfRemediationConfig,
) *Reactor {

	app := abci.BaseApplication{}

	blockDB := dbm.NewMemDB()
	stateDB := dbm.NewMemDB()
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(blockDB)
	proxyApp := proxy.New(app, proxy.NewMetrics())

	state, err := sm.MakeGenesisState(genDoc)
	require.NoError(t, err)
	require.NoError(t, stateStore.Save(state))
	mp := mempool.NewTxMempool(mempool.TestConfig(), proxyApp, mempool.NewMetrics(), mempool.NopTxConstraintsFetcher)
	bus := eventbus.NewDefault()
	require.NoError(t, bus.Start(ctx))

	blockExec := sm.NewBlockExecutor(
		stateStore,
		proxyApp,
		mp,
		sm.EmptyEvidencePool{},
		blockStore,
		bus,
		sm.NewMetrics(),
		types.DefaultConsensusPolicy(),
	)

	r, err := NewReactor(
		stateStore,
		blockStore,
		router,
		utils.Some(SyncerConfig{
			BlockExec:             blockExec,
			ConsReactor:           utils.None[ConsensusReactor](),
			BlockSync:             blockSync,
			Metrics:               consensus.NewMetrics(),
			EventBus:              nil, // eventbus can be nil
			RestartEvent:          restartEvent,
			SelfRemediationConfig: selfRemediationConfig,
		}),
	)
	if err != nil {
		t.Fatalf("NewReactor(): %v", err)
	}
	return r
}

func (rts *reactorTestSuite) addNode(
	ctx context.Context,
	t *testing.T,
	nodeID types.NodeID,
	genDoc *types.GenesisDoc,
	privVal types.PrivValidator,
	maxBlockHeight int64,
) {
	t.Helper()

	rts.nodes = append(rts.nodes, nodeID)

	remediationConfig := config.DefaultSelfRemediationConfig()
	remediationConfig.BlocksBehindThreshold = 1000

	reactor := makeReactor(
		ctx,
		t,
		genDoc,
		rts.network.Node(nodeID).Router,
		true,
		func() {},
		remediationConfig,
	)
	lastCommit := &types.Commit{}

	state, err := reactor.stateStore.Load()
	require.NoError(t, err)
	for blockHeight := int64(1); blockHeight <= maxBlockHeight; blockHeight++ {
		block, blockID, partSet, seenCommit := makeNextBlock(ctx, t, state, privVal, blockHeight, lastCommit)

		syncer := reactor.syncer.OrPanic("syncer should be configured in tests")
		state, err = syncer.blockExec.ApplyBlock(ctx, state, blockID, block, nil)
		require.NoError(t, err)

		reactor.store.SaveBlock(block, partSet, seenCommit)
		lastCommit = seenCommit
	}

	rts.reactors[nodeID] = reactor
	require.NoError(t, reactor.Start(ctx))
	require.True(t, reactor.IsRunning())
}

func makeNextBlock(ctx context.Context,
	t *testing.T,
	state sm.State,
	signer types.PrivValidator,
	height int64,
	lc *types.Commit) (*types.Block, types.BlockID, *types.PartSet, *types.Commit) {
	block := sf.MakeBlock(state, height, lc)
	partSet, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: partSet.Header()}

	// Simulate a commit for the current height
	vote, err := factory.MakeVote(
		ctx,
		signer,
		block.Header.ChainID,
		0,
		block.Header.Height,
		0,
		2,
		blockID,
		time.Now(),
	)
	require.NoError(t, err)
	seenCommit := &types.Commit{
		Height:     vote.Height,
		Round:      vote.Round,
		BlockID:    blockID,
		Signatures: []types.CommitSig{vote.CommitSig()},
	}
	return block, blockID, partSet, seenCommit
}

func (rts *reactorTestSuite) start(t *testing.T) {
	t.Helper()
	rts.network.Start(t)
}

func TestReactor_AbruptDisconnect(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_reactor_test")
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	maxBlockHeight := int64(64)

	rts := setup(ctx, t, genDoc, privVals[0], []int64{maxBlockHeight, 0})

	require.Equal(t, maxBlockHeight, rts.reactors[rts.nodes[0]].store.Height())

	rts.start(t)

	secondarySyncer := rts.reactors[rts.nodes[1]].syncer.OrPanic("syncer should be configured in tests")

	require.Eventually(
		t,
		func() bool {
			pool := secondarySyncer.pool.Load()
			if pool == nil {
				return false
			}
			height, _, _ := pool.GetStatus()
			return pool.MaxPeerHeight() == maxBlockHeight && height > 0 && height <= maxBlockHeight
		},
		10*time.Second,
		10*time.Millisecond,
		"expected node to be partially synced",
	)

	// Remove synced node from the syncing node which should not result in any
	// deadlocks or race conditions within the context of poolRoutine.
	rts.network.Remove(t, rts.nodes[0])
}

// TestReactor_OnStopWaitsForGoroutines is a regression test for the
// "panic: leveldb/table: reader released" shutdown panic seen on v6.4.4
// sentry nodes. Before the fix, blocksync's long-running goroutines
// (Reactor.requestRoutine, Reactor.poolRoutine, Reactor.processBlockSyncCh,
// Reactor.processPeerUpdates, Reactor.autoRestartIfBehind, and
// BlockPool.makeRequestersRoutine) were started with raw `go fn(ctx)` using
// the outer ctx, instead of `Spawn(...)` which would register them with the
// BaseService WaitGroup and bind them to BaseService.inner.ctx. As a result,
// Reactor.Stop() / BlockPool.Stop() — which cancels only the inner ctx —
// did not signal these goroutines to exit, let alone wait for them. The
// node's OnStop then proceeded to n.blockStore.Close() while poolRoutine
// was still mid-SaveBlock -> Base() -> bs.db.Iterator, causing goleveldb to
// panic when the table reader was released underneath the live iterator.
//
// This test asserts the fix: after `reactor.Stop()` returns, the
// blocksync-package goroutines have exited. The outer ctx is still live at
// this point in the test, so the unfixed code keeps them running and the
// assertion fails deterministically. On failure the live goroutine stacks
// are dumped to make the leak obvious.
func TestReactor_OnStopWaitsForGoroutines(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_reactor_stop_test")
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())

	rts := setup(ctx, t, genDoc, privVals[0], []int64{0})

	reactor := rts.reactors[rts.nodes[0]]
	require.True(t, reactor.IsRunning())

	dumpBlocksyncGoroutines := func() (string, int) {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		var out strings.Builder
		count := 0
		for _, g := range strings.Split(string(buf[:n]), "\n\n") {
			if !strings.Contains(g, "/internal/blocksync.") {
				continue
			}
			// The test functions themselves live in the blocksync package, so
			// runtime.Stack reports them as matches. Only count background
			// routines spawned by Reactor.OnStart and BlockPool.OnStart,
			// which are created by libs/service.Spawn, not testing.tRunner.
			if strings.Contains(g, "testing.tRunner") {
				continue
			}
			out.WriteString(g)
			out.WriteString("\n\n")
			count++
		}
		return out.String(), count
	}

	// OnStart Spawns 5 reactor routines and BlockPool.OnStart Spawns 1.
	require.Eventually(t, func() bool {
		_, c := dumpBlocksyncGoroutines()
		return c >= 6
	}, 5*time.Second, 10*time.Millisecond, "blocksync goroutines did not start")

	reactor.Stop()
	require.False(t, reactor.IsRunning())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, c := dumpBlocksyncGoroutines(); c == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	dump, c := dumpBlocksyncGoroutines()
	t.Fatalf("%d blocksync goroutine(s) still alive after Reactor.Stop() returned. "+
		"This means at least one routine was not registered with the "+
		"BaseService WaitGroup via Spawn(), so Stop did not wait for it. "+
		"Live stacks:\n\n%s", c, dump)
}

func TestReactor_SyncTime(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_reactor_test")
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	maxBlockHeight := int64(101)

	rts := setup(ctx, t, genDoc, privVals[0], []int64{maxBlockHeight, 0})
	require.Equal(t, maxBlockHeight, rts.reactors[rts.nodes[0]].store.Height())
	rts.start(t)

	require.Eventually(
		t,
		func() bool {
			pool := rts.reactors[rts.nodes[1]].syncer.OrPanic("syncer should be configured in tests").pool.Load()
			if pool == nil {
				return false
			}
			return rts.reactors[rts.nodes[1]].GetRemainingSyncTime() > time.Nanosecond &&
				pool.getLastSyncRate() > 0.001
		},
		10*time.Second,
		10*time.Millisecond,
		"expected node to be partially synced",
	)
}

type MockBlockStore struct {
	mock.Mock
	sm.BlockStore
}

func (m *MockBlockStore) Height() int64 {
	args := m.Called()
	return args.Get(0).(int64)
}

func TestAutoRestartIfBehind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                      string
		blocksBehindThreshold     uint64
		blocksBehindCheckInterval time.Duration
		selfHeight                int64
		maxPeerHeight             int64
		isBlockSync               bool
		restartExpected           bool
	}{
		{
			name:                      "Should not restart if blocksBehindThreshold is 0",
			blocksBehindThreshold:     0,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			selfHeight:                100,
			maxPeerHeight:             200,
			isBlockSync:               false,
			restartExpected:           false,
		},
		{
			name:                      "Should not restart if behindHeight is less than threshold",
			blocksBehindThreshold:     50,
			selfHeight:                100,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			maxPeerHeight:             140,
			isBlockSync:               false,
			restartExpected:           false,
		},
		{
			name:                      "Should restart if behindHeight is greater than or equal to threshold",
			blocksBehindThreshold:     50,
			selfHeight:                100,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			maxPeerHeight:             160,
			isBlockSync:               false,
			restartExpected:           true,
		},
		{
			name:                      "Should not restart if blocksync",
			blocksBehindThreshold:     50,
			selfHeight:                100,
			blocksBehindCheckInterval: 10 * time.Millisecond,
			maxPeerHeight:             160,
			isBlockSync:               true,
			restartExpected:           false,
		},
	}

	for _, tt := range tests {
		t.Log(tt.name)
		t.Run(tt.name, func(t *testing.T) {
			mockBlockStore := new(MockBlockStore)
			mockBlockStore.On("Height").Return(tt.selfHeight)

			blockPool := &BlockPool{
				height:        tt.selfHeight,
				maxPeerHeight: tt.maxPeerHeight,
			}

			restart := utils.NewAtomicSend(false)
			syncer := &syncController{
				store:                     mockBlockStore,
				blocksBehindThreshold:     tt.blocksBehindThreshold,
				blocksBehindCheckInterval: tt.blocksBehindCheckInterval,
				restartEvent:              func() { restart.Store(true) },
			}
			if tt.isBlockSync {
				syncer.blockSync.Store(true)
			}
			r := &Reactor{syncer: utils.Some(syncer)}

			ctx := t.Context()
			if tt.restartExpected {
				r.syncer.OrPanic("syncer").autoRestartIfBehind(ctx, blockPool)
				assert.True(t, restart.Load(), "Expected restart but did not occur")
			} else {
				ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer cancel()
				r.syncer.OrPanic("syncer").autoRestartIfBehind(ctx, blockPool)
				assert.False(t, restart.Load(), "Unexpected restart")
			}
		})
	}
}

func TestQueryResponder_ServesBlockRequestsWhenBlockSyncDisabled(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_query_responder_test")
	require.NoError(t, err)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 2})
	nodeIDs := network.NodeIDs()

	server := makeReactor(
		ctx,
		t,
		genDoc,
		network.Node(nodeIDs[0]).Router,
		false,
		func() {},
		config.DefaultSelfRemediationConfig(),
	)
	lastCommit := &types.Commit{}
	state, err := server.stateStore.Load()
	require.NoError(t, err)
	for height := int64(1); height <= 3; height++ {
		block, blockID, partSet, seenCommit := makeNextBlock(ctx, t, state, privVals[0], height, lastCommit)
		state, err = server.syncer.OrPanic("syncer should be configured in tests").blockExec.ApplyBlock(ctx, state, blockID, block, nil)
		require.NoError(t, err)
		server.store.SaveBlock(block, partSet, seenCommit)
		lastCommit = seenCommit
	}
	require.NoError(t, server.Start(ctx))
	t.Cleanup(server.Wait)

	client := p2p.TestMakeChannelNoCleanup(t, network.Node(nodeIDs[1]), GetChannelDescriptor())
	network.Start(t)

	client.Send(wrap(&pb.BlockRequest{Height: 2}), nodeIDs[0])
	for range 2 {
		msg, err := client.Recv(ctx)
		require.NoError(t, err)
		if blockResponse, ok := msg.Message.Sum.(*pb.Message_BlockResponse); ok {
			require.Equal(t, int64(2), blockResponse.BlockResponse.GetBlock().Header.Height)
			require.Equal(t, nodeIDs[0], msg.From)
			return
		}
	}
	t.Fatal("did not receive block response")
}

func TestQueryResponder_ServesStatusRequestsWhenBlockSyncDisabled(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_query_status_test")
	require.NoError(t, err)

	valSet, _ := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: 2})
	nodeIDs := network.NodeIDs()

	server := makeReactor(
		ctx,
		t,
		genDoc,
		network.Node(nodeIDs[0]).Router,
		false,
		func() {},
		config.DefaultSelfRemediationConfig(),
	)
	require.NoError(t, server.Start(ctx))
	t.Cleanup(server.Wait)

	client := p2p.TestMakeChannelNoCleanup(t, network.Node(nodeIDs[1]), GetChannelDescriptor())
	network.Start(t)

	client.Send(wrap(&pb.StatusRequest{}), nodeIDs[0])
	msg, err := client.Recv(ctx)
	require.NoError(t, err)

	statusResponse, ok := msg.Message.Sum.(*pb.Message_StatusResponse)
	require.True(t, ok)
	require.Equal(t, server.store.Base(), statusResponse.StatusResponse.GetBase())
	require.Equal(t, server.store.Height(), statusResponse.StatusResponse.GetHeight())
	require.Equal(t, nodeIDs[0], msg.From)
}
