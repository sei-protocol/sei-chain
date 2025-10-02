package p2ptest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/libs/utils"
)

// RequireEmpty requires that the given channel is empty.
func RequireEmpty(t *testing.T, channels ...*p2p.Channel) {
	t.Helper()
	for _, ch := range channels {
		if ch.ReceiveLen() != 0 {
			t.Errorf("nonempty channel %v", ch)
		}
	}
}

// RequireReceive requires that the given envelope is received on the channel.
func RequireReceive(t *testing.T, channel *p2p.Channel, expect p2p.Envelope) {
	t.Helper()
	RequireReceiveUnordered(t, channel, utils.Slice(&expect))
}

// RequireReceiveUnordered requires that the given envelopes are all received on
// the channel, ignoring order.
func RequireReceiveUnordered(t *testing.T, channel *p2p.Channel, expect []*p2p.Envelope) {
	t.Helper()
	t.Logf("awaiting %d messages", len(expect))
	actual := []*p2p.Envelope{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	iter := channel.Receive(ctx)
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
func RequireSend(t *testing.T, channel *p2p.Channel, envelope p2p.Envelope) {
	t.Logf("sending message %v", envelope)
	require.NoError(t, channel.Send(t.Context(), envelope))
}

// RequireNoUpdates requires that a PeerUpdates subscription is empty.
func RequireNoUpdates(ctx context.Context, t *testing.T, peerUpdates *p2p.PeerUpdates) {
	t.Helper()
	if len(peerUpdates.Updates()) != 0 {
		require.FailNow(t, "unexpected peer updates")
	}
}

// RequireError requires that the given peer error is submitted for a peer.
func RequireSendError(t *testing.T, channel *p2p.Channel, peerError p2p.PeerError) {
	require.NoError(t, channel.SendError(t.Context(), peerError))
}

// RequireUpdate requires that a PeerUpdates subscription yields the given update.
func RequireUpdate(t *testing.T, peerUpdates *p2p.PeerUpdates, expect p2p.PeerUpdate) {
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
func RequireUpdates(t *testing.T, peerUpdates *p2p.PeerUpdates, expect []p2p.PeerUpdate) {
	t.Logf("awaiting %d updates", len(expect))
	actual := []p2p.PeerUpdate{}
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
