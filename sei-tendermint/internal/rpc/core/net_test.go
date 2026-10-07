package core

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func TestNetInfo_ReportsConnectedPeersOnly(t *testing.T) {
	ctx := t.Context()
	network := p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{})
	opts := p2p.TestNodeOptions{PexOnHandshake: true, SelfAddress: true}
	hub := network.MakeNode(t, opts)
	a := network.MakeNode(t, opts)
	opts.MaxConnected = utils.Some(1)
	b := network.MakeNode(t, opts)

	require.NoError(t, a.Connect(ctx, hub))
	require.NoError(t, b.Connect(ctx, hub))

	// b learns a's address from hub during the handshake, but cannot dial it.
	require.Eventually(t, func() bool {
		return slices.Contains(b.KnownAddrs(), a.NodeAddress)
	}, 10*time.Second, 10*time.Millisecond)

	env := &Environment{Router: b.Router}
	res, err := env.NetInfo(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, res.NPeers)
	require.Equal(t, []coretypes.Peer{{ID: hub.NodeID, URL: hub.NodeAddress.String()}}, res.Peers)
	require.Len(t, res.PeerConnections, 1)
	require.Equal(t, hub.NodeID, res.PeerConnections[0].ID)

	env = &Environment{Router: hub.Router}
	res, err = env.NetInfo(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, res.NPeers)
	require.ElementsMatch(t, []coretypes.Peer{
		{ID: a.NodeID, URL: a.NodeAddress.String()},
		{ID: b.NodeID, URL: b.NodeAddress.String()},
	}, res.Peers)
	require.Len(t, res.PeerConnections, 2)
}
