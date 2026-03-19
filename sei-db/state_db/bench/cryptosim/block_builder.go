package cryptosim

import (
	"context"
	"fmt"
)

// A builder for blocks of transactions.
type blockBuilder struct {
	ctx context.Context

	config *CryptoSimConfig

	// Metrics for the benchmark.
	metrics *CryptosimMetrics

	// Produces random data.
	dataGenerator *DataGenerator

	// Blocks are sent to this channel.
	blocksChan chan *block

	// The next block number to be used.
	nextBlockNumber int64
}

// Asyncronously produces blocks of transactions.
func NewBlockBuilder(
	ctx context.Context,
	config *CryptoSimConfig,
	metrics *CryptosimMetrics,
	dataGenerator *DataGenerator,
) *blockBuilder {
	return &blockBuilder{
		ctx:           ctx,
		config:        config,
		metrics:       metrics,
		dataGenerator: dataGenerator,
		blocksChan:    make(chan *block, config.BlockChannelCapacity),
	}
}

// Starts the block builder. This should not be called until all other threads are done using the data generator,
// as the data generator is not thread-safe.
func (b *blockBuilder) Start() {
	go b.mainLoop()
}

// Builds blocks and sends them to the blocks channel.
func (b *blockBuilder) mainLoop() {
	for {
		block := b.buildBlock()
		select {
		case <-b.ctx.Done():
			return
		case b.blocksChan <- block:
		}
	}
}

func (b *blockBuilder) buildBlock() *block {
	blk := NewBlock(b.config, b.metrics, b.nextBlockNumber, b.config.TransactionsPerBlock)
	b.nextBlockNumber++

	for i := 0; i < b.config.TransactionsPerBlock; i++ {
		txn, err := BuildTransaction(b.dataGenerator)
		if err != nil {
			fmt.Printf("failed to build transaction: %v\n", err)
			continue
		}
		blk.AddTransaction(txn)

		if b.config.GenerateReceipts {
			receipt, err := BuildERC20TransferReceiptFromTxn(
				b.dataGenerator.Rand(),
				b.dataGenerator.FeeCollectionAddress(),
				uint64(blk.BlockNumber()), //nolint:gosec
				uint32(i),                 //nolint:gosec
				txn,
			)
			if err != nil {
				fmt.Printf("failed to build receipt: %v\n", err)
				continue
			}
			blk.AddReceipt(receipt)
		}
	}

	blk.SetBlockAccountStats(
		b.dataGenerator.NextAccountID(),
		b.dataGenerator.NumberOfColdAccounts(),
		b.dataGenerator.NextErc20ContractID())

	b.dataGenerator.ReportEndOfBlock()

	return blk
}
