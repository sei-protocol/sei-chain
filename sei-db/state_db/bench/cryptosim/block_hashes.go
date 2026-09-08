package cryptosim

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// hashWaitTimeout bounds how long the benchmark waits for one block's hash. Nothing bounds how long
// hashing may legitimately take, so this is reached only by a database that has stopped hashing
// altogether — which it does by failing, and a failed database will never produce the hash being
// waited for.
const hashWaitTimeout = 5 * time.Minute

// blockHashWaiter holds the hashes a database has published but the benchmark has not taken yet, so
// that the benchmark runs a bounded number of blocks ahead of hashing and then waits.
//
// A benchmark that never took a hash would measure a database that commits blocks without finishing
// them. Taking exactly one hash per block, a fixed number of blocks late, is what turns hashing that
// cannot keep up into time the main thread spends waiting.
//
// The listener half is called from the database's own goroutine; every other method belongs to the
// main thread.
type blockHashWaiter struct {
	// The hashes published but not yet taken. One deeper than the window, because the hash of the
	// block just committed can already be here when the one from lagBlocks back is taken, and a
	// publisher blocking then would be blocking while still inside its allowance.
	hashes chan *lthash.BlockHash

	// How many blocks the benchmark commits before it starts taking hashes, and so how late a block's
	// hash may be.
	lagBlocks int

	// Blocks committed since this waiter was built. Only those publish hashes: the database may have
	// been opened at a height an earlier run reached.
	committed int

	// The block the next hash taken must describe, or 0 until the first one has been taken.
	nextExpected int64

	// How long to wait for one hash before reporting a database that has stopped hashing.
	waitTimeout time.Duration

	metrics *CryptosimMetrics
}

// newBlockHashWaiter returns a waiter that lets the benchmark run lagBlocks ahead of hashing.
func newBlockHashWaiter(lagBlocks int, metrics *CryptosimMetrics) *blockHashWaiter {
	return &blockHashWaiter{
		hashes:      make(chan *lthash.BlockHash, lagBlocks+1),
		lagBlocks:   lagBlocks,
		waitTimeout: hashWaitTimeout,
		metrics:     metrics,
	}
}

// listen takes one block's hash from the database, blocking while the benchmark is further ahead
// than its window allows.
//
// Blocking here is the backpressure: it stops a database finalizing blocks faster than the benchmark
// accepts their hashes. The context is the release, cancelled when the database shuts down, since a
// send with no taker left would otherwise never return.
func (w *blockHashWaiter) listen(ctx context.Context, _ int64, hash *lthash.BlockHash) error {
	select {
	case w.hashes <- hash:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("the database is shutting down: %w", ctx.Err())
	}
}

// awaitBlock accounts for one committed block and, once the benchmark is a full window ahead, takes
// one block's hash, waiting for it to arrive.
func (w *blockHashWaiter) awaitBlock() error {
	w.committed++
	if w.committed <= w.lagBlocks {
		return nil
	}

	hash, err := w.takeHash()
	if err != nil {
		return err
	}

	// The database publishes one hash per block in block order, so the block this describes is
	// predictable, and a hash for any other block means blocks have been lost or repeated.
	if w.nextExpected != 0 && hash.BlockNumber != w.nextExpected {
		return fmt.Errorf("expected the hash of block %d, got block %d", w.nextExpected, hash.BlockNumber)
	}
	w.nextExpected = hash.BlockNumber + 1
	return nil
}

// takeHash waits for the next block's hash, reporting a database that has stopped producing them.
func (w *blockHashWaiter) takeHash() (*lthash.BlockHash, error) {
	w.metrics.SetMainThreadPhase("awaiting_hash")
	startedWaiting := time.Now()
	defer func() {
		w.metrics.RecordBlockHashWaitDuration(time.Since(startedWaiting))
	}()

	timeout := time.NewTimer(w.waitTimeout)
	defer timeout.Stop()

	select {
	case hash := <-w.hashes:
		return hash, nil
	case <-timeout.C:
		return nil, fmt.Errorf("no block hash arrived in %s: the database has stopped hashing, "+
			"%d blocks behind the block just committed", w.waitTimeout, w.lagBlocks)
	}
}
