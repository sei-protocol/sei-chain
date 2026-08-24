package cryptosim

import (
	"context"
	"log"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

type TransactionExecutor struct {
	ctx    context.Context
	cancel context.CancelFunc
	config *CryptoSimConfig

	// The database for the benchmark.
	database *Database

	// The address of the fee collection account.
	feeCollectionAddress []byte

	// The Incoming transactions to be executed.
	workChan chan any

	// Used to time the execution of transactions.
	phaseTimer *metrics.PhaseTimer
}

// A request to flush the transaction executor.
type flushRequest struct {
	doneChan chan struct{}
}

// A single threaded transaction executor.
func NewTransactionExecutor(
	ctx context.Context,
	cancel context.CancelFunc,
	config *CryptoSimConfig,
	database *Database,
	feeCollectionAddress []byte,
	queueSize int,
	metrics *CryptosimMetrics,
) *TransactionExecutor {
	e := &TransactionExecutor{
		ctx:                  ctx,
		cancel:               cancel,
		config:               config,
		database:             database,
		feeCollectionAddress: feeCollectionAddress,
		workChan:             make(chan any, queueSize),
		phaseTimer:           metrics.GetTransactionPhaseTimerInstance(),
	}

	go e.mainLoop()

	return e
}

// Schedule a run of transactions for execution.
//
// A whole range is handed over in one message rather than one message per transaction: at thousands of
// transactions per block, the channel sends and receives were themselves a measurable share of the main
// thread's time and of this goroutine's. The slice is owned by the block and is only read here.
func (e *TransactionExecutor) ScheduleRange(txns []*transaction) {
	select {
	case <-e.ctx.Done():
	case e.workChan <- txns:
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
			case []*transaction:

				if e.config.DisableTransactionExecution {
					continue
				}

				for _, txn := range request {
					e.execute(txn)
				}
			case flushRequest:
				request.doneChan <- struct{}{}
			}
		}
	}
}

// execute runs one transaction. A failure stops the benchmark: a transaction that cannot execute means
// the database is not answering, and whatever ran afterwards would not be measuring anything.
func (e *TransactionExecutor) execute(txn *transaction) {
	var phaseTimer *metrics.PhaseTimer
	if txn.ShouldCaptureMetrics() {
		phaseTimer = e.phaseTimer
	}

	if err := txn.Execute(e.database, e.feeCollectionAddress, phaseTimer); err != nil {
		log.Printf("transaction execution error: %v", err)
		e.cancel()
	}
}
