package flatkv

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// A flush waits behind every block offered before it, so a manager stopped while one of those blocks
// still awaits its hash has to release the caller rather than leave it parked for good.
func TestFinalizationFlushReturnsOnceTheManagerIsStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	// Nothing ever publishes on this, so the block below never gets a hash.
	engineHashChan := make(chan *lthash.BlockHash)
	fm := newFinalizationManager(ctx, engineHashChan, nil, 1, newHashListenerRegistry())

	// Queued directly rather than through Offer: a block's view is only touched once its hash arrives,
	// which here it never does, so this block needs no store behind it.
	require.NoError(t, fm.enqueue(&pendingFinalization{blockNumber: 1}))

	flushed := make(chan error, 1)
	go func() { flushed <- fm.Flush() }()

	select {
	case err := <-flushed:
		t.Fatalf("Flush returned while block 1 was still unhashed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-flushed:
		require.Error(t, err, "a flush released by shutdown reports that it never flushed")
	case <-time.After(30 * time.Second):
		t.Fatal("Flush never returned after the manager was stopped")
	}
}
