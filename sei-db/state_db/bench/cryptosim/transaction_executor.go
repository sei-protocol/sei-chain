package cryptosim

import (
	"context"
	"log"
)

type TransactionExecutor struct {
	ctx    context.Context
	cancel context.CancelFunc

	// The database for the benchmark.
	database *Database

	// The address of the fee collection account.
	feeCollectionAddress []byte

	// The Incoming transactions to be executed.
	workChan chan any

	// Used to time the execution of transactions.
	phaseTimer *PhaseTimer
}

// A request to flush the transaction executor.
type flushRequest struct {
	doneChan chan struct{}
}

// A single threaded transaction executor.
func NewTransactionExecutor(
	ctx context.Context,
	cancel context.CancelFunc,
	database *Database,
	feeCollectionAddress []byte,
	queueSize int,
	metrics *CryptosimMetrics,
) *TransactionExecutor {
	e := &TransactionExecutor{
		ctx:                  ctx,
		database:             database,
		feeCollectionAddress: feeCollectionAddress,
		workChan:             make(chan any, queueSize),
		phaseTimer:           metrics.GetTransactionPhaseTimerInstance(),
	}

	go e.mainLoop()

	return e
}

// Schedule a transaction for execution.
func (e *TransactionExecutor) ScheduleForExecution(txn *transaction) {
	select {
	case <-e.ctx.Done():
	case e.workChan <- txn:
	}
}

// Blocks until all currently queued transactions have been executed.
func (e *TransactionExecutor) Flush() {

	request := flushRequest{doneChan: make(chan struct{}, 1)}

	select {
	case <-e.ctx.Done():
	case e.workChan <- request:
	}

	select {
	case <-request.doneChan:
	case <-e.ctx.Done():
	}
}

func (e *TransactionExecutor) mainLoop() {

	for {
		select {
		case <-e.ctx.Done():
			return
		case request := <-e.workChan:
			switch request := request.(type) {
			case *transaction:

				var phaseTimer *PhaseTimer
				if request.ShouldCaptureMetrics() {
					phaseTimer = e.phaseTimer
				}

				if err := request.Execute(e.database, e.feeCollectionAddress, phaseTimer); err != nil {
					log.Printf("transaction execution error: %v", err)
					e.cancel()
				}
			case flushRequest:
				request.doneChan <- struct{}{}
			}
		}
	}
}
