package statesync

import (
	"context"
	"fmt"
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
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/light/provider"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var m = Global

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
	proxyConn := proxy.New(conn)

	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{
		NumNodes: 1,
		NodeOpts: p2p.TestNodeOptions{
			MaxConnected: utils.Some(100),
		},
	})
	stateStore := &smmocks.Store{}
	blockStore := store.NewBlockStore(dbm.NewMemDB())

	cfg := config.DefaultStateSyncConfig()
	cfg.LightBlockResponseTimeout = 100 * time.Millisecond

	n := network.Nodes()[0]
	reactor, err := NewReactor(
		factory.DefaultTestChainID,
		1,
		*cfg,
		proxyConn,
		n.Router,
		stateStore,
		blockStore,
		"",
		nil,   // eventbus can be nil
		nil,   // post-sync-hook
		false, // run Sync during Start()
		func() {},
		config.DefaultSelfRemediationConfig(),
	)
	require.NoError(t, err)

	if setSyncer {
		reactor.syncer = &syncer{
			stateProvider: stateProvider,
			conn:          proxyConn,
			snapshots:     newSnapshotPool(),
			snapshotCh:    reactor.snapshotChannel,
			chunkCh:       reactor.chunkChannel,
			tempDir:       t.TempDir(),
			fetchers:      cfg.Fetchers,
			retryTimeout:  cfg.ChunkRequestTimeout,
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

func (rts *reactorTestSuite) AddPeerWithoutWaiting(ctx context.Context, t *testing.T) (*Node, error) {
	testNode := rts.network.MakeNode(t, p2p.TestNodeOptions{
		MaxConnected: utils.Some(1),
	})
	n := &Node{
		TestNode:   testNode,
		snapshotCh: orPanic(p2p.OpenChannel(testNode.Router, GetSnapshotChannelDescriptor())),
		chunkCh:    orPanic(p2p.OpenChannel(testNode.Router, GetChunkChannelDescriptor())),
		blockCh:    orPanic(p2p.OpenChannel(testNode.Router, GetLightBlockChannelDescriptor())),
		paramsCh:   orPanic(p2p.OpenChannel(testNode.Router, GetParamsChannelDescriptor())),
	}
	if err := testNode.Connect(ctx, rts.node); err != nil {
		return nil, err
	}
	return n, nil
}

func (rts *reactorTestSuite) AddPeer(ctx context.Context, t *testing.T) (*Node, error) {
	n, err := rts.AddPeerWithoutWaiting(ctx, t)
	if err != nil {
		return nil, err
	}
	// Peer registration in the reactor is asynchronous, so block until this peer
	// is visible before returning to callers that may assert on peer counts.
	if err := rts.reactor.peers.WaitUntilContains(ctx, n.TestNode.NodeID); err != nil {
		return nil, err
	}
	return n, nil
}

func TestReactor_Sync(t *testing.T) {
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
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
		appConn.info.Push(mkConst(&abci.ResponseInfo{
			AppVersion:       testAppVersion,
			LastBlockHeight:  snapshotHeight,
			LastBlockAppHash: chain[snapshotHeight+1].AppHash,
		}))

		// store accepts state and validator sets
		rts.stateStore.On("Bootstrap", mock.AnythingOfType("state.State")).Return(nil)
		rts.stateStore.On("SaveValidatorSets", mock.AnythingOfType("int64"), mock.AnythingOfType("int64"),
			mock.AnythingOfType("*types.ValidatorSet")).Return(nil)

		s.SpawnBg(func() error {
			return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
				for {
					if err := utils.Sleep(ctx, time.Second); err != nil {
						return err
					}
					n, err := rts.AddPeerWithoutWaiting(ctx, t)
					if err != nil {
						return fmt.Errorf("rtx.AddPeerWithoutWaiting(): %w", err)
					}
					s.SpawnBg(func() error { return n.handleLightBlockRequests(ctx, chain, false) })
					s.SpawnBg(func() error { return n.handleChunkRequests(ctx, []byte("abc")) })
					s.SpawnBg(func() error { return n.handleConsensusParamsRequest(ctx) })
					s.SpawnBg(func() error {
						return n.handleSnapshotRequests(ctx, []snapshot{
							{
								Height: uint64(snapshotHeight),
								Format: 1,
								Chunks: 1,
							},
						})
					})
				}
			}))
		})

		// update the config to use the p2p provider
		rts.reactor.cfg.UseP2P = true
		rts.reactor.cfg.TrustHeight = 1
		rts.reactor.cfg.TrustHash = fmt.Sprintf("%X", chain[1].Hash())
		rts.reactor.cfg.DiscoveryTime = 1 * time.Second

		// Run state sync
		_, err := rts.reactor.Sync(ctx)
		return err
	})
	require.NoError(t, err)
}

func TestReactor_ChunkRequest(t *testing.T) {
	ctx := t.Context()

	conn := newTestStatesyncApp()
	called := false
	conn.loadSnapshotChunk.Set(func(context.Context, *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
		called = true
		return &abci.ResponseLoadSnapshotChunk{Chunk: []byte{1, 2, 3}}, nil
	})

	rts := setup(t, conn, nil, false)
	n := utils.OrPanic1(rts.AddPeer(ctx, t))
	n.chunkCh.Broadcast(wrap(&pb.ChunkRequest{Height: 1, Format: 1, Index: 1}))
	m, err := n.chunkCh.Recv(ctx)
	require.NoError(t, err)
	got := m.Message.Sum.(*pb.Message_ChunkResponse).ChunkResponse
	if err := utils.TestDiff(&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Missing: true}, got); err != nil {
		t.Fatal(err)
	}
	require.False(t, called)
}

func TestReactor_ChunkRequestServedWhenEnabled(t *testing.T) {
	ctx := t.Context()

	conn := newTestStatesyncApp()
	conn.loadSnapshotChunk.Set(func(context.Context, *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
		return &abci.ResponseLoadSnapshotChunk{Chunk: []byte{1, 2, 3}}, nil
	})

	rts := setup(t, conn, nil, false)
	rts.reactor.SetServeSnapshotsAndBlocks(true)
	n := utils.OrPanic1(rts.AddPeer(ctx, t))
	n.chunkCh.Broadcast(wrap(&pb.ChunkRequest{Height: 1, Format: 1, Index: 1}))
	m, err := n.chunkCh.Recv(ctx)
	require.NoError(t, err)
	got := m.Message.Sum.(*pb.Message_ChunkResponse).ChunkResponse
	if err := utils.TestDiff(&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Chunk: []byte{1, 2, 3}}, got); err != nil {
		t.Fatal(err)
	}
}

func TestReactor_SnapshotsRequest(t *testing.T) {
	ctx := t.Context()

	conn := newTestStatesyncApp()
	called := false
	conn.listSnapshots.Set(func(context.Context, *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
		called = true
		return &abci.ResponseListSnapshots{Snapshots: []*abci.Snapshot{{Height: 3, Format: 1, Chunks: 1}}}, nil
	})

	rts := setup(t, conn, nil, false)
	err := rts.reactor.handleSnapshotMessage(ctx, p2p.RecvMsg[*pb.Message]{
		Message: wrap(&pb.SnapshotsRequest{}),
	})
	require.NoError(t, err)
	require.False(t, called)
}

func TestReactor_SnapshotsRequestServedWhenEnabled(t *testing.T) {
	ctx := t.Context()

	conn := newTestStatesyncApp()
	conn.listSnapshots.Set(mkHandler(&abci.RequestListSnapshots{}, &abci.ResponseListSnapshots{
		Snapshots: []*abci.Snapshot{{
			Height: 3, Format: 1, Chunks: 1, Hash: []byte{1}, Metadata: []byte{2},
		}},
	}))

	rts := setup(t, conn, nil, false)
	rts.reactor.SetServeSnapshotsAndBlocks(true)
	n := utils.OrPanic1(rts.AddPeer(ctx, t))
	n.snapshotCh.Broadcast(wrap(&pb.SnapshotsRequest{}))
	m, err := n.snapshotCh.Recv(ctx)
	require.NoError(t, err)
	got := m.Message.Sum.(*pb.Message_SnapshotsResponse).SnapshotsResponse
	if err := utils.TestDiff(&pb.SnapshotsResponse{
		Height: 3, Format: 1, Chunks: 1, Hash: []byte{1}, Metadata: []byte{2},
	}, got); err != nil {
		t.Fatal(err)
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
	n := utils.OrPanic1(rts.AddPeer(ctx, t))
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
	a := utils.OrPanic1(rts.AddPeer(ctx, t))
	b := utils.OrPanic1(rts.AddPeer(ctx, t))

	chain := buildLightBlockChain(ctx, t, 1, 10, time.Now())
	go a.handleLightBlockRequests(t.Context(), chain, false)
	go b.handleLightBlockRequests(t.Context(), chain, false)

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
	peerA := utils.OrPanic1(rts.AddPeer(ctx, t))
	peerB := utils.OrPanic1(rts.AddPeer(ctx, t))
	peerC := utils.OrPanic1(rts.AddPeer(ctx, t))
	chain := buildLightBlockChain(ctx, t, 1, 10, time.Now())
	for _, peer := range utils.Slice(peerA, peerB, peerC) {
		go peer.handleLightBlockRequests(t.Context(), chain, false)
		go peer.handleConsensusParamsRequest(t.Context())
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
			rts := setup(t, nil, nil, false)

			var (
				startHeight int64 = 20
				stopHeight  int64 = 10
				stopTime          = time.Date(2020, 1, 1, 0, 100, 0, 0, time.UTC)
			)

			var peers []*Node
			for range 10 {
				peers = append(peers, utils.OrPanic1(rts.AddPeer(ctx, t)))
			}
			chain := buildLightBlockChain(ctx, t, stopHeight-1, startHeight+1, stopTime)
			for i, peer := range peers {
				go peer.handleLightBlockRequests(t.Context(), chain, i < failureRate)
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
	ctx context.Context,
	chain map[int64]*types.LightBlock,
	shouldFail bool,
) error {
	errorCount := 0
	for requests := 0; ; requests++ {
		m, err := n.blockCh.Recv(ctx)
		if err != nil {
			return err
		}
		wmsg, ok := m.Message.Sum.(*pb.Message_LightBlockRequest)
		if !ok {
			continue
		}
		msg := wmsg.LightBlockRequest
		if !shouldFail {
			lb := utils.OrPanic1(chain[int64(msg.Height)].ToProto())
			n.blockCh.Send(wrap(&pb.LightBlockResponse{LightBlock: lb}), m.From)
		} else {
			switch errorCount % 3 {
			case 0: // send a different block
				vals, pv := factory.ValidatorSet(ctx, 3, 10)
				_, _, lb := mockLB(ctx, int64(msg.Height), factory.DefaultTestTime, factory.MakeBlockID(), vals, pv)
				differntLB := utils.OrPanic1(lb.ToProto())
				n.blockCh.Send(wrap(&pb.LightBlockResponse{LightBlock: differntLB}), m.From)
			case 1: // send nil block i.e. pretend we don't have it
				n.blockCh.Send(wrap(&pb.LightBlockResponse{LightBlock: nil}), m.From)
			case 2: // don't do anything
			}
			errorCount++
		}
	}
}

func (n *Node) handleConsensusParamsRequest(ctx context.Context) error {
	params := types.DefaultConsensusParams()
	paramsProto := params.ToProto()
	for {
		m, err := n.paramsCh.Recv(ctx)
		if err != nil {
			return err
		}
		msg := m.Message.Sum.(*pb.Message_ParamsRequest).ParamsRequest
		n.paramsCh.Send(wrap(&pb.ParamsResponse{
			Height:          msg.Height,
			ConsensusParams: paramsProto,
		}), m.From)
	}
}

func (n *Node) handleSnapshotRequests(ctx context.Context, snapshots []snapshot) error {
	for {
		m, err := n.snapshotCh.Recv(ctx)
		if err != nil {
			return err
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

func (n *Node) handleChunkRequests(ctx context.Context, chunk []byte) error {
	for {
		m, err := n.chunkCh.Recv(ctx)
		if err != nil {
			return err
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
