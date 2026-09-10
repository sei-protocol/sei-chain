package gigasim

import (
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// executorBatch is one executor's share of a block, and the group it reports completion to.
type executorBatch struct {
	transactions []*transaction
	done         *sync.WaitGroup
}

// transactionExecutor runs transactions on one goroutine. The benchmark holds a pool of them so that a
// block's transactions execute concurrently, as they would on a node.
//
// Work arrives one share of a block at a time rather than one transaction at a time: a channel
// operation per transaction costs more than the simulated transaction itself.
type transactionExecutor struct {
	state  *executionState
	feeAcc []byte

	workChan chan executorBatch

	phaseTimer *metrics.PhaseTimer
}

// newTransactionExecutor starts an executor's goroutine and registers it with running, which stop
// waits on.
func newTransactionExecutor(
	state *executionState,
	feeAccount []byte,
	metrics *GigasimMetrics,
	running *sync.WaitGroup,
) *transactionExecutor {
	e := &transactionExecutor{
		state:  state,
		feeAcc: feeAccount,
		// One share per block is outstanding at a time, and the buffer is what lets the dispatcher hand
		// out the rest of a block's shares without waiting for this one to be picked up.
		workChan:   make(chan executorBatch, 1),
		phaseTimer: metrics.NewTransactionPhaseTimer(),
	}

	running.Add(1)
	go func() {
		defer running.Done()
		e.mainLoop()
	}()
	return e
}

// submit queues a share of a block for execution.
func (e *transactionExecutor) submit(batch executorBatch) {
	e.workChan <- batch
}

// stop ends the executor once everything already queued has run.
func (e *transactionExecutor) stop() {
	close(e.workChan)
}

// mainLoop executes shares until the work channel closes. It does not watch for cancellation: an
// executor stopping early would leave a block half executed and the dispatcher waiting on it forever, so
// the pool outlives the run loop and teardown stops it.
func (e *transactionExecutor) mainLoop() {
	for batch := range e.workChan {
		for _, txn := range batch.transactions {
			var phaseTimer *metrics.PhaseTimer
			if txn.captureMetrics {
				phaseTimer = e.phaseTimer
			}
			txn.execute(e.state, e.feeAcc, phaseTimer)
		}
		batch.done.Done()
	}
}
