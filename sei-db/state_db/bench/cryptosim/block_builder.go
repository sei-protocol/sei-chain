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

	// Where writes are accumulated. The builder is the only writer once setup is done, which is what
	// makes the accumulating map safe to keep unsynchronized.
	database *Database

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
	database *Database,
) *blockBuilder {
	return &blockBuilder{
		ctx:           ctx,
		config:        config,
		metrics:       metrics,
		dataGenerator: dataGenerator,
		database:      database,
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
	defer b.dataGenerator.Close()
	for {
		block := b.buildBlock()
		select {
		case <-b.ctx.Done():
			return
		case b.blocksChan <- block:
		}
	}
}

// buildBlock generates a block's transactions and the changeset they produce.
//
// The changeset is built here, rather than accumulated by the executors and converted by the main
// thread at finalize time, because none of that work touches the DB and so none of it belongs on the
// critical path. It is possible here because a transaction's written values are pre-generated random
// bytes that do not depend on anything it reads: the whole block's writes are known before a single
// transaction executes. A real system could not do this, and simulating a parallel execution layer's
// consistency is explicitly not what this benchmark measures — it measures the DB underneath, and
// assumes such a layer exists and is correct.
//
// This goroutine runs BlockChannelCapacity blocks ahead of the consumer, so the work is absorbed by
// slack that already existed. If get_block time stops being near zero, that slack is gone and this
// has become the bottleneck.
func (b *blockBuilder) buildBlock() *block {
	blk := NewBlock(b.config, b.metrics, b.nextBlockNumber, b.config.TransactionsPerBlock)
	b.nextBlockNumber++

	for i := 0; i < b.config.TransactionsPerBlock; i++ {
		// BuildTransaction writes account and contract data of its own for newly created accounts, so
		// the accumulating map is already being filled from this goroutine before writeTransaction adds
		// the transaction's own writes.
		txn, err := BuildTransaction(b.dataGenerator)
		if err != nil {
			fmt.Printf("failed to build transaction: %v\n", err)
			continue
		}
		blk.AddTransaction(txn)

		if err := b.writeTransaction(txn); err != nil {
			fmt.Printf("failed to record transaction writes: %v\n", err)
			continue
		}

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

	// Hand the accumulated writes to the block and take a fresh map for the next one. After this the
	// map belongs to the block and must not be touched again from here: publishing the block is what
	// exposes it to the executors, who read it without locks.
	blk.SetWrites(b.database.HarvestWrites())

	b.dataGenerator.ReportEndOfBlock()

	return blk
}

// writeTransaction records the writes a transaction makes: the two accounts' balances, their two
// ERC20 storage slots, and the fee collection account.
//
// These used to be issued by Execute on the executor threads. They are issued here because the
// values are pre-generated and independent of everything the transaction reads, so making the
// executors pay for them bought nothing. Reads still happen on the executors, which is the part the
// benchmark is measuring.
func (b *blockBuilder) writeTransaction(txn *transaction) error {
	writes := [...]struct {
		key   []byte
		value []byte
	}{
		{txn.srcAccount, txn.newSrcBalance},
		{txn.dstAccount, txn.newDstBalance},
		{txn.srcAccountSlot, txn.newSrcAccountSlot},
		{txn.dstAccountSlot, txn.newDstAccountSlot},
		{b.dataGenerator.FeeCollectionAddress(), txn.newFeeBalance},
	}
	for _, write := range writes {
		if err := b.database.Put(write.key, write.value); err != nil {
			return fmt.Errorf("failed to put %x: %w", write.key, err)
		}
	}
	return nil
}
