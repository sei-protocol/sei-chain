package pex

import (
	"context"
	"errors"
	//"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/p2p"
	p2pproto "github.com/tendermint/tendermint/proto/tendermint/p2p"
	"github.com/tendermint/tendermint/types"
)

const (
	checkFrequency    = 500 * time.Millisecond
	shortWait         = 5 * time.Second
	longWait          = 20 * time.Second
)

func TestReactorBasic(t *testing.T) {
	ctx := t.Context()
	t.Log("start a network with one mock reactor and one \"real\" reactor")
	testNet := setupNetwork(t, testOptions{
		MockNodes:  1,
		TotalNodes: 2,
	})
	testNet.connectAll(t)
	testNet.start(ctx, t)

	t.Log("assert that the mock node receives a request from the real node")
	testNet.listenForRequest(ctx, t, 1, 0, shortWait)

	t.Log("assert that when a mock node sends a request it receives a response (and the correct one)")
	testNet.sendRequest(t, 0, 1)
	testNet.listenForResponse(ctx, t, 1, 0, shortWait, []p2pproto.PexAddress(nil))
}

func TestReactorConnectFullNetwork(t *testing.T) {
	ctx := t.Context()

	testNet := setupNetwork(t, testOptions{
		TotalNodes: 4,
	})

	// make every node be only connected with one other node (it actually ends up
	// being two because of two way connections but oh well)
	testNet.seedAddrs(t)
	testNet.start(ctx, t)

	t.Logf("assert that all nodes add each other in the network")
	for idx := 0; idx < len(testNet.nodes); idx++ {
		testNet.requireNumberOfPeers(t, idx, len(testNet.nodes)-1, longWait)
	}
}

func TestReactorSendsRequestsTooOften(t *testing.T) {
	ctx := t.Context()
	testNet := setupNetwork(t, testOptions{
		MockNodes:  1,
		TotalNodes: 2,
	})
	testNet.connectAll(t)
	testNet.start(ctx, t)

	n0,n1 := testNet.checkNodePair(t, 0, 1)
	ch := testNet.pexChannels[n0]
	t.Log("Send request.")
	ch.Send(&p2pproto.PexRequest{}, n1)
	t.Log("Await response.")
	for {
		m,err := ch.Recv(ctx)
		require.NoError(t, err)
		require.Equal(t, n1, m.From)
		_,ok := m.Message.(*p2pproto.PexResponse)
		if !ok { continue }
		break
	}
	t.Log("Send again.")
	ch.Send(&p2pproto.PexRequest{}, n1)
	t.Log("n0 should force disconnect.")
	testNet.listenForPeerDown(t, 1, 0)
}

func TestReactorSendsResponseWithoutRequest(t *testing.T) {
	t.Skip("This test needs updated https://github.com/tendermint/tendermint/issue/7634")
	ctx := t.Context()

	testNet := setupNetwork(t, testOptions{
		MockNodes:  1,
		TotalNodes: 3,
	})
	testNet.connectAll(t)
	testNet.start(ctx, t)

	// firstNode sends the secondNode an unrequested response
	// NOTE: secondNode will send a request by default during startup so we send
	// two responses to counter that.
	testNet.sendResponse(t, 0, 1, []int{2})
	testNet.sendResponse(t, 0, 1, []int{2})

	// secondNode should evict the firstNode
	testNet.listenForPeerDown(t, 1, 0)
}

func TestReactorNeverSendsTooManyPeers(t *testing.T) {
	t.Skip("This test needs updated https://github.com/tendermint/tendermint/issue/7634")
	ctx := t.Context()

	testNet := setupNetwork(t, testOptions{
		MockNodes:  1,
		TotalNodes: 2,
	})
	testNet.connectAll(t)
	testNet.start(ctx, t)

	testNet.addNodes(t, 110)
	nodes := make([]int, 110)
	for i := range nodes {
		nodes[i] = i + 2
	}
	testNet.addAddresses(t, 1, nodes)

	// first we check that even although we have 110 peers, honest pex reactors
	// only send 100 (test if secondNode sends firstNode 100 addresses)
	testNet.pingAndlistenForNAddresses(ctx, t, 1, 0, shortWait, 100)
}

/*
func TestReactorErrorsOnReceivingTooManyPeers(t *testing.T) {
	ctx := t.Context()

	r := setupSingle(ctx, t)
	peer := p2p.Endpoint{
		AddrPort: netip.AddrPortFrom(netip.IPv6Loopback(), 1234),
	}.NodeAddress(randomNodeID())
	added, err := r.manager.Add(peer)
	require.NoError(t, err)
	require.True(t, added)

	addresses := make([]p2pproto.PexAddress, 101)
	for i := range addresses {
		nodeAddress := p2p.NodeAddress{NodeID: randomNodeID()}
		addresses[i] = p2pproto.PexAddress{
			URL: nodeAddress.String(),
		}
	}

	r.peerCh <- p2p.PeerUpdate{
		NodeID: peer.NodeID,
		Status: p2p.PeerStatusUp,
	}

	select {
	// wait for a request and then send a response with too many addresses
	case req := <-r.pexOutCh:
		if _, ok := req.Message.(*p2pproto.PexRequest); !ok {
			t.Fatal("expected v2 pex request")
		}
		r.pexInCh.Send(p2p.Envelope{
			From: peer.NodeID,
			Message: &p2pproto.PexResponse{
				Addresses: addresses,
			},
		}, 0)

	case <-time.After(10 * time.Second):
		t.Fatal("pex failed to send a request within 10 seconds")
	}

	peerErr := <-r.pexErrCh
	require.Error(t, peerErr.Err)
	require.Empty(t, r.pexOutCh)
	require.Contains(t, peerErr.Err.Error(), "peer sent too many addresses")
	require.Equal(t, peer.NodeID, peerErr.NodeID)
}*/

func TestReactorSmallPeerStoreInALargeNetwork(t *testing.T) {
	ctx := t.Context()

	testNet := setupNetwork(t, testOptions{
		TotalNodes:   8,
		MaxPeers:     7, // total-1, because PeerManager doesn't count self
		MaxConnected: 2, // enough capacity to establish a connected graph
		MaxRetryTime: 5 * time.Minute,
	})
	testNet.network.ConnectCycle(ctx, t) // Saturate capacity by connecting nodes in a cycle.
	testNet.start(ctx, t)

	t.Logf("test that peers are gossiped even if connection cap is reached")
	for _, nodeID := range testNet.nodes {
		node := testNet.network.Node(nodeID)
		require.Eventually(t, func() bool {
			return node.PeerManager.PeerRatio() >= 0.9
		}, time.Minute, checkFrequency,
			"peer ratio is: %f", node.PeerManager.PeerRatio())
	}
}

func TestReactorLargePeerStoreInASmallNetwork(t *testing.T) {
	ctx := t.Context()

	testNet := setupNetwork(t, testOptions{
		TotalNodes:   3,
		MaxPeers:     25,
		MaxConnected: 25,
		MaxRetryTime: 5 * time.Minute,
	})
	testNet.seedAddrs(t)
	testNet.start(ctx, t)

	// assert that all nodes add each other in the network
	for idx := 0; idx < len(testNet.nodes); idx++ {
		testNet.requireNumberOfPeers(t, idx, len(testNet.nodes)-1, longWait)
	}
}

func TestReactorWithNetworkGrowth(t *testing.T) {
	t.Skip("This test needs updated https://github.com/tendermint/tendermint/issue/7634")
	ctx := t.Context()

	testNet := setupNetwork(t, testOptions{
		TotalNodes: 5,
	})
	testNet.connectAll(t)
	testNet.start(ctx, t)

	// assert that all nodes add each other in the network
	for idx := 0; idx < len(testNet.nodes); idx++ {
		testNet.requireNumberOfPeers(t, idx, len(testNet.nodes)-1, shortWait)
	}

	// now we inject 10 more nodes
	testNet.addNodes(t, 10)
	for i := 5; i < testNet.total; i++ {
		node := testNet.nodes[i]
		require.NoError(t, testNet.reactors[node].Start(ctx))
		require.True(t, testNet.reactors[node].IsRunning())
		// we connect all new nodes to a single entry point and check that the
		// node can distribute the addresses to all the others
		testNet.connectPeers(ctx, t, 0, i)
	}
	require.Len(t, testNet.reactors, 15)

	// assert that all nodes add each other in the network
	for idx := 0; idx < len(testNet.nodes); idx++ {
		testNet.requireNumberOfPeers(t, idx, len(testNet.nodes)-1, longWait)
	}
}

type reactorTestSuite struct {
	network *p2p.TestNetwork

	reactors    map[types.NodeID]*Reactor
	pexChannels map[types.NodeID]*p2p.Channel

	nodes []types.NodeID
	mocks []types.NodeID
	total int
	opts  testOptions
}

type testOptions struct {
	MockNodes    int
	TotalNodes   int
	MaxPeers     uint16
	MaxConnected uint16
	MaxRetryTime time.Duration
}

// setup setups a test suite with a network of nodes. Mocknodes represent the
// hollow nodes that the test can listen and send on
func setupNetwork(t *testing.T, opts testOptions) *reactorTestSuite {
	t.Helper()

	require.Greater(t, opts.TotalNodes, opts.MockNodes)
	networkOpts := p2p.TestNetworkOptions{
		NumNodes: opts.TotalNodes,
		NodeOpts: p2p.TestNodeOptions{
			MaxPeers:     opts.MaxPeers,
			MaxConnected: opts.MaxConnected,
			MaxRetryTime: opts.MaxRetryTime,
		},
	}
	realNodes := opts.TotalNodes - opts.MockNodes

	rts := &reactorTestSuite{
		network:     p2p.MakeTestNetwork(t, networkOpts),
		reactors:    make(map[types.NodeID]*Reactor, realNodes),
		pexChannels: make(map[types.NodeID]*p2p.Channel, opts.TotalNodes),
		total:       opts.TotalNodes,
		opts:        opts,
	}

	// NOTE: we don't assert that the channels get drained after stopping the
	// reactor
	rts.pexChannels = rts.network.MakeChannelsNoCleanup(t, ChannelDescriptor())

	for idx, node := range rts.network.Nodes() {
		nodeID := node.NodeID

		// the first nodes in the array are always mock nodes
		if idx < opts.MockNodes {
			rts.mocks = append(rts.mocks, nodeID)
		} else {
			rts.reactors[nodeID] = NewReactor(
				node.Logger,
				node.PeerManager,
				make(chan struct{}),
				config.DefaultSelfRemediationConfig(),
			)
			rts.reactors[nodeID].SetChannel(rts.pexChannels[nodeID])
		}
		rts.nodes = append(rts.nodes, nodeID)
	}

	require.Len(t, rts.reactors, realNodes)

	return rts
}

// connects node1 to node2
func (r *reactorTestSuite) connectPeers(ctx context.Context, t *testing.T, sourceNode, targetNode int) {
	t.Helper()
	node1, node2 := r.checkNodePair(t, sourceNode, targetNode)

	n1 := r.network.Node(node1)
	if n1 == nil {
		require.Fail(t, "connectPeers: source node %v is not part of the testnet", node1)
		return
	}

	n2 := r.network.Node(node2)
	if n2 == nil {
		require.Fail(t, "connectPeers: target node %v is not part of the testnet", node2)
		return
	}

	n1.Connect(ctx, n2)
}

// starts up the pex reactors for each node
func (r *reactorTestSuite) start(ctx context.Context, t *testing.T) {
	t.Helper()

	for name, reactor := range r.reactors {
		require.NoError(t, reactor.Start(ctx))
		require.True(t, reactor.IsRunning())
		t.Log("started", name)
	}
	t.Cleanup(func() {
		for _, reactor := range r.reactors {
			if reactor.IsRunning() {
				reactor.Wait()
				require.False(t, reactor.IsRunning())
			}
		}
	})
}

func (r *reactorTestSuite) addNodes(t *testing.T, nodes int) {
	t.Helper()

	for range nodes {
		node := r.network.MakeNode(t, p2p.TestNodeOptions{
			MaxPeers:     r.opts.MaxPeers,
			MaxConnected: r.opts.MaxConnected,
			MaxRetryTime: r.opts.MaxRetryTime,
		})
		nodeID := node.NodeID
		r.pexChannels[nodeID] = node.MakeChannelNoCleanup(t, ChannelDescriptor())
		r.reactors[nodeID] = NewReactor(
			node.Logger,
			node.PeerManager,
			make(chan struct{}),
			config.DefaultSelfRemediationConfig(),
		)
		r.nodes = append(r.nodes, nodeID)
		r.total++
	}
}

func (r *reactorTestSuite) listenFor(
	ctx context.Context,
	t *testing.T,
	node types.NodeID,
	conditional func(msg p2p.RecvMsg) bool,
	assertion func(t *testing.T, msg p2p.RecvMsg) bool,
	waitPeriod time.Duration,
) {
	ctx, cancel := context.WithTimeout(ctx, waitPeriod)
	defer cancel()
	for {
		m,err := r.pexChannels[node].Recv(ctx)
		if err!=nil {
			break
		}
		if conditional(m) && assertion(t, m) {
			return
		}
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		require.Fail(t, "timed out waiting for message",
			"node=%v, waitPeriod=%s", node, waitPeriod)
	}

}

func (r *reactorTestSuite) listenForRequest(ctx context.Context, t *testing.T, fromNode, toNode int, waitPeriod time.Duration) {
	to, from := r.checkNodePair(t, toNode, fromNode)
	conditional := func(msg p2p.RecvMsg) bool {
		_, ok := msg.Message.(*p2pproto.PexRequest)
		return ok && msg.From == from
	}
	assertion := func(t *testing.T, msg p2p.RecvMsg) bool {
		require.Equal(t, &p2pproto.PexRequest{}, msg.Message)
		return true
	}
	r.listenFor(ctx, t, to, conditional, assertion, waitPeriod)
}

func (r *reactorTestSuite) pingAndlistenForNAddresses(
	ctx context.Context,
	t *testing.T,
	fromNode, toNode int,
	waitPeriod time.Duration,
	addresses int,
) {
	t.Helper()

	to, from := r.checkNodePair(t, toNode, fromNode)
	conditional := func(msg p2p.RecvMsg) bool {
		_, ok := msg.Message.(*p2pproto.PexResponse)
		return ok && msg.From == from
	}
	assertion := func(t *testing.T, msg p2p.RecvMsg) bool {
		m, ok := msg.Message.(*p2pproto.PexResponse)
		if !ok {
			require.Fail(t, "expected pex response v2")
			return true
		}
		// assert the same amount of addresses
		if len(m.Addresses) == addresses {
			return true
		}
		// if we didn't get the right length, we wait and send the
		// request again
		time.Sleep(300 * time.Millisecond)
		r.sendRequest(t, toNode, fromNode)
		return false
	}
	r.sendRequest(t, toNode, fromNode)
	r.listenFor(ctx, t, to, conditional, assertion, waitPeriod)
}

func (r *reactorTestSuite) listenForResponse(
	ctx context.Context,
	t *testing.T,
	fromNode, toNode int,
	waitPeriod time.Duration,
	addresses []p2pproto.PexAddress,
) {
	to, from := r.checkNodePair(t, toNode, fromNode)
	conditional := func(msg p2p.RecvMsg) bool {
		_, ok := msg.Message.(*p2pproto.PexResponse)
		return ok && msg.From == from
	}
	assertion := func(t *testing.T, msg p2p.RecvMsg) bool {
		require.Equal(t, &p2pproto.PexResponse{Addresses: addresses}, msg.Message)
		return true
	}
	r.listenFor(ctx, t, to, conditional, assertion, waitPeriod)
}

func (r *reactorTestSuite) listenForPeerDown(
	t *testing.T,
	onNode, withNode int,
) {
	on, with := r.checkNodePair(t, onNode, withNode)
	r.network.Node(on).WaitUntilDisconnected(t.Context(), with)
}

func (r *reactorTestSuite) getAddressesFor(nodes []int) []p2pproto.PexAddress {
	addresses := make([]p2pproto.PexAddress, len(nodes))
	for idx, node := range nodes {
		nodeID := r.nodes[node]
		addresses[idx] = p2pproto.PexAddress{
			URL: r.network.Node(nodeID).NodeAddress.String(),
		}
	}
	return addresses
}

func (r *reactorTestSuite) sendRequest(t *testing.T, fromNode, toNode int) {
	t.Helper()
	to, from := r.checkNodePair(t, toNode, fromNode)
	r.pexChannels[from].Send(&p2pproto.PexRequest{}, to)
}

func (r *reactorTestSuite) sendResponse(
	t *testing.T,
	fromNode, toNode int,
	withNodes []int,
) {
	t.Helper()
	from, to := r.checkNodePair(t, fromNode, toNode)
	addrs := r.getAddressesFor(withNodes)
	r.pexChannels[from].Send(&p2pproto.PexResponse{Addresses: addrs}, to)
}

func (r *reactorTestSuite) requireNumberOfPeers(
	t *testing.T,
	nodeIndex, numPeers int,
	waitPeriod time.Duration,
) {
	t.Helper()
	node := r.network.Node(r.nodes[nodeIndex])
	require.Eventuallyf(t, func() bool {
		actualNumPeers := len(node.PeerManager.Peers())
		return actualNumPeers >= numPeers
	}, waitPeriod, checkFrequency, "peer failed to connect with the asserted amount of peers "+
		"index=%d, node=%q, waitPeriod=%s expected=%d actual=%d",
		nodeIndex, r.nodes[nodeIndex], waitPeriod, numPeers,
		len(node.PeerManager.Peers()),
	)
}

func (r *reactorTestSuite) connectAll(t *testing.T) {
	r.network.Start(t)
}

// Adds enough addresses to peerManagers, so that all nodes are discoverable.
func (r *reactorTestSuite) seedAddrs(t *testing.T) {
	t.Helper()
	for i := range r.total - 1 {
		n1 := r.network.Node(r.nodes[i])
		n2 := r.network.Node(r.nodes[i+1])
		_, err := n1.PeerManager.Add(n2.NodeAddress)
		require.NoError(t, err)
	}
}

func (r *reactorTestSuite) checkNodePair(t *testing.T, first, second int) (types.NodeID, types.NodeID) {
	require.NotEqual(t, first, second)
	require.Less(t, first, r.total)
	require.Less(t, second, r.total)
	return r.nodes[first], r.nodes[second]
}

func (r *reactorTestSuite) addAddresses(t *testing.T, node int, addrs []int) {
	peerManager := r.network.Node(r.nodes[node]).PeerManager
	for _, addr := range addrs {
		require.Less(t, addr, r.total)
		address := r.network.Node(r.nodes[addr]).NodeAddress
		added, err := peerManager.Add(address)
		require.NoError(t, err)
		require.True(t, added)
	}
}

func newNodeID(t *testing.T, id string) types.NodeID {
	nodeID, err := types.NewNodeID(strings.Repeat(id, 2*types.NodeIDByteLength))
	require.NoError(t, err)
	return nodeID
}

func randomNodeID() types.NodeID {
	return types.NodeIDFromPubKey(ed25519.GenPrivKey().PubKey())
}
