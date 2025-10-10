package p2p

import (
	"context"
	"math/rand"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/tcp"
	"github.com/tendermint/tendermint/types"
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
	MaxPeers     uint16
	MaxConnected uint16
	MaxRetryTime time.Duration
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
	var res []*TestNode
	for nodes := range n.nodes.Lock() {
		for _, node := range nodes {
			res = append(res, node)
		}
	}
	return res
}

// Start starts the network by setting up a list of node addresses to dial in
// addition to creating a peer update subscription for each node. Finally, all
// nodes are connected to each other.
func (n *TestNetwork) Start(t *testing.T) {
	subs := map[types.NodeID]*PeerUpdates{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	nodes := n.Nodes()
	for _, node := range nodes {
		subs[node.NodeID] = node.PeerManager.Subscribe(ctx)
	}

	// For each node, dial the nodes that it still doesn't have a connection to
	// (either inbound or outbound), and wait for both sides to confirm the
	// connection via the subscriptions.
	for i, source := range nodes {
		for _, target := range nodes[i+1:] { // nodes <i already connected
			added, err := source.PeerManager.Add(target.NodeAddress)
			require.NoError(t, err)
			require.True(t, added)

			select {
			case <-ctx.Done():
				require.Fail(t, "operation canceled")
			case peerUpdate := <-subs[source.NodeID].Updates():
				peerUpdate.Channels = nil
				require.Equal(t, PeerUpdate{
					NodeID: target.NodeID,
					Status: PeerStatusUp,
				}, peerUpdate)
			}

			select {
			case <-ctx.Done():
				require.Fail(t, "operation canceled")
			case peerUpdate := <-subs[target.NodeID].Updates():
				peerUpdate.Channels = nil
				require.Equal(t, PeerUpdate{
					NodeID: source.NodeID,
					Status: PeerStatusUp,
				}, peerUpdate)
			}

			// Add the address to the target as well, so it's able to dial the
			// source back if that's even necessary.
			added, err = target.PeerManager.Add(source.NodeAddress)
			require.NoError(t, err)
			require.True(t, added)
		}
	}
}

// NodeIDs returns the network's node IDs.
func (n *TestNetwork) NodeIDs() []types.NodeID {
	ids := []types.NodeID{}
	for nodes := range n.nodes.Lock() {
		for id := range nodes {
			ids = append(ids, id)
		}
	}
	return ids
}

// MakeChannels makes a channel on all nodes and returns them, automatically
// doing error checks and cleanups.
func (n *TestNetwork) MakeChannels(
	t *testing.T,
	chDesc *ChannelDescriptor,
) map[types.NodeID]*Channel {
	channels := map[types.NodeID]*Channel{}
	for nodes := range n.nodes.Lock() {
		for id, n := range nodes {
			channels[id] = n.MakeChannel(t, chDesc)
		}
	}
	return channels
}

// MakeChannelsNoCleanup makes a channel on all nodes and returns them,
// automatically doing error checks. The caller must ensure proper cleanup of
// all the channels.
func (n *TestNetwork) MakeChannelsNoCleanup(
	t *testing.T,
	chDesc *ChannelDescriptor,
) map[types.NodeID]*Channel {
	channels := map[types.NodeID]*Channel{}
	for nodes := range n.nodes.Lock() {
		for _, node := range nodes {
			channels[node.NodeID] = node.MakeChannelNoCleanup(t, chDesc)
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
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var node *TestNode
	var subs []*PeerUpdates
	for nodes := range n.nodes.Lock() {
		require.Contains(t, nodes, id)
		node = nodes[id]
		delete(nodes, id)
		for _, peer := range nodes {
			subs = append(subs, peer.PeerManager.Subscribe(ctx))
		}
	}
	node.cancel()
	if node.Router.IsRunning() {
		node.Router.Stop()
		node.Router.Wait()
	}
	for _, sub := range subs {
		RequireUpdate(t, sub, PeerUpdate{
			NodeID: node.NodeID,
			Status: PeerStatusDown,
		})
	}
}

// Node is a node in a Network, with a Router and a PeerManager.
type TestNode struct {
	Logger      log.Logger
	NodeID      types.NodeID
	NodeInfo    types.NodeInfo
	NodeAddress NodeAddress
	PrivKey     crypto.PrivKey
	Router      *Router
	PeerManager *PeerManager

	cancel context.CancelFunc
}

// MakeNode creates a new Node configured for the network with a
// running peer manager, but does not add it to the existing
// network. Callers are responsible for updating peering relationships.
func (n *TestNetwork) MakeNode(t *testing.T, opts TestNodeOptions) *TestNode {
	privKey := ed25519.GenPrivKey()
	nodeID := types.NodeIDFromPubKey(privKey.PubKey())
	logger := n.logger.With("node", nodeID[:5])

	maxRetryTime := 1000 * time.Millisecond
	if opts.MaxRetryTime > 0 {
		maxRetryTime = opts.MaxRetryTime
	}

	routerOpts := RouterOptions{
		DialSleep:  func(_ context.Context) error { return nil },
		Endpoint:   Endpoint{AddrPort: tcp.TestReserveAddr()},
		Connection: conn.DefaultMConnConfig(),
	}
	routerOpts.Connection.FlushThrottle = 0
	nodeInfo := types.NodeInfo{
		NodeID:     nodeID,
		ListenAddr: routerOpts.Endpoint.String(),
		Moniker:    string(nodeID),
		Network:    "test",
	}

	peerManager, err := NewPeerManager(logger, nodeID, dbm.NewMemDB(), PeerManagerOptions{
		MinRetryTime:    10 * time.Millisecond,
		MaxRetryTime:    maxRetryTime,
		RetryTimeJitter: time.Millisecond,
		MaxPeers:        opts.MaxPeers,
		MaxConnected:    opts.MaxConnected,
	}, NopMetrics())
	require.NoError(t, err)

	router, err := NewRouter(
		logger,
		NopMetrics(),
		privKey,
		peerManager,
		func() *types.NodeInfo { return &nodeInfo },
		nil,
		routerOpts,
	)

	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, router.Start(ctx))
	t.Cleanup(func() {
		if router.IsRunning() {
			router.Stop()
			router.Wait()
		}
		cancel()
	})

	node := &TestNode{
		Logger:      logger,
		NodeID:      nodeID,
		NodeInfo:    nodeInfo,
		NodeAddress: routerOpts.Endpoint.NodeAddress(nodeID),
		PrivKey:     privKey,
		Router:      router,
		PeerManager: peerManager,
		cancel:      cancel,
	}

	for nodes := range n.nodes.Lock() {
		nodes[node.NodeID] = node
	}
	return node
}

// MakeChannel opens a channel, with automatic error handling and cleanup. On
// test cleanup, it also checks that the channel is empty, to make sure
// all expected messages have been asserted.
func (n *TestNode) MakeChannel(
	t *testing.T,
	chDesc *ChannelDescriptor,
) *Channel {
	channel, err := n.Router.OpenChannel(chDesc)
	require.NoError(t, err)
	t.Cleanup(func() {
		RequireEmpty(t, channel)
	})
	return channel
}

// MakeChannelNoCleanup opens a channel, with automatic error handling. The
// caller must ensure proper cleanup of the channel.
func (n *TestNode) MakeChannelNoCleanup(
	t *testing.T,
	chDesc *ChannelDescriptor,
) *Channel {
	channel, err := n.Router.OpenChannel(chDesc)
	require.NoError(t, err)
	return channel
}

// MakePeerUpdates opens a peer update subscription, with automatic cleanup.
// It checks that all updates have been consumed during cleanup.
func (n *TestNode) MakePeerUpdates(ctx context.Context, t *testing.T) *PeerUpdates {
	t.Helper()
	sub := n.PeerManager.Subscribe(ctx)
	t.Cleanup(func() { RequireNoUpdates(t, sub) })
	return sub
}

// MakePeerUpdatesNoRequireEmpty opens a peer update subscription, with automatic cleanup.
// It does *not* check that all updates have been consumed, but will
// close the update channel.
func (n *TestNode) MakePeerUpdatesNoRequireEmpty(ctx context.Context) *PeerUpdates {
	return n.PeerManager.Subscribe(ctx)
}

func MakeTestChannelDesc(chID ChannelID) *ChannelDescriptor {
	return &ChannelDescriptor{
		ID:                  chID,
		MessageType:         &TestMessage{},
		Priority:            5,
		SendQueueCapacity:   10,
		RecvMessageCapacity: 10,
	}
}

// RequireEmpty requires that the given channel is empty.
func RequireEmpty(t *testing.T, channels ...*Channel) {
	t.Helper()
	for _, ch := range channels {
		if ch.ReceiveLen() != 0 {
			t.Errorf("nonempty channel %v", ch)
		}
	}
}

// RequireReceive requires that the given envelope is received on the channel.
func RequireReceive(t *testing.T, channel *Channel, expect Envelope) {
	t.Helper()
	RequireReceiveUnordered(t, channel, utils.Slice(&expect))
}

// RequireReceiveUnordered requires that the given envelopes are all received on
// the channel, ignoring order.
func RequireReceiveUnordered(t *testing.T, channel *Channel, expect []*Envelope) {
	t.Helper()
	t.Logf("awaiting %d messages", len(expect))
	actual := []*Envelope{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	iter := channel.RecvAll(ctx)
	for iter.Next(ctx) {
		actual = append(actual, iter.Envelope())
		if len(actual) == len(expect) {
			require.ElementsMatch(t, expect, actual, "len=%d", len(actual))
			return
		}
	}
	require.FailNow(t, "not enough messages")
}

// RequireSend requires that the given envelope is sent on the channel.
func RequireSend(t *testing.T, channel *Channel, envelope Envelope) {
	t.Logf("sending message %v", envelope)
	require.NoError(t, channel.Send(t.Context(), envelope))
}

// RequireNoUpdates requires that a PeerUpdates subscription is empty.
func RequireNoUpdates(t *testing.T, peerUpdates *PeerUpdates) {
	t.Helper()
	if len(peerUpdates.Updates()) != 0 {
		require.FailNow(t, "unexpected peer updates")
	}
}

// RequireError requires that the given peer error is submitted for a peer.
func RequireSendError(t *testing.T, channel *Channel, peerError PeerError) {
	require.NoError(t, channel.SendError(t.Context(), peerError))
}

// RequireUpdate requires that a PeerUpdates subscription yields the given update.
func RequireUpdate(t *testing.T, peerUpdates *PeerUpdates, expect PeerUpdate) {
	t.Helper()
	t.Logf("awaiting update %v", expect)
	update, err := utils.Recv(t.Context(), peerUpdates.Updates())
	if err != nil {
		require.FailNow(t, "utils.Recv(): %w", err)
	}
	require.Equal(t, expect.NodeID, update.NodeID, "node id did not match")
	require.Equal(t, expect.Status, update.Status, "statuses did not match")
}

// RequireUpdates requires that a PeerUpdates subscription yields the given updates
// in the given order.
func RequireUpdates(t *testing.T, peerUpdates *PeerUpdates, expect []PeerUpdate) {
	t.Helper()
	t.Logf("awaiting %d updates", len(expect))
	actual := []PeerUpdate{}
	for {
		update, err := utils.Recv(t.Context(), peerUpdates.Updates())
		if err != nil {
			require.FailNow(t, "utils.Recv(): %v", err)
		}
		actual = append(actual, update)
		if len(actual) == len(expect) {
			for idx := range expect {
				require.Equal(t, expect[idx].NodeID, actual[idx].NodeID)
				require.Equal(t, expect[idx].Status, actual[idx].Status)
			}
			return
		}
	}
}
