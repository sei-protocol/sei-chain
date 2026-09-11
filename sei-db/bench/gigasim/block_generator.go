package gigasim

import (
	"context"
	"fmt"
	"hash"

	"golang.org/x/crypto/sha3"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// simulatedBlock is one block's worth of work: the transactions the execution phase runs, the payload
// the block store persists, and the receipts that execution is taken to have produced.
type simulatedBlock struct {
	// The height this block commits at, shared by the block store, the state DB and the receipt store.
	number int64

	// The transactions the execution phase runs against state. They are executed in parallel across the
	// executor pool, so a block's transaction count is also its degree of parallelism.
	transactions []*transaction

	// The receipts written to the receipt store, empty when receipts are disabled.
	receipts []*evmtypes.Receipt

	// The transaction bytes the block store persists. These stand in for encoded transactions, which
	// the block store holds as opaque bytes.
	payload [][]byte

	// The identifier counters as of this block, committed alongside it so that a resumed run mints
	// identifiers where this one stopped. They travel with the block because the account model that
	// produced them keeps moving on the generator's goroutine.
	counters identifierCounters
}

// identifierCounters is the account and contract population recorded in state at a given height.
type identifierCounters struct {
	nextAccountID       int64
	nextErc20ContractID int64
}

// payloadBytes is the block's size on the block store's write path.
func (b *simulatedBlock) payloadBytes() int64 {
	return payloadBytes(b.payload)
}

// blockGenerator produces blocks on its own goroutine, persists each one to the block ledger, and hands
// it to the execution phase through a channel. Storing a block and executing it therefore proceed
// concurrently, the way a node's consensus and execution paths do.
//
// It owns the account model and the block store writer, neither of which is thread safe, and so it also
// owns block numbering: the number a block will commit at has to be known while it is being written.
type blockGenerator struct {
	ctx    context.Context
	cancel context.CancelFunc
	config *GigasimConfig

	accounts *accountModel
	blocks   *blockStoreWriter

	// The height the next block generated commits at.
	next int64

	// The number of blocks written to the ledger, which drives the flush cadence.
	written int64

	// Enforces a maximum block rate, or nil when generation runs unthrottled.
	rateLimiter *rate.Limiter

	blocksChan chan *simulatedBlock

	// The error that stopped generation, if one did. Written before blocksChan is closed and read only
	// once that channel has drained, which is what orders the two.
	failure error

	// The keccak hasher every receipt's bloom is built with, held here because only this goroutine
	// builds receipts.
	bloomHasher hash.Hash

	// This goroutine's share of a block's critical path: building it and storing it.
	lifecycle *metrics.PhaseTimer

	// Breaks the block store write into the records written and the flush behind them, subdividing
	// this loop's write_block phase. Shared with the writer, which names each record.
	blockStoreWrite *metrics.PhaseTimer

	metrics *GigasimMetrics
}

// newBlockGenerator creates a generator without starting it. Generation must not begin until setup has
// finished, because both draw on the account model and both write to the block ledger.
func newBlockGenerator(
	ctx context.Context,
	cancel context.CancelFunc,
	config *GigasimConfig,
	accounts *accountModel,
	blocks *blockStoreWriter,
	gigasimMetrics *GigasimMetrics,
	blockStoreWrite *metrics.PhaseTimer,
) *blockGenerator {
	var rateLimiter *rate.Limiter
	if config.MaxBlocksPerSecond > 0 {
		rateLimiter = rate.NewLimiter(rate.Limit(config.MaxBlocksPerSecond), 1)
	}

	return &blockGenerator{
		ctx:             ctx,
		cancel:          cancel,
		config:          config,
		accounts:        accounts,
		blocks:          blocks,
		rateLimiter:     rateLimiter,
		blocksChan:      make(chan *simulatedBlock, config.MaxPendingExecutionQueueSize),
		bloomHasher:     sha3.NewLegacyKeccak256(),
		lifecycle:       gigasimMetrics.NewBlockProducingTimer(),
		blockStoreWrite: blockStoreWrite,
		metrics:         gigasimMetrics,
	}
}

// Start begins generating blocks at the given height.
func (g *blockGenerator) Start(first int64) {
	g.next = first
	go g.mainLoop()
}

// mainLoop generates and stores blocks until the run is cancelled.
//
// Closing the channel is how the execution phase learns that no more blocks are coming, and it happens
// last: everything the generator owns is released before the consumer can observe the close and start
// tearing the stores down.
func (g *blockGenerator) mainLoop() {
	defer close(g.blocksChan)
	// The account model holds the canned random buffer, which is the benchmark's largest allocation.
	defer g.accounts.Close()
	defer g.finalFlush()
	// Registered last so it runs first, closing the phase in flight before teardown begins. Teardown
	// itself is a one-off that would distort a phase it was charged to.
	defer g.lifecycle.Reset()

	for {
		if g.ctx.Err() != nil {
			return
		}
		g.lifecycle.SetPhase("throttle")
		g.throttle()

		g.lifecycle.SetPhase("generate")
		block, err := g.buildBlock()
		if err != nil {
			g.abort(fmt.Errorf("failed to generate block %d: %w", g.next, err))
			return
		}
		g.lifecycle.SetPhase("write_block")
		if err := g.storeBlock(block); err != nil {
			g.abort(err)
			return
		}
		// The block is finished, so what follows is this goroutine waiting on execution rather than a
		// stage the block passes through. It is named rather than dropped: the phases are read as a
		// pie, which renormalizes to 100%, so time left out inflates every other slice.
		g.lifecycle.SetPhase("wait_for_execution")

		// A block already in the ledger has to reach execution, so this hand-off is not abandoned on
		// cancellation: the consumer drains the queue before it closes the stores.
		g.metrics.StageBlock(g.blocksChan, block)
	}
}

// abort records the error that stopped generation and ends the run. The consumer reports it once the
// queue has drained, so that a run which died producing blocks is visible in the exit code rather than
// only on the console.
func (g *blockGenerator) abort(err error) {
	g.failure = err
	g.cancel()
}

// buildBlock assembles the next block: its transactions, the payload standing in for their encoded
// form, and their receipts when receipts are enabled.
func (g *blockGenerator) buildBlock() (*simulatedBlock, error) {

	number := g.next
	g.next++

	count := g.config.TransactionsPerBlock

	// Each of these is one allocation for the whole block. Allocating per transaction instead puts
	// thousands of objects a second in front of the collector, which the storage stack then pays for.
	transactions := make([]transaction, count)
	block := &simulatedBlock{
		number:       number,
		transactions: make([]*transaction, count),
		payload:      make([][]byte, count),
	}
	var receipts *receiptBuffer
	if g.config.EnableReceiptStore {
		receipts = newReceiptBuffer(count, g.bloomHasher)
		block.receipts = receipts.receipts
	}

	for i := range count {
		txn := &transactions[i]
		if err := buildTransaction(txn, g.accounts); err != nil {
			return nil, fmt.Errorf("failed to build transaction %d: %w", i, err)
		}
		block.transactions[i] = txn
		block.payload[i] = g.accounts.Rand().Bytes(g.config.BytesPerTransaction)

		if receipts != nil {
			receipts.build(i, g.accounts.Rand(), txn, number)
		}
	}

	// Accounts minted for this block become legal read targets once it is complete.
	g.accounts.ReportEndOfBlock()
	block.counters = g.accounts.Counters()
	return block, nil
}

// storeBlock appends a block to the ledger and flushes on the configured cadence.
func (g *blockGenerator) storeBlock(block *simulatedBlock) error {
	// The writer names the record it is on; this closes whichever it ended on, so that these phases
	// cover the same window as the generator's write_block phase and no more.
	defer g.blockStoreWrite.Reset()

	if err := g.blocks.writeBlock(block.number, block.payload); err != nil {
		return err
	}
	g.written++

	if g.config.FlushIntervalBlocks <= 0 || g.written%int64(g.config.FlushIntervalBlocks) != 0 {
		return nil
	}
	g.blockStoreWrite.SetPhase("flush")
	return g.flush()
}

// finalFlush pushes the last blocks to disk as generation ends, so that the consumer can close the
// stores without reaching for a writer it does not own.
func (g *blockGenerator) finalFlush() {
	if err := g.flush(); err != nil {
		fmt.Printf("%v\n", err)
	}
}

// flush pushes the block ledger's buffered writes to disk and records that it happened.
func (g *blockGenerator) flush() error {
	if err := g.blocks.Flush(); err != nil {
		return err
	}
	g.metrics.ReportFlush()
	return nil
}

// throttle holds generation to the configured block rate. A run without one waits for nothing here.
func (g *blockGenerator) throttle() {
	if g.rateLimiter == nil {
		return
	}
	_ = g.rateLimiter.Wait(g.ctx)
}
