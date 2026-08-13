package p2p

import (
	"context"
	"strings"
	"testing"
	"time"

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
	id := types.NodeID(strings.Repeat("a1", 20))
	added, err := peerManager.Add(NodeAddress{NodeID: id, Protocol: "memory"})
	require.NoError(t, err)
	require.True(t, added)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("Synchronous", func(t *testing.T) {
		// update the manager and make sure it's correct
		defaultScore := DefaultMutableScore
		require.EqualValues(t, defaultScore, peerManager.Scores()[id])

		// add a bunch of good status updates and watch things increase.
		for i := 1; i < 10; i++ {
			peerManager.processPeerEvent(ctx, PeerUpdate{
				NodeID: id,
				Status: PeerStatusGood,
			})
			require.EqualValues(t, defaultScore+PeerScore(i), peerManager.Scores()[id])
		}
		// watch the corresponding decreases respond to update
		for i := 1; i < 10; i++ {
			peerManager.processPeerEvent(ctx, PeerUpdate{
				NodeID: id,
				Status: PeerStatusBad,
			})
			require.EqualValues(t, DefaultMutableScore+PeerScore(9)-PeerScore(i), peerManager.Scores()[id])
		}

		// Dial failure should decrease score
		addr := NodeAddress{NodeID: id, Protocol: "memory"}
		_ = peerManager.DialFailed(ctx, addr)
		_ = peerManager.DialFailed(ctx, addr)
		_ = peerManager.DialFailed(ctx, addr)
		require.EqualValues(t, DefaultMutableScore-2, peerManager.Scores()[id])

		// Disconnect every 3 times should also decrease score
		for i := 1; i < 7; i++ {
			peerManager.Disconnected(ctx, id)
		}
		require.EqualValues(t, DefaultMutableScore-2, peerManager.Scores()[id])
	})
	t.Run("AsynchronousIncrement", func(t *testing.T) {
		start := peerManager.Scores()[id]
		pu := peerManager.Subscribe(ctx)
		pu.SendUpdate(ctx, PeerUpdate{
			NodeID: id,
			Status: PeerStatusGood,
		})
		require.Eventually(t,
			func() bool { return start+1 == peerManager.Scores()[id] },
			time.Second,
			time.Millisecond,
			"startAt=%d score=%d", start, peerManager.Scores()[id])
	})
	t.Run("AsynchronousDecrement", func(t *testing.T) {
		start := peerManager.Scores()[id]
		pu := peerManager.Subscribe(ctx)
		pu.SendUpdate(ctx, PeerUpdate{
			NodeID: id,
			Status: PeerStatusBad,
		})
		require.Eventually(t,
			func() bool { return start-1 == peerManager.Scores()[id] },
			time.Second,
			time.Millisecond,
			"startAt=%d score=%d", start, peerManager.Scores()[id])
	})
	t.Run("TestNonPersistantPeerUpperBound", func(t *testing.T) {
		// Reset peer state to remove any previous penalties
		peerManager.store.peers[id] = &peerInfo{
			ID:           id,
			MutableScore: DefaultMutableScore,
		}

		// Add successful blocks to increase score
		for i := 0; i < 100; i++ {
			peerManager.IncrementBlockSyncs(id)
		}

		// Score should be capped at MaxPeerScoreNotPersistent
		require.EqualValues(t, MaxPeerScoreNotPersistent, peerManager.Scores()[id])
	})
}
