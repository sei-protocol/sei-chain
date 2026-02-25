package cryptosim

import "context"

type TransactionExecutor struct {
	ctx context.Context

	// The database for the benchmark.
	database *Database

	// The address of the fee collection account.
	feeCollectionAddress []byte

	// The Incoming transactions to be executed.
	workChan chan any
}

// A request to flush the transaction executor.
type flushRequest struct {
	doneChan chan struct{}
}

// A single threaded transaction executor.
func NewTransactionExecutor(
	ctx context.Context,
	database *Database,
	feeCollectionAddress []byte,
	queueSize int,
) *TransactionExecutor {
	e := &TransactionExecutor{
		ctx:                  ctx,
		database:             database,
		feeCollectionAddress: feeCollectionAddress,
		workChan:             make(chan any, queueSize),
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
				request.Execute(e.database, e.feeCollectionAddress)
			case flushRequest:
				request.doneChan <- struct{}{}
			}
		}
	}
}
