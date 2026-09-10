// Package gigasim benchmarks a Giga node's whole storage stack end to end, driving the block store,
// the state DB and the receipt store from one simulated block pipeline.
package gigasim

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// The benchmark runner for the gigasim benchmark.
type GigaSim struct {
	// The run scope, covering the main loop, the generator and the executor pool. Cancelling it stops
	// block production; the stores stay open until teardown.
	ctx    context.Context
	cancel context.CancelFunc

	// Releases the scope the databases were opened under. Called only once the run loop has stopped, so
	// that the block it was in the middle of still had somewhere to write.
	stopStorage context.CancelFunc

	config *GigasimConfig

	// Owns every database the benchmark drives, along with the checkpoint schedule and the prune cycle
	// running above them.
	storage *bootstrap.GigaStorageManager

	// Appends to the block ledger. Setup writes through it directly; once generation starts it belongs
	// to the generator's goroutine and must not be touched from here.
	blocks *blockStoreWriter

	state     *executionState
	receipts  *receiptWriter
	accounts  *accountModel
	generator *blockGenerator

	// The executor pool a block's transactions are spread across.
	executors []*transactionExecutor

	// Counts the shares of the current block still executing, and the executor goroutines still alive.
	executing        sync.WaitGroup
	executorsRunning sync.WaitGroup

	metrics *GigasimMetrics

	// Console reporting state.
	consoleUpdatePeriod         time.Duration
	lastConsoleUpdateTime       time.Time
	lastConsoleUpdateBlockCount int64
	startTimestamp              time.Time
	totalTransactions           int64
	totalPayloadBytes           int64

	// Progress, published atomically because callers observe it while the main loop advances it.
	// totalBlocks counts measured blocks only, so it excludes the ones setup wrote.
	totalBlocks  atomic.Int64
	highestBlock atomic.Int64

	// The first error the run hit, which Close reports. Written only from the run goroutine and read
	// only after closeChan has been received from, which is what orders the two.
	runErr error

	// A message is sent on this channel when the benchmark is fully stopped.
	closeChan chan struct{}

	// Suspend/resume toggle channel.
	suspendChan chan bool
}

// Creates a new gigasim benchmark runner. The returned benchmark is already running.
func NewGigaSim(
	ctx context.Context,
	config *GigasimConfig,
	metrics *GigasimMetrics,
) (*GigaSim, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if err := resolveDirectories(config); err != nil {
		return nil, err
	}

	fmt.Printf("Running gigasim benchmark from data directory: %s\n", config.DataDir)
	fmt.Printf("Logs are being routed to: %s\n", config.LogDir)
	fmt.Printf("The historical state store is %s and the receipt store is %s.\n",
		enabledLabel(config.EnableStateStore), enabledLabel(config.EnableReceiptStore))

	storageConfig, err := config.storageConfig()
	if err != nil {
		return nil, err
	}

	// The databases are opened under a scope of their own rather than the caller's. An interrupt has to
	// stop block production while leaving the stores open, because the blocks already staged are drained
	// through them on the way out; a cancelled store would fail those writes and leave the ledger ahead
	// of the state DB. Only teardown releases this scope.
	storageCtx, stopStorage := context.WithCancel(context.Background())

	fmt.Printf("Opening storage.\n")
	storage, err := bootstrap.NewGigaStorageManager(storageCtx, storageConfig)
	if err != nil {
		stopStorage()
		return nil, fmt.Errorf("failed to open storage: %w", err)
	}

	// The run scope covers the main loop, the generator and the executor pool, and follows the caller's
	// context so that an interrupt reaches them and only them.
	runCtx, cancel := context.WithCancel(ctx)

	g, err := assemble(runCtx, cancel, config, metrics, storage)
	if err != nil {
		// A failure here has no pipeline to unwind, so it releases the stores directly. They hold
		// exclusive locks on their directories, so a handle left open makes an in-process retry fail to
		// reopen them.
		cancel()
		if closeErr := storage.Close(); closeErr != nil {
			fmt.Printf("failed to close storage during error recovery: %v\n", closeErr)
		}
		stopStorage()
		return nil, err
	}
	g.stopStorage = stopStorage

	// Setup runs over the assembled pipeline, so its failure unwinds through the same ordered shutdown
	// a completed run uses rather than a second copy of that order.
	if err := g.setup(); err != nil {
		if closeErr := g.closePipeline(); closeErr != nil {
			fmt.Printf("failed to close storage during error recovery: %v\n", closeErr)
		}
		return nil, err
	}

	g.startTimestamp = time.Now()
	g.lastConsoleUpdateTime = g.startTimestamp

	// Generation starts only now: it draws on the account model, which setup owns until it finishes.
	g.generator.Start(g.highestBlock.Load() + 1)

	go g.run()
	return g, nil
}

// assemble builds the pipeline over already-opened storage, leaving it stopped.
func assemble(
	ctx context.Context,
	cancel context.CancelFunc,
	config *GigasimConfig,
	metrics *GigasimMetrics,
	storage *bootstrap.GigaStorageManager,
) (*GigaSim, error) {
	state, err := newExecutionState(config, storage.StateDB(), metrics)
	if err != nil {
		return nil, err
	}

	blocks := newBlockStoreWriter(storage.BlockStore(), config, metrics)
	nextBlock, err := agreedNextBlockNumber(state, blocks)
	if err != nil {
		state.Close()
		return nil, err
	}

	rand := crand.NewCannedRandom(config.CannedRandomSize, config.Seed)
	accounts := newAccountModel(config, state, rand, metrics)

	var receipts *receiptWriter
	if config.EnableReceiptStore {
		receipts = newReceiptWriter(storage.ReceiptDB(), metrics)
	}

	g := &GigaSim{
		ctx:                 ctx,
		cancel:              cancel,
		config:              config,
		storage:             storage,
		blocks:              blocks,
		state:               state,
		receipts:            receipts,
		accounts:            accounts,
		metrics:             metrics,
		consoleUpdatePeriod: time.Duration(config.ConsoleUpdateIntervalSeconds * float64(time.Second)),
		closeChan:           make(chan struct{}, 1),
		suspendChan:         make(chan bool, 1),
	}
	g.highestBlock.Store(nextBlock - 1)
	g.startExecutors(state, accounts.FeeCollectionAddress())
	g.generator = newBlockGenerator(ctx, cancel, config, accounts, blocks, metrics)
	return g, nil
}

// startExecutors builds the executor pool a block's transactions are spread across.
func (g *GigaSim) startExecutors(state *executionState, feeAccount []byte) {
	count := max(1, int(g.config.ThreadsPerCore*float64(runtime.NumCPU()))+g.config.ConstantThreadCount)
	fmt.Printf("Starting %d transaction executors.\n", count)

	g.executors = make([]*transactionExecutor, 0, count)
	for range count {
		g.executors = append(g.executors,
			newTransactionExecutor(state, feeAccount, g.metrics, &g.executorsRunning))
	}
}

// stopExecutors ends the executor pool and waits for the goroutines to leave, which has to happen
// before the stores they read and write are closed.
func (g *GigaSim) stopExecutors() {
	for _, executor := range g.executors {
		executor.stop()
	}
	g.executorsRunning.Wait()
}

// agreedNextBlockNumber returns the height the next block commits at, refusing a data directory whose
// block store and state DB disagree on where they left off.
//
// Generation runs ahead of execution during a run, so the ledger leads until the queue drains on
// shutdown. A gap that survives into the next run is what an interrupted one leaves behind: recovery
// cuts state and receipts back to a common height but never rolls the ledger back, and this benchmark
// generates blocks rather than replaying them, so it has no way to close the gap. Such a directory has
// to be cleaned rather than resumed.
func agreedNextBlockNumber(state *executionState, blocks *blockStoreWriter) (int64, error) {
	next := state.height() + 1
	if blockStoreNext, ok := blocks.nextBlockNumber(); ok && blockStoreNext != next {
		return 0, fmt.Errorf(
			"the block store resumes at block %d but the state DB resumes at block %d; "+
				"this data directory was left by an interrupted run and has to be cleaned to be reused",
			blockStoreNext, next)
	}
	return next, nil
}

// setup brings the account population up to the configured size before measurement begins, so that the
// benchmark's reads land in a state DB of a realistic resident size rather than an empty one.
//
// Setup blocks go through the same stores as measured ones, which is what keeps the block store and the
// state DB on a single height.
func (g *GigaSim) setup() error {
	if err := g.setupErc20Contracts(); err != nil {
		return err
	}
	return g.setupAccounts()
}

func (g *GigaSim) setupErc20Contracts() error {
	target := int64(g.config.MinimumNumberOfErc20Contracts)
	if g.accounts.NextErc20ContractID() >= target {
		return nil
	}
	fmt.Printf("Creating ERC20 contracts up to %s.\n", utils.Int64Commas(target))

	staged := 0
	for g.accounts.NextErc20ContractID() < target {
		if err := g.ctx.Err(); err != nil {
			return fmt.Errorf("interrupted while creating ERC20 contracts: %w", err)
		}
		g.accounts.CreateErc20Contract()
		staged++
		if staged >= g.config.TransactionsPerBlock {
			if err := g.finalizeSetupBlock(); err != nil {
				return err
			}
			staged = 0
		}
	}
	if staged > 0 {
		return g.finalizeSetupBlock()
	}
	return nil
}

// setupAccounts creates the configured account population, assigning each account to the cold or the
// dormant set by its identifier rather than by a draw, so that a run holds the counts its config asked
// for.
func (g *GigaSim) setupAccounts() error {
	population := plannedAccountPopulation(g.config)
	if g.accounts.NextAccountID() >= population.total {
		return nil
	}
	fmt.Printf("Creating accounts up to %s.\n", utils.Int64Commas(population.total))

	staged := 0
	for g.accounts.NextAccountID() < population.total {
		if err := g.ctx.Err(); err != nil {
			return fmt.Errorf("interrupted while creating accounts: %w", err)
		}
		g.accounts.CreateAccount(g.accounts.NextAccountID() >= population.firstCold)
		staged++
		if staged >= g.config.TransactionsPerBlock {
			if err := g.finalizeSetupBlock(); err != nil {
				return err
			}
			staged = 0
			fmt.Printf("Created %s of %s accounts.      \r",
				utils.Int64Commas(g.accounts.NextAccountID()), utils.Int64Commas(population.total))
		}
	}
	if staged > 0 {
		return g.finalizeSetupBlock()
	}
	fmt.Printf("\n")
	return nil
}

// finalizeSetupBlock commits the accounts and contracts staged so far as one block, carrying a payload
// of the configured shape so the block store sees the same write volume it will during measurement.
func (g *GigaSim) finalizeSetupBlock() error {
	number := g.highestBlock.Load() + 1
	payload := make([][]byte, 0, g.config.TransactionsPerBlock)
	for range g.config.TransactionsPerBlock {
		payload = append(payload, g.accounts.Rand().Bytes(g.config.BytesPerTransaction))
	}

	if err := g.blocks.writeBlock(number, payload); err != nil {
		return err
	}
	if err := g.persistExecutionResults(number, nil, g.accounts.Counters()); err != nil {
		return err
	}
	g.accounts.ReportEndOfBlock()
	g.highestBlock.Store(number)
	return nil
}

// The main loop of the benchmark: it takes each generated block through execution and into the stores
// that record its results. The block ledger already holds the block; the generator wrote it there
// before handing it over.
//
// The loop ends when the generator closes the channel, and not on cancellation, so that a run being
// shut down first drains the blocks already in the ledger. Draining is what leaves the ledger and the
// state DB on the same height, which is the condition for resuming the data directory later.
func (g *GigaSim) run() {
	defer g.teardown()

	var timeoutChan <-chan time.Time
	if g.config.MaxRuntimeSeconds > 0 {
		timeoutChan = time.After(time.Duration(g.config.MaxRuntimeSeconds) * time.Second)
	}

	for {
		g.metrics.SetMainThreadPhase("get_block")
		g.metrics.RecordStagedBlockQueueDepth(int64(len(g.generator.blocksChan)))

		select {
		case isSuspended := <-g.suspendChan:
			if isSuspended {
				g.suspend()
			}
		case <-timeoutChan:
			fmt.Printf("\nBenchmark timed out after %s.\n",
				utils.FormatDuration(time.Since(g.startTimestamp), 1))
			g.cancel()
			// Stop rearming the timer, but keep consuming: the queued blocks still have to be drained.
			timeoutChan = nil
		case block, ok := <-g.generator.blocksChan:
			if !ok {
				g.halt()
				return
			}
			if err := g.executeAndRecord(block); err != nil {
				// A torn height is not recoverable in place, so the remaining blocks are abandoned rather
				// than executed on top of state that no longer matches the ledger.
				g.fail(err)
				g.halt()
				return
			}
			g.generateConsoleReport(false)
		}
	}
}

// halt prints the closing report for a run that has stopped.
func (g *GigaSim) halt() {
	g.generateConsoleReport(true)
	fmt.Printf("\nBenchmark halted.\n")
}

// executeAndRecord runs one block's transactions and writes what they produced to the receipt store and
// the state DB.
//
// It is deliberately atomic with respect to shutdown — it never returns early on cancellation, and the
// loop above observes shutdown between blocks. A height half-written across the stores is a height
// recovery has to reconcile on the next open, so do not add a mid-block abort.
func (g *GigaSim) executeAndRecord(block *simulatedBlock) error {
	g.executeBlock(block)

	if err := g.persistExecutionResults(block.number, block.receipts, block.counters); err != nil {
		return err
	}

	g.totalBlocks.Add(1)
	g.totalTransactions += int64(len(block.transactions))
	g.totalPayloadBytes += block.payloadBytes()
	g.highestBlock.Store(block.number)
	g.metrics.ReportBlockProcessed(block.number, int64(len(block.transactions)), block.payloadBytes())
	return nil
}

// executeBlock spreads a block's transactions across the executor pool and waits for all of them. The
// wait is what makes the writes a complete block before any of them is committed.
func (g *GigaSim) executeBlock(block *simulatedBlock) {
	g.metrics.SetMainThreadPhase("execute_block")

	transactions := block.transactions
	share := len(transactions) / len(g.executors)
	remainder := len(transactions) % len(g.executors)

	start := 0
	for i, executor := range g.executors {
		size := share
		if i < remainder {
			size++
		}
		if size == 0 {
			break
		}
		g.executing.Add(1)
		executor.submit(executorBatch{transactions: transactions[start : start+size], done: &g.executing})
		start += size
	}
	g.executing.Wait()
}

// persistExecutionResults writes what executing a block produced: its receipts, then its state changes.
// Receipts go first because a receipt for a block the state DB never committed is reconcilable, while
// the reverse leaves committed state whose receipts were dropped.
func (g *GigaSim) persistExecutionResults(
	number int64,
	receipts []*evmtypes.Receipt,
	counters identifierCounters,
) error {
	if g.receipts != nil {
		g.metrics.SetMainThreadPhase("write_receipts")
		if err := g.receipts.writeBlock(number, receipts); err != nil {
			return err
		}
	}
	return g.state.commitBlock(number, counters)
}

// awaitGenerator waits for block production to stop and the staging queue to empty, which it does by
// consuming whatever is left in it, and reports the error that ended generation if one did.
//
// The stores cannot close while the generator still holds the block ledger, and a generator part-way
// through handing over a block never gets to release it. After a clean run there is nothing left to
// take; after a failure the blocks discarded here are ones the failure already orphaned.
func (g *GigaSim) awaitGenerator() {
	g.cancel()
	for range g.generator.blocksChan {
	}
	// The generator sets this before closing the channel, so draining it above orders the read.
	if err := g.generator.failure; err != nil {
		g.recordFailure(err)
	}
}

// fail reports an error that stops the benchmark. Errors on the write path are not recoverable: the
// stores are left mid-block, and continuing would measure a stack that is no longer consistent.
func (g *GigaSim) fail(err error) {
	g.recordFailure(err)
	g.cancel()
}

// recordFailure prints an error and keeps the first one, which Close returns so that a harness reading
// the exit code can tell a completed run from one that died part-way through.
func (g *GigaSim) recordFailure(err error) {
	fmt.Printf("\n%v\n", err)
	if g.runErr == nil {
		g.runErr = err
	}
}

// suspend stops consuming blocks until the benchmark is resumed. Generation stops with it: the
// generator fills the staging queue and then waits to hand over the block it has ready.
func (g *GigaSim) suspend() {
	fmt.Printf("Benchmark suspended.\n")
	g.metrics.SetMainThreadPhase("suspended")

	for {
		select {
		case <-g.ctx.Done():
			return
		case suspended := <-g.suspendChan:
			if suspended {
				break
			}
			// Console rates measure a run, and a suspension is not part of one.
			g.totalBlocks.Store(0)
			g.totalTransactions = 0
			g.totalPayloadBytes = 0
			g.startTimestamp = time.Now()
			fmt.Printf("Benchmark resumed.\n")
			return
		}
	}
}

func (g *GigaSim) teardown() {
	g.awaitGenerator()

	fmt.Printf("Flushing and closing storage.\n")
	if err := g.closePipeline(); err != nil {
		g.recordFailure(fmt.Errorf("failed to close storage: %w", err))
	}

	if g.config.CleanDataOnExit {
		fmt.Printf("CleanDataOnExit is enabled, removing contents of: %s\n", g.config.DataDir)
		if err := removeContents(g.config.DataDir); err != nil {
			g.recordFailure(fmt.Errorf("failed to clean the data directory on exit: %w", err))
		}
	}
	if g.config.CleanLogsOnExit {
		fmt.Printf("CleanLogsOnExit is enabled, removing contents of: %s\n", g.config.LogDir)
		if err := removeContents(g.config.LogDir); err != nil {
			g.recordFailure(fmt.Errorf("failed to clean the log directory on exit: %w", err))
		}
	}

	g.closeChan <- struct{}{}
}

// closePipeline stops the executor pool and releases every database, in the order the stores require:
// the executors stop reading first, then the state read view closes, because the commit store cannot
// release the reference it holds while a view is open, and the storage manager closes last.
func (g *GigaSim) closePipeline() error {
	g.cancel()
	g.stopExecutors()
	g.state.Close()
	err := g.storage.Close()
	g.stopStorage()
	return err
}

func (g *GigaSim) generateConsoleReport(force bool) {
	now := time.Now()
	totalBlocks := g.totalBlocks.Load()
	if !force &&
		now.Sub(g.lastConsoleUpdateTime) < g.consoleUpdatePeriod &&
		totalBlocks-g.lastConsoleUpdateBlockCount < int64(g.config.ConsoleUpdateIntervalBlocks) {
		return
	}
	g.lastConsoleUpdateTime = now
	g.lastConsoleUpdateBlockCount = totalBlocks

	elapsed := now.Sub(g.startTimestamp)
	seconds := elapsed.Seconds()

	fmt.Printf("block %s | %s blocks in %s | %s blocks/sec | %s txns/sec | %s written      \r",
		utils.Int64Commas(g.highestBlock.Load()),
		utils.Int64Commas(totalBlocks),
		utils.FormatDuration(elapsed, 1),
		utils.FormatNumberFloat64(float64(totalBlocks)/seconds, 2),
		utils.FormatNumberFloat64(float64(g.totalTransactions)/seconds, 1),
		utils.FormatBytes(g.totalPayloadBytes))
}

// BlocksProcessed returns how many blocks have been taken through the whole stack since measurement
// began, which excludes the blocks setup wrote and resets on resume.
func (g *GigaSim) BlocksProcessed() int64 {
	return g.totalBlocks.Load()
}

// HighestBlock returns the newest height the benchmark has committed.
func (g *GigaSim) HighestBlock() int64 {
	return g.highestBlock.Load()
}

// BlockUntilHalted blocks until the benchmark has halted.
func (g *GigaSim) BlockUntilHalted() {
	<-g.closeChan
	g.closeChan <- struct{}{}
}

// Close shuts down the benchmark and releases every database it opened. It returns the first error the
// run hit, so that a run which died part-way through is distinguishable from one that completed.
func (g *GigaSim) Close() error {
	g.cancel()
	<-g.closeChan
	g.closeChan <- struct{}{}
	if g.runErr != nil {
		fmt.Printf("Benchmark terminated with an error.\n")
		return g.runErr
	}
	fmt.Printf("Benchmark terminated successfully.\n")
	return nil
}

// Suspend pauses the benchmark. Call Resume to continue.
func (g *GigaSim) Suspend() {
	select {
	case <-g.ctx.Done():
	case g.suspendChan <- true:
	}
}

// Resume continues the benchmark after a Suspend.
func (g *GigaSim) Resume() {
	select {
	case <-g.ctx.Done():
	case g.suspendChan <- false:
	}
}

// resolveDirectories expands the configured paths and applies the clean-on-start options, leaving the
// config holding absolute paths that every store's location is derived from.
func resolveDirectories(config *GigasimConfig) error {
	var err error
	if config.DataDir, err = utils.ResolveAndCreateDir(config.DataDir); err != nil {
		return fmt.Errorf("failed to resolve the data directory: %w", err)
	}
	if config.LogDir, err = utils.ResolveAndCreateDir(config.LogDir); err != nil {
		return fmt.Errorf("failed to resolve the log directory: %w", err)
	}

	if config.CleanDataOnStart {
		fmt.Printf("CleanDataOnStart is enabled, removing contents of: %s\n", config.DataDir)
		if err := removeContents(config.DataDir); err != nil {
			return fmt.Errorf("failed to clean the data directory: %w", err)
		}
	}
	if config.CleanLogsOnStart {
		fmt.Printf("CleanLogsOnStart is enabled, removing contents of: %s\n", config.LogDir)
		if err := removeContents(config.LogDir); err != nil {
			return fmt.Errorf("failed to clean the log directory: %w", err)
		}
	}
	return nil
}

// removeContents deletes all entries inside dir without removing dir itself.
func removeContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
