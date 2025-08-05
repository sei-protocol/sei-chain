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
	"github.com/tendermint/tendermint/internal/p2p/p2ptest"
	"github.com/tendermint/tendermint/internal/proxy"
	sm "github.com/tendermint/tendermint/internal/state"
	sf "github.com/tendermint/tendermint/internal/state/test/factory"
	"github.com/tendermint/tendermint/internal/store"
	"github.com/tendermint/tendermint/internal/test/factory"
	"github.com/tendermint/tendermint/libs/log"
	bcproto "github.com/tendermint/tendermint/proto/tendermint/blocksync"
	"github.com/tendermint/tendermint/types"
)

type reactorTestSuite struct {
	network *p2ptest.Network
	logger  log.Logger
	nodes   []types.NodeID

	reactors map[types.NodeID]*Reactor
	app      map[types.NodeID]abciclient.Client

	blockSyncChannels map[types.NodeID]*p2p.Channel
	peerChans         map[types.NodeID]chan p2p.PeerUpdate
	peerUpdates       map[types.NodeID]*p2p.PeerUpdates
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
		logger:            log.NewNopLogger().With("module", "block_sync", "testCase", t.Name()),
		network:           p2ptest.MakeNetwork(ctx, t, p2ptest.NetworkOptions{NumNodes: numNodes}),
		nodes:             make([]types.NodeID, 0, numNodes),
		reactors:          make(map[types.NodeID]*Reactor, numNodes),
		app:               make(map[types.NodeID]abciclient.Client, numNodes),
		blockSyncChannels: make(map[types.NodeID]*p2p.Channel, numNodes),
		peerChans:         make(map[types.NodeID]chan p2p.PeerUpdate, numNodes),
		peerUpdates:       make(map[types.NodeID]*p2p.PeerUpdates, numNodes),
	}

	chDesc := &p2p.ChannelDescriptor{ID: BlockSyncChannel, MessageType: new(bcproto.Message)}
	rts.blockSyncChannels = rts.network.MakeChannelsNoCleanup(ctx, t, chDesc)

	i := 0
	for nodeID := range rts.network.Nodes {
		rts.addNode(ctx, t, nodeID, genDoc, privVal, maxBlockHeights[i])
		rts.reactors[nodeID].SetChannel(rts.blockSyncChannels[nodeID])
		i++
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
	nodeID types.NodeID,
	genDoc *types.GenesisDoc,
	privVal types.PrivValidator,
	channelCreator p2p.ChannelCreator,
	peerEvents p2p.PeerEventSubscriber,
	peerManager *p2p.PeerManager,
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

	return NewReactor(
		logger,
		stateStore,
		blockExec,
		blockStore,
		nil,
		peerEvents,
		peerManager,
		true,
		consensus.NopMetrics(),
		nil, // eventbus, can be nil
		restartChan,
		selfRemediationConfig,
	)
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

	rts.peerChans[nodeID] = make(chan p2p.PeerUpdate)
	rts.peerUpdates[nodeID] = p2p.NewPeerUpdates(rts.peerChans[nodeID], 1)
	rts.network.Nodes[nodeID].PeerManager.Register(ctx, rts.peerUpdates[nodeID])

	chCreator := func(ctx context.Context, chdesc *p2p.ChannelDescriptor) (*p2p.Channel, error) {
		return rts.blockSyncChannels[nodeID], nil
	}

	peerEvents := func(ctx context.Context) *p2p.PeerUpdates { return rts.peerUpdates[nodeID] }
	restartChan := make(chan struct{})
	remediationConfig := config.DefaultSelfRemediationConfig()
	remediationConfig.BlocksBehindThreshold = 1000

	reactor := makeReactor(
		ctx,
		t,
		nodeID,
		genDoc,
		privVal,
		chCreator,
		peerEvents,
		rts.network.Nodes[nodeID].PeerManager,
		restartChan,
		config.DefaultSelfRemediationConfig(),
	)

	reactor.SetChannel(rts.blockSyncChannels[nodeID])
	lastExtCommit := &types.ExtendedCommit{}

	state, err := reactor.stateStore.Load()
	require.NoError(t, err)
	for blockHeight := int64(1); blockHeight <= maxBlockHeight; blockHeight++ {
		block, blockID, partSet, seenExtCommit := makeNextBlock(ctx, t, state, privVal, blockHeight, lastExtCommit)

		state, err = reactor.blockExec.ApplyBlock(ctx, state, blockID, block, nil)
		require.NoError(t, err)

		reactor.store.SaveBlockWithExtendedCommit(block, partSet, seenExtCommit)
		lastExtCommit = seenExtCommit
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
	lc *types.ExtendedCommit) (*types.Block, types.BlockID, *types.PartSet, *types.ExtendedCommit) {

	lastExtCommit := lc.Clone()

	block := sf.MakeBlock(state, height, lastExtCommit.ToCommit())
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
	seenExtCommit := &types.ExtendedCommit{
		Height:             vote.Height,
		Round:              vote.Round,
		BlockID:            blockID,
		ExtendedSignatures: []types.ExtendedCommitSig{vote.ExtendedCommitSig()},
	}
	return block, blockID, partSet, seenExtCommit

}

func (rts *reactorTestSuite) start(ctx context.Context, t *testing.T) {
	t.Helper()
	rts.network.Start(ctx, t)
	require.Len(t,
		rts.network.RandomNode().PeerManager.Peers(),
		len(rts.nodes)-1,
		"network does not have expected number of nodes")
}

func TestReactor_AbruptDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_reactor_test")
	require.NoError(t, err)
	defer os.RemoveAll(cfg.RootDir)

	valSet, privVals := factory.ValidatorSet(ctx, t, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	maxBlockHeight := int64(64)

	rts := setup(ctx, t, genDoc, privVals[0], []int64{maxBlockHeight, 0})

	require.Equal(t, maxBlockHeight, rts.reactors[rts.nodes[0]].store.Height())

	rts.start(ctx, t)

	secondaryPool := rts.reactors[rts.nodes[1]].pool

	require.Eventually(
		t,
		func() bool {
			height, _, _ := secondaryPool.GetStatus()
			return secondaryPool.MaxPeerHeight() > 0 && height > 0 && height < 10
		},
		10*time.Second,
		10*time.Millisecond,
		"expected node to be partially synced",
	)

	// Remove synced node from the syncing node which should not result in any
	// deadlocks or race conditions within the context of poolRoutine.
	rts.peerChans[rts.nodes[1]] <- p2p.PeerUpdate{
		Status: p2p.PeerStatusDown,
		NodeID: rts.nodes[0],
	}
	rts.network.Nodes[rts.nodes[1]].PeerManager.Disconnected(ctx, rts.nodes[0])
}

func TestReactor_SyncTime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.ResetTestRoot(t.TempDir(), "block_sync_reactor_test")
	require.NoError(t, err)
	defer os.RemoveAll(cfg.RootDir)

	valSet, privVals := factory.ValidatorSet(ctx, t, 1, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	maxBlockHeight := int64(101)

	rts := setup(ctx, t, genDoc, privVals[0], []int64{maxBlockHeight, 0})
	require.Equal(t, maxBlockHeight, rts.reactors[rts.nodes[0]].store.Height())
	rts.start(ctx, t)

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

			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
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
