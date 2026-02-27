package statesync

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/mock"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	smmocks "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/statesync/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/light/provider"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var m = PrometheusMetrics(config.TestConfig().Instrumentation.Namespace)

type reactorTestSuite struct {
	network *p2p.TestNetwork
	node    *p2p.TestNode
	reactor *Reactor

	conn          *testStatesyncApp
	stateProvider *mocks.StateProvider

	stateStore *smmocks.Store
	blockStore *store.BlockStore
}

func setup(
	t *testing.T,
	conn *testStatesyncApp,
	stateProvider *mocks.StateProvider,
	setSyncer bool,
) *reactorTestSuite {
	t.Helper()

	if conn == nil {
		conn = newTestStatesyncApp()
	}

	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{
		NumNodes: 1,
		NodeOpts: p2p.TestNodeOptions{
			MaxPeers:     utils.Some(100),
			MaxConnected: utils.Some(100),
		},
	})
	stateStore := &smmocks.Store{}
	blockStore := store.NewBlockStore(dbm.NewMemDB())

	cfg := config.DefaultStateSyncConfig()
	cfg.LightBlockResponseTimeout = 100 * time.Millisecond

	logger, _ := log.NewDefaultLogger("plain", "debug")

	n := network.Nodes()[0]
	reactor, err := NewReactor(
		factory.DefaultTestChainID,
		1,
		*cfg,
		logger.With("component", "reactor"),
		conn,
		n.Router,
		stateStore,
		blockStore,
		"",
		m,
		nil,   // eventbus can be nil
		nil,   // post-sync-hook
		false, // run Sync during Start()
		func() {},
		config.DefaultSelfRemediationConfig(),
	)
	require.NoError(t, err)

	if setSyncer {
		reactor.syncer = &syncer{
			logger:        logger,
			stateProvider: stateProvider,
			conn:          conn,
			snapshots:     newSnapshotPool(),
			snapshotCh:    reactor.snapshotChannel,
			chunkCh:       reactor.chunkChannel,
			tempDir:       t.TempDir(),
			fetchers:      cfg.Fetchers,
			retryTimeout:  cfg.ChunkRequestTimeout,
			metrics:       reactor.metrics,
		}
	}

	require.NoError(t, reactor.Start(t.Context()))
	network.Start(t)
	t.Cleanup(reactor.Stop)
	t.Cleanup(leaktest.CheckTimeout(t, 30*time.Second))

	return &reactorTestSuite{
		network:       network,
		node:          n,
		conn:          conn,
		stateProvider: stateProvider,
		stateStore:    stateStore,
		blockStore:    blockStore,
		reactor:       reactor,
	}
}

func orPanic[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func (rts *reactorTestSuite) AddPeer(t *testing.T) *Node {
	testNode := rts.network.MakeNode(t, p2p.TestNodeOptions{
		MaxPeers:     utils.Some(1),
		MaxConnected: utils.Some(1),
	})
	n := &Node{
		TestNode:   testNode,
		snapshotCh: orPanic(p2p.OpenChannel(testNode.Router, GetSnapshotChannelDescriptor())),
		chunkCh:    orPanic(p2p.OpenChannel(testNode.Router, GetChunkChannelDescriptor())),
		blockCh:    orPanic(p2p.OpenChannel(testNode.Router, GetLightBlockChannelDescriptor())),
		paramsCh:   orPanic(p2p.OpenChannel(testNode.Router, GetParamsChannelDescriptor())),
	}
	rts.node.Connect(t.Context(), testNode)
	return n
}

func TestReactor_Sync(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const snapshotHeight = 7
	rts := setup(t, nil, nil, false)
	chain := buildLightBlockChain(ctx, t, 1, 10, time.Now())
	appConn := rts.conn
	appConn.offerSnapshot.Set(func(context.Context, *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ACCEPT}, nil
	})
	appConn.applySnapshotChunk.Set(func(context.Context, *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
		return &abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT}, nil
	})
	appConn.info.Push(mkHandler(&proxy.RequestInfo, &abci.ResponseInfo{
		AppVersion:       testAppVersion,
		LastBlockHeight:  snapshotHeight,
		LastBlockAppHash: chain[snapshotHeight+1].AppHash,
	}))

	// store accepts state and validator sets
	rts.stateStore.On("Bootstrap", mock.AnythingOfType("state.State")).Return(nil)
	rts.stateStore.On("SaveValidatorSets", mock.AnythingOfType("int64"), mock.AnythingOfType("int64"),
		mock.AnythingOfType("*types.ValidatorSet")).Return(nil)

	go func() {
		ticker := time.NewTicker(time.Second)
		for {
			if _, err := utils.Recv(ctx, ticker.C); err != nil {
				return
			}
			n := rts.AddPeer(t)
			go n.handleLightBlockRequests(t, chain, false)
			go n.handleChunkRequests(t, []byte("abc"))
			go n.handleConsensusParamsRequest(t)
			go n.handleSnapshotRequests(t, []snapshot{
				{
					Height: uint64(snapshotHeight),
					Format: 1,
					Chunks: 1,
				},
			})
		}
	}()

	// update the config to use the p2p provider
	rts.reactor.cfg.UseP2P = true
	rts.reactor.cfg.TrustHeight = 1
	rts.reactor.cfg.TrustHash = fmt.Sprintf("%X", chain[1].Hash())
	rts.reactor.cfg.DiscoveryTime = 1 * time.Second

	// Run state sync
	_, err := rts.reactor.Sync(ctx)
	require.NoError(t, err)
}

func TestReactor_ChunkRequest(t *testing.T) {
	testcases := map[string]struct {
		request        *pb.ChunkRequest
		chunk          []byte
		expectResponse *pb.ChunkResponse
	}{
		"chunk is returned": {
			&pb.ChunkRequest{Height: 1, Format: 1, Index: 1},
			[]byte{1, 2, 3},
			&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Chunk: []byte{1, 2, 3}},
		},
		"empty chunk is returned, as empty": {
			&pb.ChunkRequest{Height: 1, Format: 1, Index: 1},
			[]byte{},
			&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Chunk: []byte{}},
		},
		"nil (missing) chunk is returned as missing": {
			&pb.ChunkRequest{Height: 1, Format: 1, Index: 1},
			nil,
			&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Missing: true},
		},
		"invalid request": {
			&pb.ChunkRequest{Height: 1, Format: 1, Index: 1},
			nil,
			&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Missing: true},
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			// mock ABCI connection to return local snapshots
			conn := newTestStatesyncApp()
			expected := &abci.RequestLoadSnapshotChunk{
				Height: tc.request.Height,
				Format: tc.request.Format,
				Chunk:  tc.request.Index,
			}
			conn.loadSnapshotChunk.Push(mkHandler(expected, &abci.ResponseLoadSnapshotChunk{Chunk: tc.chunk}))

			rts := setup(t, conn, nil, false)
			n := rts.AddPeer(t)
			// Send the actual message.
			n.chunkCh.Broadcast(wrap(tc.request))
			m, err := n.chunkCh.Recv(ctx)
			require.NoError(t, err)
			got := m.Message.Sum.(*pb.Message_ChunkResponse).ChunkResponse
			if err := utils.TestDiff(tc.expectResponse, got); err != nil {
				t.Fatal(err)
			}
			conn.AssertExpectations(t)
		})
	}
}

func abciToSSProtoSnapshot(snapshot *abci.Snapshot) *pb.SnapshotsResponse {
	return &pb.SnapshotsResponse{
		Height:   snapshot.Height,
		Format:   snapshot.Format,
		Chunks:   snapshot.Chunks,
		Hash:     snapshot.Hash,
		Metadata: snapshot.Metadata,
	}
}

func TestReactor_SnapshotsRequest(t *testing.T) {
	testcases := map[string]struct {
		snapshots []*abci.Snapshot
	}{
		"no snapshots": {nil},
		">10 unordered snapshots": {
			[]*abci.Snapshot{
				{Height: 1, Format: 2, Chunks: 7, Hash: []byte{1, 2}, Metadata: []byte{1}},
				{Height: 2, Format: 2, Chunks: 7, Hash: []byte{2, 2}, Metadata: []byte{2}},
				{Height: 3, Format: 2, Chunks: 7, Hash: []byte{3, 2}, Metadata: []byte{3}},
				{Height: 1, Format: 1, Chunks: 7, Hash: []byte{1, 1}, Metadata: []byte{4}},
				{Height: 2, Format: 1, Chunks: 7, Hash: []byte{2, 1}, Metadata: []byte{5}},
				{Height: 3, Format: 1, Chunks: 7, Hash: []byte{3, 1}, Metadata: []byte{6}},
				{Height: 1, Format: 4, Chunks: 7, Hash: []byte{1, 4}, Metadata: []byte{7}},
				{Height: 2, Format: 4, Chunks: 7, Hash: []byte{2, 4}, Metadata: []byte{8}},
				{Height: 3, Format: 4, Chunks: 7, Hash: []byte{3, 4}, Metadata: []byte{9}},
				{Height: 1, Format: 3, Chunks: 7, Hash: []byte{1, 3}, Metadata: []byte{10}},
				{Height: 2, Format: 3, Chunks: 7, Hash: []byte{2, 3}, Metadata: []byte{11}},
				{Height: 3, Format: 3, Chunks: 7, Hash: []byte{3, 3}, Metadata: []byte{12}},
			},
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			snapshots := make([]*abci.Snapshot, len(tc.snapshots))
			for i, s := range tc.snapshots {
				snapshots[i] = utils.ProtoClone(s)
			}

			// mock ABCI connection to return local snapshots
			conn := newTestStatesyncApp()
			conn.listSnapshots.Set(mkHandler(&abci.RequestListSnapshots{}, &abci.ResponseListSnapshots{Snapshots: snapshots}))

			rts := setup(t, conn, nil, false)
			n := rts.AddPeer(t)
			// Send the actual message.
			n.snapshotCh.Broadcast(wrap(&pb.SnapshotsRequest{}))

			// Compute the expected answer.
			want := make([]*pb.SnapshotsResponse, len(tc.snapshots))
			for i, snapshot := range tc.snapshots {
				want[i] = abciToSSProtoSnapshot(snapshot)
			}
			less := func(a, b *pb.SnapshotsResponse) int {
				return cmp.Or(
					cmp.Compare(b.Height, a.Height),
					cmp.Compare(b.Format, a.Format),
				)
			}
			slices.SortFunc(want, less)
			if len(want) > recentSnapshots {
				want = want[:recentSnapshots]
			}

			// Receive the actual answer.
			got := make([]*pb.SnapshotsResponse, len(want))
			for i := range want {
				m, err := n.snapshotCh.Recv(ctx)
				require.NoError(t, err)
				got[i] = m.Message.Sum.(*pb.Message_SnapshotsResponse).SnapshotsResponse
			}

			slices.SortFunc(got, less)
			if err := utils.TestDiff(want, got); err != nil {
				t.Fatal(err)
			}
			conn.AssertExpectations(t)
		})
	}
}

func TestReactor_LightBlockResponse(t *testing.T) {
	ctx := t.Context()

	rts := setup(t, nil, nil, false)

	var height int64 = 10
	// generates a random header
	h := factory.MakeHeader(&types.Header{})
	h.Height = height
	blockID := factory.MakeBlockIDWithHash(h.Hash())
	vals, pv := factory.ValidatorSet(ctx, 1, 10)
	vote, err := factory.MakeVote(ctx, pv[0], h.ChainID, 0, h.Height, 0, 2,
		blockID, factory.DefaultTestTime)
	require.NoError(t, err)

	sh := &types.SignedHeader{
		Header: h,
		Commit: &types.Commit{
			Height:  h.Height,
			BlockID: blockID,
			Signatures: []types.CommitSig{
				vote.CommitSig(),
			},
		},
	}

	lb := &types.LightBlock{
		SignedHeader: sh,
		ValidatorSet: vals,
	}

	require.NoError(t, rts.blockStore.SaveSignedHeader(sh, blockID))

	rts.stateStore.On("LoadValidators", height).Return(vals, nil)
	n := rts.AddPeer(t)
	n.blockCh.Broadcast(wrap(&pb.LightBlockRequest{Height: 10}))
	m, err := n.blockCh.Recv(ctx)
	require.NoError(t, err)
	res := m.Message.Sum.(*pb.Message_LightBlockResponse).LightBlockResponse
	receivedLB, err := types.LightBlockFromProto(res.LightBlock)
	require.NoError(t, err)
	require.Equal(t, lb, receivedLB)
}

func TestReactor_BlockProviders(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	rts := setup(t, nil, nil, false)
	a := rts.AddPeer(t)
	b := rts.AddPeer(t)

	chain := buildLightBlockChain(ctx, t, 1, 10, time.Now())
	go a.handleLightBlockRequests(t, chain, false)
	go b.handleLightBlockRequests(t, chain, false)

	peers := rts.reactor.peers.All()
	require.Len(t, peers, 2)

	providers := make([]provider.Provider, len(peers))
	for idx, peer := range peers {
		providers[idx] = NewBlockProvider(peer, factory.DefaultTestChainID, rts.reactor.dispatcher)
	}

	wg := sync.WaitGroup{}

	for _, p := range providers {
		wg.Add(1)
		go func(t *testing.T, p provider.Provider) {
			defer wg.Done()
			for height := 2; height < 10; height++ {
				lb, err := p.LightBlock(ctx, int64(height))
				require.NoError(t, err)
				require.NotNil(t, lb)
				require.Equal(t, height, int(lb.Height))
			}
		}(t, p)
	}

	go func() { wg.Wait(); cancel() }()

	select {
	case <-time.After(time.Second):
		// not all of the requests to the dispatcher were responded to
		// within the timeout
		t.Fail()
	case <-ctx.Done():
	}

}

func TestReactor_StateProviderP2P(t *testing.T) {
	ctx := t.Context()

	rts := setup(t, nil, nil, true)
	peerA := rts.AddPeer(t)
	peerB := rts.AddPeer(t)
	peerC := rts.AddPeer(t)
	chain := buildLightBlockChain(ctx, t, 1, 10, time.Now())
	for _, peer := range utils.Slice(peerA, peerB, peerC) {
		go peer.handleLightBlockRequests(t, chain, false)
		go peer.handleConsensusParamsRequest(t)
	}

	rts.reactor.cfg.UseP2P = true
	rts.reactor.cfg.TrustHeight = 1
	rts.reactor.cfg.TrustHash = fmt.Sprintf("%X", chain[1].Hash())

	// Peer registration is asynchronous; wait for a minimum set before
	// initializing the provider to avoid CI flakes under load.
	require.Eventually(t, func() bool {
		return rts.reactor.peers.Len() >= 2
	}, 5*time.Second, 100*time.Millisecond)

	ictx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	func() {
		rts.reactor.mtx.Lock()
		defer rts.reactor.mtx.Unlock()
		err := rts.reactor.initStateProvider(ictx, factory.DefaultTestChainID, 1)
		require.NoError(t, err)
		rts.reactor.syncer.stateProvider = rts.reactor.stateProvider
	}()

	// initStateProvider is expected to block until 2 peers are available.
	// However we need 3 peers to test witness removal.
	require.Eventually(t, func() bool {
		return rts.reactor.peers.Len() == 3
	}, 5*time.Second, 100*time.Millisecond)

	actx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	appHash, err := rts.reactor.stateProvider.AppHash(actx, 5)
	require.NoError(t, err)
	require.Len(t, appHash, 32)

	state, err := rts.reactor.stateProvider.State(actx, 5)
	require.NoError(t, err)
	require.Equal(t, appHash, state.AppHash)
	require.Equal(t, types.DefaultConsensusParams(), &state.ConsensusParams)

	commit, err := rts.reactor.stateProvider.Commit(actx, 5)
	require.NoError(t, err)
	require.Equal(t, commit.BlockID, state.LastBlockID)

	added, err := rts.reactor.syncer.AddSnapshot(peerA.NodeID, &snapshot{
		Height: 1, Format: 2, Chunks: 7, Hash: []byte{1, 2}, Metadata: []byte{1},
	})
	require.NoError(t, err)
	require.True(t, added)

	// verify that the state provider is a p2p provider
	sp := rts.reactor.stateProvider.(*StateProviderP2P)

	// This is not really a list of providers, but rather list of witnesses,
	// which excludes the first provider (which is primary)
	require.Len(t, sp.Providers(), 2)

	t.Log("Disconnect the witness.")
	n0 := types.NodeID(sp.Providers()[0].ID())
	n1 := types.NodeID(sp.Providers()[1].ID())
	rts.network.Node(n0).Router.Stop()

	// removal is async, so we need to wait for the reactor to update
	require.Eventually(t, func() bool {
		return len(sp.Providers()) == 1
	}, 5*time.Second, 100*time.Millisecond)
	require.Equal(t, n1, types.NodeID(sp.Providers()[0].ID()))
}

func TestReactor_Backfill(t *testing.T) {
	// test backfill algorithm with varying failure rates [0, 10]
	failureRates := []int{0, 2, 9}
	for _, failureRate := range failureRates {
		t.Run(fmt.Sprintf("failure rate: %d", failureRate), func(t *testing.T) {
			ctx := t.Context()
			t.Cleanup(leaktest.CheckTimeout(t, 1*time.Minute))
			rts := setup(t, nil, nil, false)

			var (
				startHeight int64 = 20
				stopHeight  int64 = 10
				stopTime          = time.Date(2020, 1, 1, 0, 100, 0, 0, time.UTC)
			)

			var peers []*Node
			for range 10 {
				peers = append(peers, rts.AddPeer(t))
			}
			chain := buildLightBlockChain(ctx, t, stopHeight-1, startHeight+1, stopTime)
			for i, peer := range peers {
				go peer.handleLightBlockRequests(t, chain, i < failureRate)
			}

			trackingHeight := startHeight
			rts.stateStore.On("SaveValidatorSets", mock.AnythingOfType("int64"), mock.AnythingOfType("int64"),
				mock.AnythingOfType("*types.ValidatorSet")).Return(func(lh, uh int64, vals *types.ValidatorSet) error {
				require.Equal(t, trackingHeight, lh)
				require.Equal(t, lh, uh)
				require.GreaterOrEqual(t, lh, stopHeight)
				trackingHeight--
				return nil
			})

			err := rts.reactor.backfill(
				ctx,
				factory.DefaultTestChainID,
				startHeight,
				stopHeight,
				1,
				factory.MakeBlockIDWithHash(chain[startHeight].Header.Hash()),
				stopTime,
			)
			require.NoError(t, err)

			for height := startHeight; height <= stopHeight; height++ {
				blockMeta := rts.blockStore.LoadBlockMeta(height)
				require.NotNil(t, blockMeta)
			}

			require.Nil(t, rts.blockStore.LoadBlockMeta(stopHeight-1))
			require.Nil(t, rts.blockStore.LoadBlockMeta(startHeight+1))

			require.Equal(t, startHeight-stopHeight+1, rts.reactor.backfilledBlocks)
			require.Equal(t, startHeight-stopHeight+1, rts.reactor.backfillBlockTotal)
			require.Equal(t, rts.reactor.backfilledBlocks, rts.reactor.BackFilledBlocks())
			require.Equal(t, rts.reactor.backfillBlockTotal, rts.reactor.BackFillBlocksTotal())
		})
	}
}

func buildLightBlockChain(ctx context.Context, t *testing.T, fromHeight, toHeight int64, startTime time.Time) map[int64]*types.LightBlock {
	t.Helper()
	chain := make(map[int64]*types.LightBlock, toHeight-fromHeight)
	lastBlockID := factory.MakeBlockID()
	blockTime := startTime.Add(time.Duration(fromHeight-toHeight) * time.Minute)
	vals, pv := factory.ValidatorSet(ctx, 3, 10)
	for height := fromHeight; height < toHeight; height++ {
		vals, pv, chain[height] = mockLB(ctx, height, blockTime, lastBlockID, vals, pv)
		lastBlockID = factory.MakeBlockIDWithHash(chain[height].Header.Hash())
		blockTime = blockTime.Add(1 * time.Minute)
	}
	return chain
}

type Node struct {
	*p2p.TestNode
	snapshotCh *p2p.Channel[*pb.Message]
	chunkCh    *p2p.Channel[*pb.Message]
	blockCh    *p2p.Channel[*pb.Message]
	paramsCh   *p2p.Channel[*pb.Message]
}

func (n *Node) handleLightBlockRequests(
	t *testing.T,
	chain map[int64]*types.LightBlock,
	shouldFail bool,
) {
	ctx := t.Context()
	errorCount := 0
	for requests := 0; ; requests++ {
		m, err := n.blockCh.Recv(ctx)
		if err != nil {
			return
		}
		wmsg, ok := m.Message.Sum.(*pb.Message_LightBlockRequest)
		if !ok {
			continue
		}
		msg := wmsg.LightBlockRequest
		if !shouldFail {
			lb, err := chain[int64(msg.Height)].ToProto()
			require.NoError(t, err)
			n.blockCh.Send(wrap(&pb.LightBlockResponse{LightBlock: lb}), m.From)
		} else {
			switch errorCount % 3 {
			case 0: // send a different block
				vals, pv := factory.ValidatorSet(ctx, 3, 10)
				_, _, lb := mockLB(ctx, int64(msg.Height), factory.DefaultTestTime, factory.MakeBlockID(), vals, pv)
				differntLB, err := lb.ToProto()
				if err != nil {
					panic(err)
				}
				n.blockCh.Send(wrap(&pb.LightBlockResponse{LightBlock: differntLB}), m.From)
			case 1: // send nil block i.e. pretend we don't have it
				n.blockCh.Send(wrap(&pb.LightBlockResponse{LightBlock: nil}), m.From)
			case 2: // don't do anything
			}
			errorCount++
		}
	}
}

func (n *Node) handleConsensusParamsRequest(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	params := types.DefaultConsensusParams()
	paramsProto := params.ToProto()
	for {
		m, err := n.paramsCh.Recv(ctx)
		if err != nil {
			return
		}
		msg := m.Message.Sum.(*pb.Message_ParamsRequest).ParamsRequest
		n.paramsCh.Send(wrap(&pb.ParamsResponse{
			Height:          msg.Height,
			ConsensusParams: paramsProto,
		}), m.From)
	}
}

func (n *Node) handleSnapshotRequests(t *testing.T, snapshots []snapshot) {
	t.Helper()
	ctx := t.Context()
	for {
		m, err := n.snapshotCh.Recv(ctx)
		if err != nil {
			return
		}
		_ = m.Message.Sum.(*pb.Message_SnapshotsRequest)
		for _, snapshot := range snapshots {
			n.snapshotCh.Send(wrap(&pb.SnapshotsResponse{
				Height:   snapshot.Height,
				Format:   snapshot.Format,
				Chunks:   snapshot.Chunks,
				Hash:     snapshot.Hash,
				Metadata: snapshot.Metadata,
			}), m.From)
		}
	}
}

func (n *Node) handleChunkRequests(t *testing.T, chunk []byte) {
	t.Helper()
	ctx := t.Context()
	for {
		m, err := n.chunkCh.Recv(ctx)
		if err != nil {
			return
		}
		msg := m.Message.Sum.(*pb.Message_ChunkRequest).ChunkRequest
		n.chunkCh.Send(wrap(&pb.ChunkResponse{
			Height:  msg.Height,
			Format:  msg.Format,
			Index:   msg.Index,
			Chunk:   chunk,
			Missing: false,
		}), m.From)
	}
}
