package gigasim

import (
	"context"
	"fmt"

	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
)

// maxWritesPerTransaction is the most keys one transfer of any kind writes: an ERC20 transfer writes the
// sender's account and both balance slots, and a native transfer both accounts. The fee account is
// written once per block rather than once per transaction, so it is not counted here.
const maxWritesPerTransaction = 3

// simulatedBlock is one superblock's worth of work: the transactions the execution phase runs, the
// lane blocks the block store persists, and the receipts that execution is taken to have produced.
// A superblock of one lane block is a single block.
type simulatedBlock struct {
	// The height this superblock commits at in the state DB and the receipt store.
	number int64

	// The block store height of lanePayloads[0]. The lane blocks occupy the heights from here on.
	firstLaneBlock int64

	// The transactions the execution phase runs against state. They are executed in parallel across the
	// executor pool, so a superblock's transaction count is also its degree of parallelism.
	transactions []*transaction

	// The receipts written to the receipt store, in the form it takes them, empty when receipts are
	// disabled. They are marshaled here because execution does not change them and its loop paces
	// the run.
	receiptRecords []receipt.ReceiptRecord

	// What those records marshaled to, which the run reports as bytes written.
	receiptBytes int64

	// The transaction bytes of each lane block, packed as ledgerPayload lays them out, in the order
	// they are written. These stand in for encoded transactions, which the block store holds as opaque
	// bytes.
	lanePayloads [][][]byte

	// The state changes this block makes, in the form the state DB takes, carrying the identifier
	// counters as of this block so that a resumed run mints identifiers where this one stopped.
	//
	// Staged when the block is generated rather than by the executors: a transaction's written values
	// are drawn up front and depend on nothing it reads, so the whole block's writes are known before
	// any of it executes. Executing it is then reads alone, and committing it has nothing to convert.
	// A real system could not do this; simulating an execution layer's consistency is explicitly not
	// what this benchmark measures.
	writes blockWrites
}

// identifierCounters is the account and contract population recorded in state at a given height.
type identifierCounters struct {
	nextAccountID       int64
	nextErc20ContractID int64
}

// payloadBytes is the superblock's size on the block store's write path, summed across its lane blocks.
func (b *simulatedBlock) payloadBytes() int64 {
	var total int64
	for _, payload := range b.lanePayloads {
		total += payloadBytes(payload)
	}
	return total
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

	// Stages the writes of the block being built. Reused across blocks: draining it hands the pairs to
	// the block and leaves the batch empty.
	batch *stateBatch

	// The height the next superblock commits at in the state DB and the receipt store.
	next int64

	// The block store height the next lane block is written at.
	nextLane int64

	// The number of blocks written to the ledger, which drives the flush cadence.
	written int64

	// Enforces a maximum transaction rate, or nil when generation runs unthrottled.
	rateLimiter *rate.Limiter

	blocksChan chan *simulatedBlock

	// The error that stopped generation, if one did. Written before blocksChan is closed and read only
	// once that channel has drained, which is what orders the two.
	failure error

	// The keccak hasher every receipt's bloom is built with, held here because only this goroutine
	// builds receipts.
	receiptCache *receiptCache

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
	if config.MaxTps > 0 {
		// The burst is one superblock, since throttle waits for a whole superblock's transactions at once.
		rateLimiter = rate.NewLimiter(rate.Limit(config.MaxTps), config.transactionsPerSuperblock())
	}

	return &blockGenerator{
		ctx:             ctx,
		cancel:          cancel,
		config:          config,
		accounts:        accounts,
		blocks:          blocks,
		batch:           newStateBatch(maxWritesPerTransaction*config.transactionsPerSuperblock() + 1),
		rateLimiter:     rateLimiter,
		blocksChan:      make(chan *simulatedBlock, config.MaxPendingExecutionQueueSize),
		receiptCache:    newReceiptCache(),
		lifecycle:       gigasimMetrics.NewBlockProducingTimer(),
		blockStoreWrite: blockStoreWrite,
		metrics:         gigasimMetrics,
	}
}

// Start begins generating blocks at the given height.
func (g *blockGenerator) Start(first int64) {
	g.next = first
	g.nextLane = g.config.firstLaneBlock(first)
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
		g.metrics.QueueForExecution(g.blocksChan, block)
	}
}

// abort records the error that stopped generation and ends the run. The consumer reports it once the
// queue has drained, so that a run which died producing blocks is visible in the exit code rather than
// only on the console.
//
// The first error is the one kept. Teardown writes of its own follow a failure and usually fail the
// same way, so a later one would bury the cause under its consequence.
func (g *blockGenerator) abort(err error) {
	if g.failure == nil {
		g.failure = err
	}
	g.cancel()
}

// buildBlock assembles the next superblock: its transactions, one payload per lane block standing in
// for their encoded form, and their receipts when receipts are enabled.
func (g *blockGenerator) buildBlock() (*simulatedBlock, error) {

	number := g.next
	g.next++

	lanes := g.config.LaneBlocksPerSuperblock
	perLane := g.config.TransactionsPerBlock
	count := perLane * lanes

	// Each of these is one allocation for the whole superblock. Allocating per transaction instead puts
	// thousands of objects a second in front of the collector, which the storage stack then pays for.
	transactions := make([]transaction, count)
	block := &simulatedBlock{
		number:         number,
		firstLaneBlock: g.nextLane,
		transactions:   make([]*transaction, count),
		lanePayloads:   make([][][]byte, lanes),
	}
	g.nextLane += int64(lanes)

	var receipts *receiptBuffer
	if g.config.EnableReceiptStore {
		//nolint:gosec // G115 - validation keeps the gas positive
		receipts = newReceiptBuffer(count, g.receiptCache, uint64(g.config.gasUsedBy(1)))
		block.receiptRecords = receipts.records
	}

	for lane := range lanes {
		for i := range perLane {
			index := lane*perLane + i
			txn := &transactions[index]
			if err := buildTransaction(txn, g.accounts); err != nil {
				return nil, fmt.Errorf("failed to build transaction %d: %w", index, err)
			}
			block.transactions[index] = txn
			g.stageTransactionWrites(txn)

			if receipts != nil {
				if err := receipts.build(index, g.accounts.Rand(), txn, number); err != nil {
					return nil, err
				}
			}
		}
		block.lanePayloads[lane] = ledgerPayload(g.accounts.Rand(), g.config)
	}
	if receipts != nil {
		block.receiptBytes = receipts.encodedBytes
	}

	// Staged once, after the transactions, because they all name this one key: every transaction draws
	// a fee balance, since the draw is part of the sequence the superblock's randomness is defined by,
	// but only the last draw survives into the superblock.
	g.batch.Put(g.accounts.FeeCollectionAddress(), transactions[count-1].newFeeBalance)

	// Accounts minted for this block become legal read targets once it is complete.
	g.accounts.ReportEndOfBlock()
	block.writes = g.batch.drainToChangeSet(g.accounts.Counters())
	return block, nil
}

// stageTransactionWrites stages the writes one transfer makes: the sender's balance, then either the
// recipient's balance for a native transfer or both token balance slots for an ERC20 transfer. The fee
// account is staged once per superblock instead; see buildBlock().
func (g *blockGenerator) stageTransactionWrites(txn *transaction) {
	g.batch.Put(txn.srcAccount, txn.newSrcBalance)
	if txn.kind == nativeTransfer {
		g.batch.Put(txn.dstAccount, txn.newDstBalance)
		return
	}
	g.batch.Put(txn.srcAccountSlot, txn.newSrcAccountSlot)
	g.batch.Put(txn.dstAccountSlot, txn.newDstAccountSlot)
}

// storeBlock appends a superblock's lane blocks to the ledger and flushes on the configured cadence.
func (g *blockGenerator) storeBlock(block *simulatedBlock) error {
	// The writer names the record it is on; this closes whichever it ended on, so that these phases
	// cover the same window as the generator's write_block phase and no more.
	defer g.blockStoreWrite.Reset()

	for i, payload := range block.lanePayloads {
		if err := g.blocks.writeBlock(block.firstLaneBlock+int64(i), payload); err != nil {
			return err
		}
		g.written++
		if g.config.FlushIntervalBlocks <= 0 || g.written%int64(g.config.FlushIntervalBlocks) != 0 {
			continue
		}
		g.blockStoreWrite.SetPhase("flush")
		if err := g.flush(); err != nil {
			return err
		}
	}
	return nil
}

// finalFlush pushes the last blocks to disk as generation ends, so that the consumer can close the
// stores without reaching for a writer it does not own.
//
// A failure here aborts the run rather than only printing. These are the last blocks the generator
// writes, so losing them is what leaves the ledger short of the state DB, and an exit code of 0 would
// report that as a clean run. It is safe to record: this runs before blocksChan is closed, which is
// the edge ordering the consumer's read of the failure.
func (g *blockGenerator) finalFlush() {
	if err := g.flush(); err != nil {
		g.abort(err)
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

// throttle holds generation to the configured transaction rate, waiting for the next block's
// transactions. A run without one waits for nothing here.
func (g *blockGenerator) throttle() {
	if g.rateLimiter == nil {
		return
	}
	_ = g.rateLimiter.WaitN(g.ctx, g.config.transactionsPerSuperblock())
}
