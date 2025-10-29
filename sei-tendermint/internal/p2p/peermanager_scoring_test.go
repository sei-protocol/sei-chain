package p2p

import (
	"testing"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/types"
)

func TestPeerScoring(t *testing.T) {
	// coppied from p2p_test shared variables
	selfKey := ed25519.GenPrivKeyFromSecret([]byte{0xf9, 0x1b, 0x08, 0xaa, 0x38, 0xee, 0x34, 0xdd})
	selfID := types.NodeIDFromPubKey(selfKey.PubKey())

	// create a mock peer manager
	db := dbm.NewMemDB()
	peerManager, err := NewPeerManager(log.NewNopLogger(), selfID, db, PeerManagerOptions{}, NopMetrics())
	require.NoError(t, err)

	// create a fake node
	a := testAddr("a")
	added, err := peerManager.Add(a)
	require.NoError(t, err)
	require.True(t, added)

	ctx := t.Context()

	t.Run("Synchronous", func(t *testing.T) {
		// update the manager and make sure it's correct
		defaultScore := DefaultMutableScore
		require.EqualValues(t, defaultScore, peerManager.Scores()[a.NodeID])

		// add a bunch of good status updates and watch things increase.
		for i := 1; i < 10; i++ {
			peerManager.SendUpdate(PeerUpdate{
				NodeID: a.NodeID,
				Status: PeerStatusGood,
			})
			require.EqualValues(t, defaultScore+PeerScore(i), peerManager.Scores()[a.NodeID])
		}
		// watch the corresponding decreases respond to update
		for i := 1; i < 10; i++ {
			peerManager.SendUpdate(PeerUpdate{
				NodeID: a.NodeID,
				Status: PeerStatusBad,
			})
			require.EqualValues(t, DefaultMutableScore+PeerScore(9)-PeerScore(i), peerManager.Scores()[a.NodeID])
		}

		// Dial failure should decrease score
		_ = peerManager.DialFailed(ctx, a)
		_ = peerManager.DialFailed(ctx, a)
		_ = peerManager.DialFailed(ctx, a)
		require.EqualValues(t, DefaultMutableScore-2, peerManager.Scores()[a.NodeID])

		// Disconnect every 3 times should also decrease score
		for i := 1; i < 7; i++ {
			peerManager.Disconnected(ctx, a.NodeID)
		}
		require.EqualValues(t, DefaultMutableScore-2, peerManager.Scores()[a.NodeID])
	})
	t.Run("TestNonPersistantPeerUpperBound", func(t *testing.T) {
		// Reset peer state to remove any previous penalties
		peerManager.store.peers[a.NodeID] = &peerInfo{
			ID:           a.NodeID,
			MutableScore: DefaultMutableScore,
		}

		// Add successful blocks to increase score
		for range 100 {
			peerManager.IncrementBlockSyncs(a.NodeID)
		}

		// Score should be capped at MaxPeerScoreNotPersistent
		require.EqualValues(t, MaxPeerScoreNotPersistent, peerManager.Scores()[a.NodeID])
	})
}
