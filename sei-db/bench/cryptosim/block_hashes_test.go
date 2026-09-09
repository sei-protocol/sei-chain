package cryptosim

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// publish hands the waiter the hash of one block, as the database's dispatch would.
func publish(t *testing.T, w *blockHashWaiter, blockNumber int64) {
	t.Helper()
	require.NoError(t, w.listen(t.Context(), blockNumber, &lthash.BlockHash{BlockNumber: blockNumber}))
}

// Hashing lags execution, so a benchmark that waited for the first block's hash before committing the
// second would serialize the two and measure something no node does.
func TestTheFirstBlocksRunAheadWithoutTakingAHash(t *testing.T) {
	waiter := newBlockHashWaiter(3, nil)

	// No hash has been published, so any of these taking one would block here rather than return.
	for block := 0; block < 3; block++ {
		require.NoError(t, waiter.awaitBlock())
	}
}

// One hash per block after the window is what holds the lag at the configured distance: taking fewer
// would let the benchmark drift arbitrarily far ahead of hashing.
func TestOneHashIsTakenPerBlockAfterTheWindow(t *testing.T) {
	waiter := newBlockHashWaiter(3, nil)
	for block := int64(1); block <= 4; block++ {
		publish(t, waiter, block)
	}

	// Blocks 1 to 3 fill the window, and the fourth is the first to take a hash — block 1's.
	for block := 0; block < 4; block++ {
		require.NoError(t, waiter.awaitBlock())
	}
	require.Len(t, waiter.hashes, 3, "exactly one hash may be taken per block committed")
	require.Equal(t, int64(2), waiter.nextExpected)
}

// The wait is the point: a database that cannot hash as fast as the benchmark commits has to slow the
// benchmark down, rather than the benchmark reporting a rate the database cannot really sustain.
func TestABlockWaitsForAHashThatHasNotArrived(t *testing.T) {
	waiter := newBlockHashWaiter(1, nil)
	require.NoError(t, waiter.awaitBlock())

	awaited := make(chan error, 1)
	go func() { awaited <- waiter.awaitBlock() }()

	select {
	case <-awaited:
		t.Fatal("the block returned before its hash arrived")
	case <-time.After(100 * time.Millisecond):
	}

	publish(t, waiter, 1)

	select {
	case err := <-awaited:
		require.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("the block never returned after its hash arrived")
	}
}

// The hashes are promised one per block in order. A gap means a block was lost, which the benchmark
// has to report rather than average away.
func TestAGapInTheHashesFailsTheRun(t *testing.T) {
	waiter := newBlockHashWaiter(1, nil)
	publish(t, waiter, 1)

	require.NoError(t, waiter.awaitBlock())
	require.NoError(t, waiter.awaitBlock(), "the first hash taken sets the sequence")

	publish(t, waiter, 3)
	require.ErrorContains(t, waiter.awaitBlock(), "expected the hash of block 2, got block 3")
}

// A database that has stopped hashing will never produce the hash being waited for. The wait has to
// end in a report rather than a hang, or a failed benchmark looks like a slow one.
func TestAHashThatNeverArrivesIsReported(t *testing.T) {
	waiter := newBlockHashWaiter(1, nil)
	waiter.waitTimeout = 50 * time.Millisecond

	require.NoError(t, waiter.awaitBlock())
	require.ErrorContains(t, waiter.awaitBlock(), "the database has stopped hashing")
}

// The listener blocks while the benchmark is too far ahead, and that block runs on the database's own
// goroutine. Shutting the database down has to release it, or its Close never returns.
func TestACancelledContextReleasesABlockedListener(t *testing.T) {
	waiter := newBlockHashWaiter(1, nil)

	// Capacity is the window plus one, so these fill it and the next send has nowhere to go.
	publish(t, waiter, 1)
	publish(t, waiter, 2)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorContains(t,
		waiter.listen(ctx, 3, &lthash.BlockHash{BlockNumber: 3}), "shutting down")
}
