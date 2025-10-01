package p2ptest

import (
	"context"
	"math/rand"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/types"
)

// Network sets up an in-memory network that can be used for high-level P2P
// testing. It creates an arbitrary number of nodes that are connected to each
// other, and can open channels across all nodes with custom reactors.
type Network struct {
	logger log.Logger
	nodes  utils.Mutex[map[types.NodeID]*Node]
}

// NetworkOptions is an argument structure to parameterize the
// MakeNetwork function.
type NetworkOptions struct {
	NumNodes int
	NodeOpts NodeOptions
}

type NodeOptions struct {
	MaxPeers     uint16
	MaxConnected uint16
	MaxRetryTime time.Duration
}

// MakeNetwork creates a test network with the given number of nodes and
// connects them to each other.
func MakeNetwork(t *testing.T, opts NetworkOptions) *Network {
	logger, _ := log.NewDefaultLogger("plain", "info")
	n := &Network{
		logger: logger,
		nodes:  utils.NewMutex(map[types.NodeID]*Node{}),
	}
	for i := 0; i < opts.NumNodes; i++ {
		n.MakeNode(t, opts.NodeOpts)
	}
	return n
}

func (n *Network) Nodes() []*Node {
	var res []*Node
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
func (n *Network) Start(t *testing.T) {
	subs := map[types.NodeID]*p2p.PeerUpdates{}
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
				require.Equal(t, p2p.PeerUpdate{
					NodeID: target.NodeID,
					Status: p2p.PeerStatusUp,
				}, peerUpdate)
			}

			select {
			case <-ctx.Done():
				require.Fail(t, "operation canceled")
			case peerUpdate := <-subs[target.NodeID].Updates():
				peerUpdate.Channels = nil
				require.Equal(t, p2p.PeerUpdate{
					NodeID: source.NodeID,
					Status: p2p.PeerStatusUp,
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
func (n *Network) NodeIDs() []types.NodeID {
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
func (n *Network) MakeChannels(
	t *testing.T,
	chDesc *p2p.ChannelDescriptor,
) map[types.NodeID]*p2p.Channel {
	channels := map[types.NodeID]*p2p.Channel{}
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
func (n *Network) MakeChannelsNoCleanup(
	t *testing.T,
	chDesc *p2p.ChannelDescriptor,
) map[types.NodeID]*p2p.Channel {
	channels := map[types.NodeID]*p2p.Channel{}
	for nodes := range n.nodes.Lock() {
		for _, node := range nodes {
			channels[node.NodeID] = node.MakeChannelNoCleanup(t, chDesc)
		}
	}
	return channels
}

func (n *Network) Node(id types.NodeID) *Node {
	for nodes := range n.nodes.Lock() {
		return nodes[id]
	}
	panic("unreachable")
}

// RandomNode returns a random node.
func (n *Network) RandomNode() *Node {
	nodes := n.Nodes()
	return nodes[rand.Intn(len(nodes))] // nolint:gosec
}

// Peers returns a node's peers (i.e. everyone except itself).
func (n *Network) Peers(id types.NodeID) []*Node {
	var peers []*Node
	for _, n := range n.Nodes() {
		if n.NodeID != id {
			peers = append(peers, n)
		}
	}
	return peers
}

// Remove removes a node from the network, stopping it and waiting for all other
// nodes to pick up the disconnection.
func (n *Network) Remove(ctx context.Context, t *testing.T, id types.NodeID) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var node *Node
	var subs []*p2p.PeerUpdates
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
		RequireUpdate(t, sub, p2p.PeerUpdate{
			NodeID: node.NodeID,
			Status: p2p.PeerStatusDown,
		})
	}
}

// Node is a node in a Network, with a Router and a PeerManager.
type Node struct {
	Logger      log.Logger
	NodeID      types.NodeID
	NodeInfo    types.NodeInfo
	NodeAddress p2p.NodeAddress
	PrivKey     crypto.PrivKey
	Router      *p2p.Router
	PeerManager *p2p.PeerManager
	Transport   *p2p.MConnTransport

	cancel context.CancelFunc
}

// MakeNode creates a new Node configured for the network with a
// running peer manager, but does not add it to the existing
// network. Callers are responsible for updating peering relationships.
func (n *Network) MakeNode(t *testing.T, opts NodeOptions) *Node {
	privKey := ed25519.GenPrivKey()
	nodeID := types.NodeIDFromPubKey(privKey.PubKey())
	logger := n.logger.With("node", nodeID[:5])
	transport := p2p.TestTransport(logger, nodeID)

	maxRetryTime := 1000 * time.Millisecond
	if opts.MaxRetryTime > 0 {
		maxRetryTime = opts.MaxRetryTime
	}

	nodeInfo := types.NodeInfo{
		NodeID:     nodeID,
		ListenAddr: transport.Endpoint().Addr.String(),
		Moniker:    string(nodeID),
		Network:    "test",
	}

	peerManager, err := p2p.NewPeerManager(logger, nodeID, dbm.NewMemDB(), p2p.PeerManagerOptions{
		MinRetryTime:    10 * time.Millisecond,
		MaxRetryTime:    maxRetryTime,
		RetryTimeJitter: time.Millisecond,
		MaxPeers:        opts.MaxPeers,
		MaxConnected:    opts.MaxConnected,
	}, p2p.NopMetrics())
	require.NoError(t, err)

	router, err := p2p.NewRouter(
		logger,
		p2p.NopMetrics(),
		privKey,
		peerManager,
		func() *types.NodeInfo { return &nodeInfo },
		transport,
		nil,
		p2p.RouterOptions{DialSleep: func(_ context.Context) error { return nil }},
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

	node := &Node{
		Logger:      logger,
		NodeID:      nodeID,
		NodeInfo:    nodeInfo,
		NodeAddress: transport.Endpoint().NodeAddress(nodeID),
		PrivKey:     privKey,
		Router:      router,
		PeerManager: peerManager,
		Transport:   transport,
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
func (n *Node) MakeChannel(
	t *testing.T,
	chDesc *p2p.ChannelDescriptor,
) *p2p.Channel {
	channel, err := n.Router.OpenChannel(chDesc)
	require.NoError(t, err)
	t.Cleanup(func() {
		RequireEmpty(t, channel)
	})
	return channel
}

// MakeChannelNoCleanup opens a channel, with automatic error handling. The
// caller must ensure proper cleanup of the channel.
func (n *Node) MakeChannelNoCleanup(
	t *testing.T,
	chDesc *p2p.ChannelDescriptor,
) *p2p.Channel {
	channel, err := n.Router.OpenChannel(chDesc)
	require.NoError(t, err)
	return channel
}

// MakePeerUpdates opens a peer update subscription, with automatic cleanup.
// It checks that all updates have been consumed during cleanup.
func (n *Node) MakePeerUpdates(ctx context.Context, t *testing.T) *p2p.PeerUpdates {
	t.Helper()
	sub := n.PeerManager.Subscribe(ctx)
	t.Cleanup(func() {
		RequireNoUpdates(ctx, t, sub)
	})

	return sub
}

// MakePeerUpdatesNoRequireEmpty opens a peer update subscription, with automatic cleanup.
// It does *not* check that all updates have been consumed, but will
// close the update channel.
func (n *Node) MakePeerUpdatesNoRequireEmpty(ctx context.Context, t *testing.T) *p2p.PeerUpdates {
	return n.PeerManager.Subscribe(ctx)
}

func MakeChannelDesc(chID p2p.ChannelID) *p2p.ChannelDescriptor {
	return &p2p.ChannelDescriptor{
		ID:                  chID,
		MessageType:         &Message{},
		Priority:            5,
		SendQueueCapacity:   10,
		RecvMessageCapacity: 10,
	}
}
