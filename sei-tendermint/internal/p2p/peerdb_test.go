package p2p
/*
import (
	"strings"
	"testing"

	"github.com/tendermint/tendermint/libs/log"

	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/types"
)

func TestPeerDB(t *testing.T) {
	aID := types.NodeID(strings.Repeat("a", 40))
	aAddresses := []NodeAddress{
		{NodeID: aID, Hostname: "127.0.0.1", Port: 26657},
	}

	bID := types.NodeID(strings.Repeat("b", 40))
	bAddresses := []NodeAddress{
		{NodeID: bID, Hostname: "b10c::1", Port: 26657},
	}

	cID := types.NodeID(strings.Repeat("c", 40))
	cAddresses := []NodeAddress{
		{NodeID: cID, Hostname: "host.domain", Port: 80},
	}

	// Create an initial peer manager and add the peers.
	db := dbm.NewMemDB()
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, db, RouterOptions{
		PersistentPeers: []types.NodeID{aID},
		PeerScores:      map[types.NodeID]PeerScore{bID: 1},
	}, NopMetrics())
	require.NoError(t, err)

	for _, addr := range append(append(aAddresses, bAddresses...), cAddresses...) {
		added, err := peerManager.Add(addr)
		require.NoError(t, err)
		require.True(t, added)
	}

	require.ElementsMatch(t, aAddresses, peerManager.Addresses(aID))
	require.ElementsMatch(t, bAddresses, peerManager.Addresses(bID))
	require.ElementsMatch(t, cAddresses, peerManager.Addresses(cID))

	require.Equal(t, map[types.NodeID]PeerScore{
		aID: PeerScorePersistent,
		bID: 1,
		cID: DefaultMutableScore,
	}, peerManager.Scores())

	// Creating a new peer manager with the same database should retain the
	// peers, but they should have updated scores from the new PersistentPeers
	// configuration.
	peerManager, err = NewPeerManager(log.NewNopLogger(), selfID, db, RouterOptions{
		PersistentPeers: []types.NodeID{bID},
		PeerScores:      map[types.NodeID]PeerScore{cID: 1},
	}, NopMetrics())
	require.NoError(t, err)

	require.ElementsMatch(t, aAddresses, peerManager.Addresses(aID))
	require.ElementsMatch(t, bAddresses, peerManager.Addresses(bID))
	require.ElementsMatch(t, cAddresses, peerManager.Addresses(cID))
	require.Equal(t, map[types.NodeID]PeerScore{
		aID: 0,
		bID: PeerScorePersistent,
		cID: 1,
	}, peerManager.Scores())

	// Introduce a dial failure and persistent peer score should be reduced by one
	ctx := t.Context()
	peerManager.DialFailed(ctx, bAddresses[0])
	require.Equal(t, map[types.NodeID]PeerScore{
		aID: 0,
		bID: PeerScorePersistent,
		cID: 1,
	}, peerManager.Scores())
}

func TestPeerManager_Advertise(t *testing.T) {
	aID := types.NodeID(strings.Repeat("a", 40))
	aTCP := NodeAddress{NodeID: aID, Hostname: "127.0.0.1", Port: 26657}

	bID := types.NodeID(strings.Repeat("b", 40))
	bTCP := NodeAddress{NodeID: bID, Hostname: "b10c::1", Port: 26657}

	cID := types.NodeID(strings.Repeat("c", 40))
	cTCP := NodeAddress{NodeID: cID, Hostname: "host.domain", Port: 80}

	dID := types.NodeID(strings.Repeat("d", 40))

	// Create an initial peer manager and add the peers.
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), RouterOptions{
		PeerScores: map[types.NodeID]PeerScore{aID: 3, bID: 2, cID: 1},
	}, NopMetrics())
	require.NoError(t, err)

	added, err := peerManager.Add(aTCP)
	require.NoError(t, err)
	require.True(t, added)
	added, err = peerManager.Add(bTCP)
	require.NoError(t, err)
	require.True(t, added)
	added, err = peerManager.Add(cTCP)
	require.NoError(t, err)
	require.True(t, added)

	// d should get all addresses.
	require.ElementsMatch(t, []NodeAddress{
		aTCP, bTCP, cTCP,
	}, peerManager.Advertise(dID, 100))

	// a should not get its own addresses.
	require.ElementsMatch(t, []NodeAddress{
		bTCP, cTCP,
	}, peerManager.Advertise(aID, 100))

	// Asking for 0 addresses should return, well, 0.
	require.Empty(t, peerManager.Advertise(aID, 0))

	// Asking for 2 addresses should get the highest-rated ones, i.e. a.
	require.ElementsMatch(t, []NodeAddress{
		aTCP,
	}, peerManager.Advertise(dID, 1))
}

func TestPeerManager_Advertise_Self(t *testing.T) {
	dID := types.NodeID(strings.Repeat("d", 40))

	self := NodeAddress{NodeID: selfID, Hostname: "2001:db8::1", Port: 26657}

	// Create a peer manager with SelfAddress defined.
	peerManager, err := NewPeerManager(selfID, RouterOptions{ SelfAddress: utils.Some[self] })
	require.NoError(t, err)

	// peer manager should always advertise its SelfAddress.
	require.ElementsMatch(t, []NodeAddress{
		self,
	}, peerManager.Advertise(dID, 100))
}*/
