package blocksync

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tendermint/tendermint/internal/mempool"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abciclient "github.com/tendermint/tendermint/abci/client"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/internal/consensus"
	"github.com/tendermint/tendermint/internal/eventbus"
	mpmocks "github.com/tendermint/tendermint/internal/mempool/mocks"
	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/internal/proxy"
	sm "github.com/tendermint/tendermint/internal/state"
	sf "github.com/tendermint/tendermint/internal/state/test/factory"
	"github.com/tendermint/tendermint/internal/store"
	"github.com/tendermint/tendermint/internal/test/factory"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
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
				logger:        log.TestingLogger(),
				height:        tt.selfHeight,
				maxPeerHeight: tt.maxPeerHeight,
			}

			restartChan := make(chan struct{}, 1)
			r := &Reactor{
				logger:                    log.TestingLogger(),
				store:                     mockBlockStore,
				pool:                      blockPool,
				blocksBehindThreshold:     tt.blocksBehindThreshold,
				blocksBehindCheckInterval: tt.blocksBehindCheckInterval,
				restartCh:                 restartChan,
				blockSync:                 newAtomicBool(tt.isBlockSync),
			}

			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()

			go r.autoRestartIfBehind(ctx)

			select {
			case <-restartChan:
				assert.True(t, tt.restartExpected, "Unexpected restart")
			case <-time.After(50 * time.Millisecond):
				assert.False(t, tt.restartExpected, "Expected restart but did not occur")
			}
		})
	}
}
