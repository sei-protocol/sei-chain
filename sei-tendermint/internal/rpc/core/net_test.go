package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func TestNetInfo_ReportsConnectedPeers(t *testing.T) {
	ctx := t.Context()
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{})
	hub := network.MakeNode(t, p2p.TestNodeOptions{})
	a := network.MakeNode(t, p2p.TestNodeOptions{SelfAddress: true})
	// b declares no address, so hub has no URL to list for it.
	b := network.MakeNode(t, p2p.TestNodeOptions{})
	require.NoError(t, a.Connect(ctx, hub))
	require.NoError(t, b.Connect(ctx, hub))

	env := &Environment{Router: hub.Router}
	res, err := env.NetInfo(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, res.NPeers)
	require.Equal(t, []coretypes.Peer{{ID: a.NodeID, URL: a.NodeAddress.String()}}, res.Peers)
	require.ElementsMatch(t, []coretypes.PeerConnection{
		{ID: a.NodeID, State: "ready,connected", Score: 100},
		{ID: b.NodeID, State: "ready,connected", Score: 100},
	}, res.PeerConnections)
}

func TestNetInfo_ExcludesPexAddresses(t *testing.T) {
	ctx := t.Context()
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{})
	opts := p2p.TestNodeOptions{PexOnHandshake: true, SelfAddress: true}
	hub := network.MakeNode(t, opts)
	a := network.MakeNode(t, opts)
	opts.MaxConnected = utils.Some(1)
	b := network.MakeNode(t, opts)
	require.NoError(t, a.Connect(ctx, hub))
	require.NoError(t, b.Connect(ctx, hub))

	// b learns both addresses via PEX but holds a single connection.
	env := &Environment{Router: b.Router}
	require.Eventually(t, func() bool {
		res, err := env.NetInfo(ctx)
		return err == nil && len(b.KnownAddrs()) == 2 &&
			res.NPeers == 1 && len(res.Peers) == 1 && len(res.PeerConnections) == 1 &&
			res.Peers[0].ID == res.PeerConnections[0].ID
	}, 10*time.Second, 10*time.Millisecond)
}
