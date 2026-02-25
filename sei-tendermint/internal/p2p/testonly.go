package p2p

import (
	"context"
	"fmt"
	"math/rand"
	"net/netip"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"

	"github.com/gogo/protobuf/proto"
	gogotypes "github.com/gogo/protobuf/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/tcp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"golang.org/x/time/rate"
)

// Message is a simple message containing a string-typed Value field.
type TestMessage = gogotypes.StringValue

func NodeInSlice(id types.NodeID, ids []types.NodeID) bool {
	for _, n := range ids {
		if id == n {
			return true
		}
	}
	return false
}

// Network sets up an in-memory network that can be used for high-level P2P
// testing. It creates an arbitrary number of nodes that are connected to each
// other, and can open channels across all nodes with custom reactors.
type TestNetwork struct {
	logger log.Logger
	nodes  utils.Mutex[map[types.NodeID]*TestNode]
}

// NetworkOptions is an argument structure to parameterize the
// MakeNetwork function.
type TestNetworkOptions struct {
	NumNodes int
	NodeOpts TestNodeOptions
}

type TestNodeOptions struct {
	MaxPeers     utils.Option[uint]
	MaxConnected utils.Option[uint]
}

func TestAddress(r *Router) NodeAddress {
	e := r.Endpoint()
	addr := e.Addr()
	port := e.Port()
	switch addr {
	case netip.IPv4Unspecified():
		addr = tcp.IPv4Loopback()
	case netip.IPv6Unspecified():
		addr = netip.IPv6Loopback()
	}
	return Endpoint{netip.AddrPortFrom(addr, port)}.
		NodeAddress(r.nodeInfoProducer().NodeID)
}

// MakeNetwork creates a test network with the given number of nodes and
// connects them to each other.
func MakeTestNetwork(t *testing.T, opts TestNetworkOptions) *TestNetwork {
	logger, _ := log.NewDefaultLogger("plain", "info")
	n := &TestNetwork{
		logger: logger,
		nodes:  utils.NewMutex(map[types.NodeID]*TestNode{}),
	}
	for i := 0; i < opts.NumNodes; i++ {
		n.MakeNode(t, opts.NodeOpts)
	}
	return n
}

func (n *TestNetwork) Nodes() []*TestNode {
	var res []*TestNode //nolint:prealloc
	for nodes := range n.nodes.Lock() {
		for _, node := range nodes {
			res = append(res, node)
		}
	}
	return res
}

func (n *TestNetwork) ConnectCycle(ctx context.Context, t *testing.T) {
	nodes := n.Nodes()
	N := len(nodes)
	for i := range nodes {
		err := nodes[i].Router.peerManager.AddAddrs(utils.Slice(nodes[(i+1)%len(nodes)].NodeAddress))
		require.NoError(t, err)
	}
	for i := range n.Nodes() {
		nodes[i].WaitForConn(ctx, nodes[(i+1)%N].NodeID, true)
		nodes[i].WaitForConn(ctx, nodes[(i+N-1)%N].NodeID, true)
	}
}

// Start starts the network by setting up a list of node addresses to dial in
// addition to creating a peer update subscription for each node. Finally, all
// nodes are connected to each other.
func (n *TestNetwork) Start(t *testing.T) {
	nodes := n.Nodes()
	// Populate peer managers.
	for i, source := range nodes {
		for _, target := range nodes[i+1:] { // nodes <i already connected
			err := source.Router.peerManager.AddAddrs(utils.Slice(target.NodeAddress))
			require.NoError(t, err)
		}
	}
	t.Log("Await connections.")
	for _, source := range nodes {
		if _, err := source.Router.peerManager.conns.Wait(t.Context(), func(conns ConnSet) bool {
			for _, target := range nodes {
				if target.NodeID == source.NodeID {
					continue
				}
				_, ok := conns.Get(target.NodeID)
				if !ok {
					return false
				}
			}
			return true
		}); err != nil {
			panic(err)
		}
	}
}

// NodeIDs returns the network's node IDs.
func (n *TestNetwork) NodeIDs() []types.NodeID {
	ids := []types.NodeID{} //nolint:prealloc
	for nodes := range n.nodes.Lock() {
		for id := range nodes {
			ids = append(ids, id)
		}
	}
	return ids
}

// MakeChannels makes a channel on all nodes and returns them, automatically
// doing error checks and cleanups.
func TestMakeChannels[T proto.Message](
	t *testing.T,
	n *TestNetwork,
	chDesc ChannelDescriptor[T],
) map[types.NodeID]*Channel[T] {
	channels := map[types.NodeID]*Channel[T]{}
	for nodes := range n.nodes.Lock() {
		for id, n := range nodes {
			channels[id] = TestMakeChannel(t, n, chDesc)
		}
	}
	return channels
}

// MakeChannelsNoCleanup makes a channel on all nodes and returns them,
// automatically doing error checks. The caller must ensure proper cleanup of
// all the channels.
func TestMakeChannelsNoCleanup[T proto.Message](
	t *testing.T,
	n *TestNetwork,
	chDesc ChannelDescriptor[T],
) map[types.NodeID]*Channel[T] {
	channels := map[types.NodeID]*Channel[T]{}
	for nodes := range n.nodes.Lock() {
		for _, node := range nodes {
			channels[node.NodeID] = TestMakeChannelNoCleanup(t, node, chDesc)
		}
	}
	return channels
}

func (n *TestNetwork) Node(id types.NodeID) *TestNode {
	for nodes := range n.nodes.Lock() {
		return nodes[id]
	}
	panic("unreachable")
}

// RandomNode returns a random node.
func (n *TestNetwork) RandomNode() *TestNode {
	nodes := n.Nodes()
	return nodes[rand.Intn(len(nodes))] // nolint:gosec
}

// Peers returns a node's peers (i.e. everyone except itself).
func (n *TestNetwork) Peers(id types.NodeID) []*TestNode {
	var peers []*TestNode
	for _, n := range n.Nodes() {
		if n.NodeID != id {
			peers = append(peers, n)
		}
	}
	return peers
}

// Remove removes a node from the network, stopping it and waiting for all other
// nodes to pick up the disconnection.
func (n *TestNetwork) Remove(t *testing.T, id types.NodeID) {
	var node *TestNode
	var peers []*TestNode //nolint:prealloc
	for nodes := range n.nodes.Lock() {
		require.Contains(t, nodes, id)
		node = nodes[id]
		delete(nodes, id)
		for _, peer := range nodes {
			peers = append(peers, peer)
		}
	}
	node.Router.Stop()
	for _, peer := range peers {
		peer.WaitForConn(t.Context(), id, false)
	}
}

// Node is a node in a Network, with a Router and a PeerManager.
type TestNode struct {
	Logger      log.Logger
	NodeID      types.NodeID
	NodeInfo    types.NodeInfo
	NodeAddress NodeAddress
	PrivKey     NodeSecretKey
	Router      *Router
}

// Waits for the specific connection to get disconnected.
func (n *TestNode) WaitForDisconnect(ctx context.Context, conn *ConnV2) {
	if _, err := n.Router.peerManager.conns.Wait(ctx, func(conns ConnSet) bool {
		got, ok := conns.Get(conn.Info().ID)
		return !ok || got != conn
	}); err != nil {
		panic(err)
	}
}

func (n *TestNode) WaitForConns(ctx context.Context, wantPeers int) {
	if _, err := n.Router.peerManager.conns.Wait(ctx, func(conns ConnSet) bool {
		return conns.Len() >= wantPeers
	}); err != nil {
		panic(err)
	}
}

func (n *TestNode) WaitForConn(ctx context.Context, target types.NodeID, status bool) {
	if _, err := n.Router.peerManager.conns.Wait(ctx, func(conns ConnSet) bool {
		_, ok := conns.Get(target)
		return ok == status
	}); err != nil {
		panic(err)
	}
}

func (n *TestNode) Connect(ctx context.Context, target *TestNode) {
	_ = n.Router.peerManager.AddAddrs(utils.Slice(target.NodeAddress))
	n.WaitForConn(ctx, target.NodeID, true)
	target.WaitForConn(ctx, n.NodeID, true)
}

func (n *TestNode) Disconnect(ctx context.Context, target types.NodeID) {
	conn, ok := n.Router.peerManager.Conns().Get(target)
	if !ok {
		panic("not connected")
	}
	conn.Close()
	n.WaitForConn(ctx, target, false)
}

// MakeNode creates a new Node configured for the network with a
// running peer manager, but does not add it to the existing
// network. Callers are responsible for updating peering relationships.
func (n *TestNetwork) MakeNode(t *testing.T, opts TestNodeOptions) *TestNode {
	privKey := NodeSecretKey(ed25519.GenerateSecretKey())
	nodeID := privKey.Public().NodeID()
	logger := n.logger.With("node", nodeID[:5])

	routerOpts := &RouterOptions{
		Endpoint:                 Endpoint{AddrPort: tcp.TestReserveAddr()},
		Connection:               conn.DefaultMConnConfig(),
		IncomingConnectionWindow: utils.Some[time.Duration](0),
		MaxAcceptRate:            utils.Some(rate.Inf),
		MaxDialRate:              utils.Some(rate.Limit(30.)),
		MaxPeers:                 opts.MaxPeers,
		MaxConnected:             opts.MaxConnected,
	}
	routerOpts.Connection.FlushThrottle = 0
	nodeInfo := types.NodeInfo{
		NodeID: nodeID,
		// Endpoint has been allocated via tcp.TestReserveAddr(), so it is NOT IP(v4/v6)Unspecified.
		ListenAddr: routerOpts.Endpoint.String(),
		Moniker:    string(nodeID),
		Network:    "test",
	}

	router, err := NewRouter(
		logger,
		NopMetrics(),
		privKey,
		func() *types.NodeInfo { return &nodeInfo },
		dbm.NewMemDB(),
		routerOpts,
	)
	require.NoError(t, err)
	require.NoError(t, router.Start(t.Context()))
	require.NoError(t, router.WaitForStart(t.Context()))
	t.Cleanup(router.Stop)

	node := &TestNode{
		Logger:      logger,
		NodeID:      nodeID,
		NodeInfo:    nodeInfo,
		NodeAddress: routerOpts.Endpoint.NodeAddress(nodeID),
		PrivKey:     privKey,
		Router:      router,
	}

	for nodes := range n.nodes.Lock() {
		nodes[node.NodeID] = node
	}
	return node
}

// MakeChannel opens a channel, with automatic error handling and cleanup. On
// test cleanup, it also checks that the channel is empty, to make sure
// all expected messages have been asserted.
func TestMakeChannel[T proto.Message](
	t *testing.T,
	n *TestNode,
	chDesc ChannelDescriptor[T],
) *Channel[T] {
	ch, err := OpenChannel(n.Router, chDesc)
	require.NoError(t, err)
	t.Cleanup(func() {
		RequireEmpty(t, ch)
	})
	return ch
}

// MakeChannelNoCleanup opens a channel, with automatic error handling. The
// caller must ensure proper cleanup of the channel.
func TestMakeChannelNoCleanup[T proto.Message](t *testing.T, n *TestNode, chDesc ChannelDescriptor[T]) *Channel[T] {
	ch, err := OpenChannel(n.Router, chDesc)
	require.NoError(t, err)
	return ch
}

// MakePeerUpdates opens a peer update subscription, with automatic cleanup.
// It checks that all updates have been consumed during cleanup.
func (n *TestNode) MakePeerUpdates() *PeerUpdatesRecv {
	return n.Router.peerManager.Subscribe()
}

func MakeTestChannelDesc(chID ChannelID) ChannelDescriptor[*TestMessage] {
	return ChannelDescriptor[*TestMessage]{
		ID:                  chID,
		MessageType:         &TestMessage{},
		Priority:            5,
		SendQueueCapacity:   10,
		RecvMessageCapacity: 10,
	}
}

// RequireEmpty requires that the given channel is empty.
func RequireEmpty[T proto.Message](t *testing.T, channels ...*Channel[T]) {
	for _, ch := range channels {
		if ch.ReceiveLen() != 0 {
			panic(fmt.Errorf("nonempty channel %T", ch))
		}
	}
}

// RequireReceive requires that the given envelope is received on the channel.
func RequireReceive[T proto.Message](t *testing.T, channel *Channel[T], want RecvMsg[T]) {
	t.Helper()
	RequireReceiveUnordered(t, channel, utils.Slice(want))
}

// RequireReceiveUnordered requires that the given envelopes are all received on
// the channel, ignoring order.
func RequireReceiveUnordered[T proto.Message](t *testing.T, channel *Channel[T], want []RecvMsg[T]) {
	t.Helper()
	t.Logf("awaiting %d messages", len(want))
	var got []RecvMsg[T]
	for len(got) < len(want) {
		m, err := channel.Recv(t.Context())
		if err != nil {
			panic(err)
		}
		got = append(got, m)
	}
	require.ElementsMatch(t, want, got, "len=%d", len(got))
}

// RequireUpdate requires that a PeerUpdates subscription yields the given update.
func RequireUpdate(t *testing.T, recv *PeerUpdatesRecv, expect PeerUpdate) {
	t.Helper()
	t.Logf("awaiting update %v", expect)
	update, err := recv.Recv(t.Context())
	if err != nil {
		require.FailNow(t, "recv.Recv(): %w", err)
	}
	utils.OrPanic(utils.TestDiff(expect.NodeID, update.NodeID))
	utils.OrPanic(utils.TestDiff(expect.Status, update.Status))
}

// RequireUpdates requires that a PeerUpdates subscription yields the given updates
// in the given order.
func RequireUpdates(t *testing.T, recv *PeerUpdatesRecv, expect []PeerUpdate) {
	t.Helper()
	t.Logf("awaiting %d updates", len(expect))
	actual := []PeerUpdate{}
	for len(actual) < len(expect) {
		update, err := recv.Recv(t.Context())
		if err != nil {
			require.FailNow(t, "utils.Recv(): %v", err)
		}
		expect = append(expect, update)
	}
	for idx := range expect {
		require.Equal(t, expect[idx].NodeID, actual[idx].NodeID)
		require.Equal(t, expect[idx].Status, actual[idx].Status)
	}
}
