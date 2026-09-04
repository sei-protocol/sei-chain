package flatkv

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/stretchr/testify/require"
)

// TestPublishWaitsForRoomOnAFullHashStream pins what publish does when the stream has no room: it holds
// the hash until a consumer makes space rather than dropping it or returning early.
//
// The wait is what stalls block commit when nobody is reading, which publishStalled reports. This covers
// the waiting, not the reporting.
func TestPublishWaitsForRoomOnAFullHashStream(t *testing.T) {
	engineHashChan := make(chan *lthash.BlockHash)
	fm := newFinalizationManager(
		t.Context(), engineHashChan, &lthash.BlockHash{BlockNumber: 0}, 1, 1, hashlog.NewNoOpHashLogger())

	// Registered before the close below so it runs after it: Close drains the engine's stream, which
	// only terminates once that stream is closed.
	defer func() { require.NoError(t, fm.Close()) }()
	defer close(engineHashChan)

	// Depth 1, so this fills the stream and the next publish has nowhere to go.
	fm.publish(&lthash.BlockHash{BlockNumber: 1})

	published := make(chan struct{})
	go func() {
		defer close(published)
		fm.publish(&lthash.BlockHash{BlockNumber: 2})
	}()

	select {
	case <-published:
		t.Fatal("publish returned while the stream was still full")
	case <-time.After(100 * time.Millisecond):
	}

	require.Equal(t, int64(1), (<-fm.HashChan()).BlockNumber)

	select {
	case <-published:
	case <-time.After(30 * time.Second):
		t.Fatal("publish never completed after the stream drained")
	}
	require.Equal(t, int64(2), (<-fm.HashChan()).BlockNumber)
}
