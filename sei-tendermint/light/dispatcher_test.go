package light

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/assert"

	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/internal/test/factory"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
	ssproto "github.com/tendermint/tendermint/proto/tendermint/statesync"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

func GetLightBlockChannelDescriptor() p2p.ChannelDescriptor {
	return p2p.ChannelDescriptor{
		ID:                  0x17,
		MessageType:         new(ssproto.Message),
		Priority:            5,
		SendQueueCapacity:   10,
		RecvMessageCapacity: 10000,
		RecvBufferCapacity:  128,
		Name:                "light-block",
	}
}

type testSuite struct {
	network    *p2p.TestNetwork
	node       *p2p.TestNode
	dispatcher *Dispatcher
}

func setup(t *testing.T) *testSuite {
	t.Helper()

	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{
		NumNodes: 1,
		NodeOpts: p2p.TestNodeOptions{
			MaxPeers:     utils.Some(100),
			MaxConnected: utils.Some(100),
		},
	})
	n := network.Nodes()[0]
	ch, err := n.Router.OpenChannel(GetLightBlockChannelDescriptor())
	if err != nil {
		panic(err)
	}
	network.Start(t)
	t.Cleanup(leaktest.Check(t))

	return &testSuite{
		network: network,
		node:    n,
		dispatcher: NewDispatcher(ch, func(height uint64) proto.Message {
			return &ssproto.LightBlockRequest{
				Height: height,
			}
		}),
	}
}

type Node struct {
	*p2p.TestNode
	blockCh *p2p.Channel
}

func (ts *testSuite) AddPeer(t *testing.T) *Node {
	testNode := ts.network.MakeNode(t, p2p.TestNodeOptions{
		MaxPeers:     utils.Some(1),
		MaxConnected: utils.Some(1),
	})
	blockCh, err := testNode.Router.OpenChannel(GetLightBlockChannelDescriptor())
	if err != nil {
		panic(err)
	}
	n := &Node{
		TestNode: testNode,
		blockCh:  blockCh,
	}
	ts.node.Connect(t.Context(), testNode)
	return n
}

// handleRequests is a helper function usually run in a separate go routine to
// imitate the expected responses of the reactor wired to the dispatcher
func (n *Node) handleRequests(ctx context.Context, d *Dispatcher) error {
	for {
		req, err := n.blockCh.Recv(ctx)
		if err != nil {
			return nil
		}
		height := req.Message.(*ssproto.LightBlockRequest).Height
		resp := mockLBResp(ctx, n.NodeID, int64(height), time.Now())
		block, _ := resp.block.ToProto()
		if err := d.Respond(ctx, block, n.NodeID); err != nil {
			return fmt.Errorf("d.Respond(): %w", err)
		}
	}
}

func TestDispatcherBasic(t *testing.T) {
	const numPeers = 5
	ctx := t.Context()
	ts := setup(t)

	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		var peers []*Node
		for range numPeers {
			n := ts.AddPeer(t)
			s.SpawnBg(func() error { return n.handleRequests(ctx, ts.dispatcher) })
			peers = append(peers, n)
		}

		// make a bunch of async requests and require that the correct responses are
		// given
		for i, peer := range peers {
			s.Spawn(func() error {
				height := int64(i + 1)
				lb, err := ts.dispatcher.LightBlock(ctx, height, peer.NodeID)
				if err != nil {
					return fmt.Errorf("LightBlock(%v): %w", height, err)
				}
				if lb.Height != height {
					return fmt.Errorf("expected height %v, got %v", height, lb.Height)
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// assert that all calls were responded to
	assert.Empty(t, ts.dispatcher.calls)
}

func TestDispatcherReturnsNoBlock(t *testing.T) {
	ts := setup(t)
	peer := ts.AddPeer(t)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			for {
				_, err := peer.blockCh.Recv(ctx)
				if err != nil {
					return nil
				}
				if err := ts.dispatcher.Respond(ctx, nil, peer.NodeID); err != nil {
					return fmt.Errorf("d.Respond(): %w", err)
				}
			}
		})
		lb, err := ts.dispatcher.LightBlock(ctx, 1, peer.NodeID)
		if err != nil {
			return fmt.Errorf("LightBlock: %w", err)
		}
		if lb != nil {
			return fmt.Errorf("expected no light block, got %v", lb)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDispatcherTimeOutWaitingOnLightBlock(t *testing.T) {
	ts := setup(t)
	peer := factory.NodeID(t, "a")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := ts.dispatcher.LightBlock(ctx, 1, peer)
	if !errors.Is(err, ErrDisconnected) {
		t.Fatalf("expected context.Canceled error, got %v", err)
	}
}

func TestDispatcherProviders(t *testing.T) {
	ctx := t.Context()
	ts := setup(t)
	var peers []*Node
	for range 5 {
		n := ts.AddPeer(t)
		go n.handleRequests(ctx, ts.dispatcher)
		peers = append(peers, n)
	}
	chainID := "test-chain"
	for _, peer := range peers {
		p := NewBlockProvider(peer.NodeID, chainID, ts.dispatcher)
		assert.Equal(t, string(peer.NodeID), p.String())
		lb, err := p.LightBlock(ctx, 10)
		assert.NoError(t, err)
		assert.NotNil(t, lb)
	}
}

func TestPeerListBasic(t *testing.T) {
	t.Cleanup(leaktest.Check(t))

	ctx := t.Context()

	peerList := NewPeerList()
	assert.Zero(t, peerList.Len())
	numPeers := 10
	peerSet := createPeerSet(numPeers)

	for _, peer := range peerSet {
		peerList.Append(peer)
	}

	for idx, peer := range peerList.All() {
		assert.Equal(t, peer, peerSet[idx])
	}

	assert.Equal(t, numPeers, peerList.Len())

	half := numPeers / 2
	for i := range half {
		assert.Equal(t, peerSet[i], peerList.Pop(ctx))
	}
	assert.Equal(t, half, peerList.Len())

	// removing a peer that doesn't exist should not change the list
	peerList.Remove(types.NodeID("lp"))
	assert.Equal(t, half, peerList.Len())

	// removing a peer that exists should decrease the list size by one
	peerList.Remove(peerSet[half])
	assert.Equal(t, numPeers-half-1, peerList.Len())

	// popping the next peer should work as expected
	assert.Equal(t, peerSet[half+1], peerList.Pop(ctx))
	assert.Equal(t, numPeers-half-2, peerList.Len())

	// append the two peers back
	peerList.Append(peerSet[half])
	peerList.Append(peerSet[half+1])
	assert.Equal(t, half, peerList.Len())
}

func TestPeerListBlocksWhenEmpty(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	peerList := NewPeerList()
	require.Zero(t, peerList.Len())
	doneCh := make(chan struct{})
	ctx := t.Context()
	go func() {
		peerList.Pop(ctx)
		close(doneCh)
	}()
	select {
	case <-doneCh:
		t.Error("empty peer list should not have returned result")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestEmptyPeerListReturnsWhenContextCanceled(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	peerList := NewPeerList()
	require.Zero(t, peerList.Len())
	doneCh := make(chan struct{})

	ctx := t.Context()

	wrapped, cancel := context.WithCancel(ctx)
	go func() {
		peerList.Pop(wrapped)
		close(doneCh)
	}()
	select {
	case <-doneCh:
		t.Error("empty peer list should not have returned result")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()

	select {
	case <-doneCh:
	case <-time.After(100 * time.Millisecond):
		t.Error("peer list should have returned after context canceled")
	}
}

func TestPeerListConcurrent(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	peerList := NewPeerList()
	numPeers := 10

	wg := sync.WaitGroup{}
	// we run a set of goroutines requesting the next peer in the list. As the
	// peer list hasn't been populated each these go routines should block
	for i := 0; i < numPeers/2; i++ {
		go func() {
			_ = peerList.Pop(ctx)
			wg.Done()
		}()
	}

	// now we add the peers to the list, this should allow the previously
	// blocked go routines to unblock
	for _, peer := range createPeerSet(numPeers) {
		wg.Add(1)
		peerList.Append(peer)
	}

	// we request the second half of the peer set
	for i := 0; i < numPeers/2; i++ {
		go func() {
			_ = peerList.Pop(ctx)
			wg.Done()
		}()
	}

	// we use a context with cancel and a separate go routine to wait for all
	// the other goroutines to close.
	go func() { wg.Wait(); cancel() }()

	select {
	case <-time.After(time.Second):
		// not all of the blocked go routines waiting on peers have closed after
		// one second. This likely means the list got blocked.
		t.Failed()
	case <-ctx.Done():
		// there should be no peers remaining
		require.Equal(t, 0, peerList.Len())
	}
}

func TestPeerListRemove(t *testing.T) {
	peerList := NewPeerList()
	numPeers := 10

	peerSet := createPeerSet(numPeers)
	for _, peer := range peerSet {
		peerList.Append(peer)
	}

	for _, peer := range peerSet {
		peerList.Remove(peer)
		for _, p := range peerList.All() {
			require.NotEqual(t, p, peer)
		}
		numPeers--
		require.Equal(t, numPeers, peerList.Len())
	}
}

func createPeerSet(num int) []types.NodeID {
	peers := make([]types.NodeID, num)
	for i := range num {
		peers[i], _ = types.NewNodeID(strings.Repeat(fmt.Sprintf("%d", i), 2*types.NodeIDByteLength))
	}
	return peers
}

const testAppVersion = 9

type lightBlockResponse struct {
	block *types.LightBlock
	peer  types.NodeID
}

func mockLBResp(ctx context.Context, peer types.NodeID, height int64, time time.Time) lightBlockResponse {
	vals, pv := factory.ValidatorSet(ctx, 3, 10)
	_, _, lb := mockLB(ctx, height, time, factory.MakeBlockID(), vals, pv)
	return lightBlockResponse{
		block: lb,
		peer:  peer,
	}
}

func mockLB(ctx context.Context, height int64, time time.Time, lastBlockID types.BlockID,
	currentVals *types.ValidatorSet, currentPrivVals []types.PrivValidator,
) (*types.ValidatorSet, []types.PrivValidator, *types.LightBlock) {
	header := factory.MakeHeader(&types.Header{
		Height:      height,
		LastBlockID: lastBlockID,
		Time:        time,
	})
	header.Version.App = testAppVersion

	nextVals, nextPrivVals := factory.ValidatorSet(ctx, 3, 10)
	header.ValidatorsHash = currentVals.Hash()
	header.NextValidatorsHash = nextVals.Hash()
	header.ConsensusHash = types.DefaultConsensusParams().HashConsensusParams()
	lastBlockID = factory.MakeBlockIDWithHash(header.Hash())
	voteSet := types.NewVoteSet(factory.DefaultTestChainID, height, 0, tmproto.PrecommitType, currentVals)
	commit, err := factory.MakeCommit(ctx, lastBlockID, height, 0, voteSet, currentPrivVals, time)
	if err != nil {
		panic(fmt.Errorf("factory.MakeCommit(): %w", err))
	}
	return nextVals, nextPrivVals, &types.LightBlock{
		SignedHeader: &types.SignedHeader{
			Header: header,
			Commit: commit,
		},
		ValidatorSet: currentVals,
	}
}
