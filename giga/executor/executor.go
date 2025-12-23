package executor

import (
	"context"

	"github.com/sei-protocol/sei-chain/giga/executor/tracks"
)

type Config struct {
	StatelessWorkerCount   int
	ReceiptSinkWorkerCount int
	blocksBuffer           int
	receiptsBuffer         int
	changeSetsBuffer       int
}

func RunExecutor[RawBlock, Block tracks.Identifiable, Receipt, ChangeSet any](
	ctx context.Context,
	config Config,
	rawBlocks <-chan RawBlock, // channel where the consensus layer produces to
	statelessFn func(RawBlock) Block, // TODO: function that checks sig, nonce, etc.
	schedulerFn func(Block, chan<- Receipt) ChangeSet, // TODO: main processing logic
	commitFn func(ChangeSet), // TODO: commit to working set after a block is fully done
	receiptSinkFn func(Receipt), // TODO: persist receipts to disk
	historicalStateSinkFn func(ChangeSet), // TODO: persist historical state to disk
	prevBlock uint64, // TODO: the last executed block id,
) {
	blocks := make(chan Block, config.blocksBuffer)
	receipts := make(chan Receipt, config.receiptsBuffer)
	changeSets := make(chan ChangeSet, config.changeSetsBuffer)

	// spins off `StatelessWorkerCount` goroutines.
	statelessTrack := tracks.NewStatelessTrack(rawBlocks, blocks, statelessFn, config.StatelessWorkerCount, prevBlock)
	// spins off 1 goroutine.
	executionTrack := tracks.NewExecutionTrack(blocks, receipts, changeSets, schedulerFn, commitFn)
	// spins off `ReceiptSinkWorkerCount` goroutines.
	receiptSinkTrack := tracks.NewReceiptSinkTrack(receipts, receiptSinkFn, config.ReceiptSinkWorkerCount)
	// spins off 1 goroutine.
	historicalStateSinkTrack := tracks.NewHistoricalStateSinkTrack(changeSets, historicalStateSinkFn)

	statelessTrack.Start()
	executionTrack.Start()
	receiptSinkTrack.Start()
	historicalStateSinkTrack.Start()

	<-ctx.Done()

	statelessTrack.Stop()
	executionTrack.Stop()
	receiptSinkTrack.Stop()
	historicalStateSinkTrack.Stop()
}
