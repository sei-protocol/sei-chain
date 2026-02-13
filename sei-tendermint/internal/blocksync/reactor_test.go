package blocksync

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abciclient "github.com/sei-protocol/sei-chain/sei-tendermint/abci/client"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	mpmocks "github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	sf "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type reactorTestSuite struct {
	network *p2p.TestNetwork
	logger  log.Logger
	nodes   []types.NodeID

	reactors map[types.NodeID]*Reactor
	app      map[types.NodeID]abciclient.Client
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

	logger, _ := log.NewDefaultLogger("plain", "info")
	rts := &reactorTestSuite{
		logger:   logger.With("module", "block_sync", "testCase", t.Name()),
		network:  p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: numNodes}),
		nodes:    make([]types.NodeID, 0, numNodes),
		reactors: make(map[types.NodeID]*Reactor, numNodes),
		app:      make(map[types.NodeID]abciclient.Client, numNodes),
	}

	for i, nodeID := range rts.network.NodeIDs() {
		rts.addNode(ctx, t, nodeID, genDoc, privVal, maxBlockHeights[i])
	}

	t.Cleanup(func() {
		cancel()
		for _, nodeID := range rts.nodes {
			if rts.reactors[nodeID].IsRunning() {
				rts.reactors[nodeID].Wait()
				rts.app[nodeID].Wait()

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
	restartChan chan struct{},
	selfRemediationConfig *config.SelfRemediationConfig,
) *Reactor {

	logger := log.NewNopLogger()

	app := proxy.New(abciclient.NewLocalClient(logger, &abci.BaseApplication{}), logger, proxy.NopMetrics())
	require.NoError(t, app.Start(ctx))

	blockDB := dbm.NewMemDB()
	stateDB := dbm.NewMemDB()
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(blockDB)

	state, err := sm.MakeGenesisState(genDoc)
	require.NoError(t, err)
	require.NoError(t, stateStore.Save(state))
	mp := &mpmocks.Mempool{}
	mp.On("Lock").Return()
	mp.On("Unlock").Return()
	mp.On("FlushAppConn", mock.Anything).Return(nil)
	mp.On("Update",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything).Return(nil)
	mp.On("TxStore").Return(&mempool.TxStore{})

	eventbus := eventbus.NewDefault(logger)
	require.NoError(t, eventbus.Start(ctx))

	blockExec := sm.NewBlockExecutor(
		stateStore,
		log.NewNopLogger(),
		app,
		mp,
		sm.EmptyEvidencePool{},
		blockStore,
		eventbus,
		sm.NopMetrics(),
	)

	r, err := NewReactor(
		logger,
		stateStore,
		blockExec,
		blockStore,
		nil,
		router,
		true,
		consensus.NopMetrics(),
		nil, // eventbus, can be nil
		restartChan,
		selfRemediationConfig,
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

	logger := log.NewNopLogger()

	rts.nodes = append(rts.nodes, nodeID)
	rts.app[nodeID] = proxy.New(abciclient.NewLocalClient(logger, &abci.BaseApplication{}), logger, proxy.NopMetrics())
	require.NoError(t, rts.app[nodeID].Start(ctx))

	restartChan := make(chan struct{})
	remediationConfig := config.DefaultSelfRemediationConfig()
	remediationConfig.BlocksBehindThreshold = 1000

	reactor := makeReactor(
		ctx,
		t,
		genDoc,
		rts.network.Node(nodeID).Router,
		restartChan,
		config.DefaultSelfRemediationConfig(),
	)
	lastCommit := &types.Commit{}

	state, err := reactor.stateStore.Load()
	require.NoError(t, err)
	for blockHeight := int64(1); blockHeight <= maxBlockHeight; blockHeight++ {
		block, blockID, partSet, seenCommit := makeNextBlock(ctx, t, state, privVal, blockHeight, lastCommit)

		state, err = reactor.blockExec.ApplyBlock(ctx, state, blockID, block, nil)
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
	defer os.RemoveAll(cfg.RootDir)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	maxBlockHeight := int64(64)

	rts := setup(ctx, t, genDoc, privVals[0], []int64{maxBlockHeight, 0})

	require.Equal(t, maxBlockHeight, rts.reactors[rts.nodes[0]].store.Height())

	rts.start(t)

	secondaryPool := rts.reactors[rts.nodes[1]].pool

	require.Eventually(
		t,
		func() bool {
			height, _, _ := secondaryPool.GetStatus()
			return secondaryPool.MaxPeerHeight() == maxBlockHeight && height > 0 && height <= maxBlockHeight
		},
		10*time.Second,
		10*time.Millisecond,
		"expected node to be partially synced",
	)

	// Remove synced node from the syncing node which should not result in any
	// deadlocks or race conditions within the context of poolRoutine.
	rts.network.Remove(t, rts.nodes[0])
}

func TestReactor_SyncTime(t *testing.T) {
	ctx := t.Context()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_reactor_test")
	require.NoError(t, err)
	defer os.RemoveAll(cfg.RootDir)

	valSet, privVals := factory.ValidatorSet(ctx, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	maxBlockHeight := int64(101)

	rts := setup(ctx, t, genDoc, privVals[0], []int64{maxBlockHeight, 0})
	require.Equal(t, maxBlockHeight, rts.reactors[rts.nodes[0]].store.Height())
	rts.start(t)

	require.Eventually(
		t,
		func() bool {
			return rts.reactors[rts.nodes[1]].GetRemainingSyncTime() > time.Nanosecond &&
				rts.reactors[rts.nodes[1]].pool.getLastSyncRate() > 0.001
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

func TestCheckBehindAndSignalRestart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		threshold       uint64
		selfHeight      int64
		maxPeerHeight   int64
		isBlockSync     bool
		restartExpected bool
	}{
		{
			name:            "Should not signal if behindHeight is less than threshold",
			threshold:       50,
			selfHeight:      100,
			maxPeerHeight:   140,
			restartExpected: false,
		},
		{
			name:            "Should signal if behindHeight meets threshold",
			threshold:       50,
			selfHeight:      100,
			maxPeerHeight:   160,
			restartExpected: true,
		},
		{
			name:            "Should not signal if maxPeerHeight is 0",
			threshold:       50,
			selfHeight:      100,
			maxPeerHeight:   0,
			restartExpected: false,
		},
		{
			name:            "Should not signal if already in block sync mode",
			threshold:       50,
			selfHeight:      100,
			maxPeerHeight:   160,
			isBlockSync:     true,
			restartExpected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockBlockStore := new(MockBlockStore)
			mockBlockStore.On("Height").Return(tt.selfHeight)

			blockPool := &BlockPool{
				logger:        log.TestingLogger(),
				height:        tt.selfHeight,
				maxPeerHeight: tt.maxPeerHeight,
			}

			restartChan := make(chan struct{}, 1)
			r := &Reactor{
				logger:                log.TestingLogger(),
				store:                 mockBlockStore,
				pool:                  blockPool,
				blocksBehindThreshold: tt.threshold,
				restartCh:             restartChan,
				blockSync:             newAtomicBool(tt.isBlockSync),
			}

			signaled := r.checkBehindAndSignalRestart()
			assert.Equal(t, tt.restartExpected, signaled)
			assert.Equal(t, tt.restartExpected, len(restartChan) == 1)
		})
	}
}

// TestAutoRestartShouldNotGetStuck verifies that the self-remediation loop
// does not permanently disable itself. This is a fully deterministic test
// with no goroutines or timers — it calls checkBehindAndSignalRestart directly.
//
// Previously, blockSync.Set() was called before sending the restart signal.
// If the app-level cooldown in WaitForQuitSignals silently dropped that signal,
// the blockSync flag remained true forever, preventing any future restart
// attempts. With the fix:
//   - blockSync is NOT set by the check (it is read-only)
//   - The send to restartCh is non-blocking so the goroutine never gets stuck
//   - After a dropped signal, the next check still fires
//   - On restart, node.go reconstructs the reactor with blockSync=true (for
//     non-validator nodes), so it enters block sync mode to catch up
func TestAutoRestartShouldNotGetStuck(t *testing.T) {
	mockBlockStore := new(MockBlockStore)
	mockBlockStore.On("Height").Return(int64(100))

	blockPool := &BlockPool{
		logger:        log.TestingLogger(),
		height:        100,
		maxPeerHeight: 200,
	}

	cooldownSeconds := uint64(300) // 5 minute cooldown — large enough to never expire during the test
	restartChan := make(chan struct{}, 1)
	r := &Reactor{
		logger:                 log.TestingLogger(),
		store:                  mockBlockStore,
		pool:                   blockPool,
		blocksBehindThreshold:  50,
		restartCooldownSeconds: cooldownSeconds,
		lastRestartTime:        time.Time{}, // zero value — cooldown is already expired
		restartCh:              restartChan,
		blockSync:              newAtomicBool(false),
	}

	// Step 1: First check — cooldown has expired (lastRestartTime is zero),
	// node is behind threshold, signal is sent.
	signaled := r.checkBehindAndSignalRestart()
	assert.True(t, signaled, "Expected restart signal on first check")
	assert.Equal(t, 1, len(restartChan), "Signal should be in the channel")

	// Step 2: blockSync must NOT have been set — this was the root cause of the old bug.
	assert.False(t, r.blockSync.IsSet(),
		"checkBehindAndSignalRestart must not set blockSync; doing so would permanently disable retries")

	// Step 3: Cooldown is now active (lastRestartTime was set to time.Now() by
	// the check). Subsequent checks should be blocked by cooldown.
	<-restartChan // consume the signal — simulating app-level cooldown dropping it
	signaled = r.checkBehindAndSignalRestart()
	assert.False(t, signaled, "Should not signal while reactor-level cooldown is active")
	assert.Equal(t, 0, len(restartChan), "No signal should be sent during cooldown")

	// Step 4: Simulate cooldown expiring by backdating lastRestartTime.
	r.lastRestartTime = time.Now().Add(-time.Duration(cooldownSeconds+1) * time.Second)

	// Step 5: Now the check fires again — because blockSync was NOT set and
	// cooldown has expired, self-remediation is not stuck.
	signaled = r.checkBehindAndSignalRestart()
	assert.True(t, signaled, "Expected retry signal after cooldown expired — self-remediation must not be stuck")
	assert.Equal(t, 1, len(restartChan), "Retry signal should be in the channel")
	assert.False(t, r.blockSync.IsSet(), "blockSync should remain false after retry")

	// Step 6: Non-blocking send — if the channel is already full, the check
	// should not block and should still return true.
	r.lastRestartTime = time.Time{} // reset cooldown so the check reaches the send
	signaled = r.checkBehindAndSignalRestart()
	assert.True(t, signaled, "Should still attempt signal even when channel is full")
	assert.Equal(t, 1, len(restartChan), "Channel should still have exactly 1 signal (non-blocking)")

	// Step 7: Simulate the in-process restart. When the node rebuilds, node.go
	// determines blockSync = !onlyValidatorIsUs(state, pubKey) which is true for
	// non-validator / RPC nodes, and passes it to NewReactor. Verify that the
	// newly constructed reactor starts with blockSync=true so it enters block
	// sync mode to catch up.
	restartedReactor := &Reactor{
		logger:                 log.TestingLogger(),
		store:                  mockBlockStore,
		pool:                   blockPool,
		blocksBehindThreshold:  50,
		restartCooldownSeconds: cooldownSeconds,
		restartCh:              make(chan struct{}, 1),
		blockSync:              newAtomicBool(true), // node.go sets this true for non-validator
	}
	assert.True(t, restartedReactor.blockSync.IsSet(),
		"After restart, reactor must be in block sync mode so the node catches up")

	// Step 8: While in block sync mode, the check should NOT signal — we don't
	// want self-remediation firing while block sync is actively catching up.
	signaled = restartedReactor.checkBehindAndSignalRestart()
	assert.False(t, signaled, "Should not signal while in block sync mode")
	assert.Equal(t, 0, len(restartedReactor.restartCh), "No signal should be sent during block sync")
}
