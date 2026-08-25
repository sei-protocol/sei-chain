package cryptosim

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
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

// Each transaction selects two accounts and writes four keys. Both are properties of what a transaction
// is, and both are needed to divide a block into ranges: the first to compute which accounts a range
// mints, the second to size its write map.
const selectionsPerTransaction = 2
const writesPerTransaction = 4

// buildRangeResult is one worker's share of a block: its transactions and receipts in the order it
// generated them, plus the writes they made.
type buildRangeResult struct {
	transactions       []*transaction
	receipts           []*evmtypes.Receipt
	writes             map[string]*proto.KVPair
	lastFeeBalance     []byte
	accountsMinted     int64
	coldAccountsMinted int64
}

// buildBlockRanges divides a block's transactions into contiguous runs, generates each on its own
// goroutine, and returns the results in block order.
//
// Which selections mint an account is a function of the selection count alone, so every range's account
// IDs are computed before any of them run and no two ranges can mint the same one. Order is preserved by
// concatenating results in range order rather than by coordinating the workers.
func (b *blockBuilder) buildBlockRanges(blockNumber int64) []buildRangeResult {
	workers := b.config.BlockBuildWorkers
	transactions := b.config.TransactionsPerBlock
	if workers < 2 || transactions < workers {
		return []buildRangeResult{
			b.buildRange(blockNumber, 0, transactions, b.dataGenerator.NextAccountID()),
		}
	}

	// The remainder is spread over the leading ranges, one extra each, so no range is more than one
	// transaction larger than another.
	base := transactions / workers
	remainder := transactions % workers

	results := make([]buildRangeResult, workers)
	firstTransaction := 0
	firstAccountID := b.dataGenerator.NextAccountID()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		count := base
		if i < remainder {
			count++
		}

		wg.Add(1)
		go func(index int, first int, transactionCount int, accountID int64) {
			defer wg.Done()
			results[index] = b.buildRange(blockNumber, first, transactionCount, accountID)
		}(i, firstTransaction, count, firstAccountID)

		firstAccountID += b.dataGenerator.AccountsMintedPerSelections(
			int64(firstTransaction)*selectionsPerTransaction,
			int64(count)*selectionsPerTransaction)
		firstTransaction += count
	}
	wg.Wait()

	return results
}

// buildRange generates one contiguous run of a block's transactions.
func (b *blockBuilder) buildRange(
	blockNumber int64,
	firstTransaction int,
	transactionCount int,
	firstAccountID int64,
) buildRangeResult {

	generator := b.dataGenerator.ForkForSelections(
		int64(firstTransaction)*selectionsPerTransaction,
		int64(transactionCount)*selectionsPerTransaction,
		firstAccountID)

	result := buildRangeResult{
		transactions: make([]*transaction, 0, transactionCount),
		writes:       make(map[string]*proto.KVPair, transactionCount*writesPerTransaction),
	}

	for i := 0; i < transactionCount; i++ {
		// Re-pointed per transaction rather than left to run on: the randomness a transaction draws has
		// to depend on which transaction it is, or a block's contents would depend on how many workers
		// generated it.
		index := int64(firstTransaction + i)
		generator.BeginTransaction(blockNumber*int64(b.config.TransactionsPerBlock)+index, index)

		txn, err := BuildTransaction(generator)
		if err != nil {
			fmt.Printf("failed to build transaction: %v\n", err)
			continue
		}
		result.transactions = append(result.transactions, txn)
		recordTransactionWrites(result.writes, txn)
		result.lastFeeBalance = txn.newFeeBalance

		if b.config.GenerateReceipts {
			rcpt, err := BuildERC20TransferReceiptFromTxn(
				generator.Rand(),
				generator.FeeCollectionAddress(),
				uint64(blockNumber), //nolint:gosec
				//nolint:gosec // G115 - a transaction's index within its block fits in uint32
				uint32(firstTransaction+i),
				txn,
			)
			if err != nil {
				fmt.Printf("failed to build receipt: %v\n", err)
				continue
			}
			result.receipts = append(result.receipts, rcpt)
		}
	}

	result.accountsMinted = generator.AccountsMinted()
	result.coldAccountsMinted = generator.ColdAccountsMinted()
	return result
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
// The transactions themselves are generated across BlockBuildWorkers goroutines; see buildBlockRanges.
// Generating a block had come to cost nearly as much as consuming one, which capped throughput
// regardless of how fast the store underneath was.
func (b *blockBuilder) buildBlock() *block {
	blockNumber := b.nextBlockNumber
	blk := NewBlock(b.config, b.metrics, blockNumber, b.config.TransactionsPerBlock)
	b.nextBlockNumber++

	results := b.buildBlockRanges(blockNumber)

	// Starts from whatever was accumulated outside the ranges — the setup path fills this before the
	// builder starts, and its writes belong to the first block.
	writes := b.database.HarvestWrites()

	// The fee balance of the last transaction to produce one. Every transaction draws a fee balance,
	// because the draw is part of the sequence this block's randomness is defined by, but they all
	// write the same key — so only the last one survives, and only the last one is written.
	var feeBalance []byte
	var accountsMinted int64
	var coldAccountsMinted int64

	for _, result := range results {
		for _, txn := range result.transactions {
			blk.AddTransaction(txn)
		}
		for _, rcpt := range result.receipts {
			blk.AddReceipt(rcpt)
		}
		// Merged in range order, so a key written by more than one range keeps the value the later
		// transaction gave it — the same answer generating them in sequence would reach.
		for key, pair := range result.writes {
			writes[key] = pair
		}
		if result.lastFeeBalance != nil {
			feeBalance = result.lastFeeBalance
		}
		accountsMinted += result.accountsMinted
		coldAccountsMinted += result.coldAccountsMinted
	}

	// Written once, after the transactions, because every transaction writes the same key: issuing it
	// per transaction produced one map entry out of TransactionsPerBlock writes and threw the rest away.
	if feeBalance != nil {
		feeKey := b.dataGenerator.FeeCollectionAddress()
		writes[string(feeKey)] = &proto.KVPair{Key: feeKey, Value: feeBalance}
	}

	// The forks minted from ranges of IDs reserved before they ran; this is where those ranges are
	// accounted for, so the next block's arithmetic starts from the right place.
	b.dataGenerator.AdoptForkResults(accountsMinted, coldAccountsMinted)

	blk.SetBlockAccountStats(
		b.dataGenerator.NextAccountID(),
		b.dataGenerator.NumberOfColdAccounts(),
		b.dataGenerator.NextErc20ContractID())

	// After this the map belongs to the block and must not be touched again from here: publishing the
	// block is what exposes it to the executors, who read it without locks.
	blk.SetWrites(writes)

	b.dataGenerator.ReportEndOfBlock()

	return blk
}

// writeTransaction records the writes a transaction makes: the two accounts' balances, their two
// ERC20 storage slots. The fee collection account is written once per block instead: see buildBlock.
//
// These used to be issued by Execute on the executor threads. They are issued here because the
// values are pre-generated and independent of everything the transaction reads, so making the
// executors pay for them bought nothing. Reads still happen on the executors, which is the part the
// benchmark is measuring.
func recordTransactionWrites(writes map[string]*proto.KVPair, txn *transaction) {
	pairs := [...]struct {
		key   []byte
		value []byte
	}{
		{txn.srcAccount, txn.newSrcBalance},
		{txn.dstAccount, txn.newDstBalance},
		{txn.srcAccountSlot, txn.newSrcAccountSlot},
		{txn.dstAccountSlot, txn.newDstAccountSlot},
	}
	for _, pair := range pairs {
		writes[string(pair.key)] = &proto.KVPair{Key: pair.key, Value: pair.value}
	}
}
