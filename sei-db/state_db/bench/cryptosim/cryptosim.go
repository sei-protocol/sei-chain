package cryptosim

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

const (
	accountPrefix    = 'a'
	contractPrefix   = 'c'
	ethStoragePrefix = 's'
)

// EVM key sizes (matches sei-db/common/evm).
const (
	AddressLen    = 20 // EVM address length
	SlotLen       = 32 // EVM storage slot length
	StorageKeyLen = AddressLen + SlotLen
)

// The test runner for the cryptosim benchmark.
type CryptoSim struct {
	ctx    context.Context
	cancel context.CancelFunc

	// Cancels the DB infrastructure context. Only called after the database
	// has been fully closed during teardown, so that pools, caches, and
	// background goroutines remain functional throughout graceful shutdown.
	dbCancel context.CancelFunc

	// The configuration for the benchmark.
	config *CryptoSimConfig

	// If this much time has passed since the last console update, the benchmark will print a report to the console.
	consoleUpdatePeriod time.Duration

	// The time of the last console update.
	lastConsoleUpdateTime time.Time

	// The number of transactions executed by the benchmark the last time the console was updated.
	lastConsoleUpdateTransactionCount int64

	// The time the benchmark started.
	startTimestamp time.Time

	// A message is sent on this channel when the benchmark is fully stopped and all resources have been released.
	closeChan chan struct{}

	// The data generator for the benchmark.
	dataGenerator *DataGenerator

	// The database for the benchmark.
	database *Database

	// The transaction executors for the benchmark. Transactions are distributed round-robin to the executors.
	executors []*TransactionExecutor

	// The index of the next executor to receive a transaction.
	nextExecutorIndex int

	// The metrics for the benchmark.
	metrics *CryptosimMetrics

	// Send a boolean value to this channel to suspend/resume the benchmark. Sending "true" will suspend the
	// benchmark, sending "false" will resume it. Suspending an already suspended benchmark will have no effect,
	// and resuming an already resumed benchmark will likewise have no effect.
	suspendChan chan bool
}

// Creates a new cryptosim benchmark runner.
func NewCryptoSim(
	ctx context.Context,
	config *CryptoSimConfig,
) (*CryptoSim, error) {

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Ensure that we at least 1 cold account and at least 1 hot account. Additionally, make sure
	// that the number of dormant accounts is at least as large as 2x the number of transactions per block.
	// This simplifies boundary condition checking when selecting random account IDs.
	if config.MinimumNumberOfColdAccounts < 1 {
		// Eliminates edge case where we want a random cold account, but there are no cold accounts.
		config.MinimumNumberOfColdAccounts = 1
	}
	if config.NumberOfHotAccounts < 1 {
		// Eliminates edge case where we want a random hot account, but there are no hot accounts.
		config.NumberOfHotAccounts = 1
	}
	if config.MinimumNumberOfDormantAccounts < 2*config.TransactionsPerBlock {
		// Simplifies cold account selection before a block is committed if we have a very
		// small number of total accounts.
		config.MinimumNumberOfDormantAccounts = 2 * config.TransactionsPerBlock
	}

	// The workload context is cancelled on Ctrl-C (or programmatically) to
	// stop the benchmark loop and executors.
	ctx, cancel := context.WithCancel(ctx)

	// The DB context keeps pools, caches, and background goroutines alive
	// until teardown has finished closing the database.
	dbCtx, dbCancel := context.WithCancel(context.Background())

	dataDir, err := resolveAndCreateDataDir(config.DataDir)
	if err != nil {
		cancel()
		dbCancel()
		return nil, fmt.Errorf("failed to resolve and create data directory: %w", err)
	}

	fmt.Printf("Running cryptosim benchmark from data directory: %s\n", dataDir)

	var dbConfig any
	if config.Backend == wrappers.FlatKV {
		dbConfig = config.FlatKVConfig
	}

	db, err := wrappers.NewDBImpl(dbCtx, config.Backend, dataDir, dbConfig)
	if err != nil {
		cancel()
		dbCancel()
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	metrics := NewCryptosimMetrics(dbCtx, db.GetPhaseTimer(), config)
	// Server start deferred until after DataGenerator loads DB state and sets gauges,
	// avoiding rate() spikes when restarting with a preserved DB.

	fmt.Printf("Initializing random number generator.\n")
	rand := NewCannedRandom(config.CannedRandomSize, config.Seed)

	consoleUpdatePeriod := time.Duration(config.ConsoleUpdateIntervalSeconds * float64(time.Second))

	start := time.Now()

	database := NewDatabase(config, db, metrics)

	dataGenerator, err := NewDataGenerator(config, database, rand, metrics)
	if err != nil {
		cancel()
		if closeErr := db.Close(); closeErr != nil {
			fmt.Printf("failed to close database during error recovery: %v\n", closeErr)
		}
		dbCancel()
		return nil, fmt.Errorf("failed to create data generator: %w", err)
	}
	threadCount := int(config.ThreadsPerCore)*runtime.NumCPU() + config.ConstantThreadCount
	if threadCount < 1 {
		threadCount = 1
	}
	fmt.Printf("Running benchmark with %d threads.\n", threadCount)

	executors := make([]*TransactionExecutor, threadCount)
	for i := 0; i < threadCount; i++ {
		executors[i] = NewTransactionExecutor(
			ctx, cancel, database, dataGenerator.FeeCollectionAddress(), config.ExecutorQueueSize, metrics)
	}

	c := &CryptoSim{
		ctx:                               ctx,
		cancel:                            cancel,
		dbCancel:                          dbCancel,
		config:                            config,
		consoleUpdatePeriod:               consoleUpdatePeriod,
		lastConsoleUpdateTime:             start,
		lastConsoleUpdateTransactionCount: 0,
		closeChan:                         make(chan struct{}, 1),
		dataGenerator:                     dataGenerator,
		database:                          database,
		executors:                         executors,
		metrics:                           metrics,
		suspendChan:                       make(chan bool, 1),
	}

	database.SetFlushFunc(c.flushExecutors)

	err = c.setup()
	if err != nil {
		return nil, fmt.Errorf("failed to setup benchmark: %w", err)
	}

	c.database.ResetTransactionCount()
	c.startTimestamp = time.Now()
	c.metrics.StartBackgroundSampling(c.startTimestamp)

	go c.run()
	return c, nil
}

// Prepare the benchmark by pre-populating the database with the minimum number of accounts.
func (c *CryptoSim) setup() error {
	err := c.setupAccounts()
	if err != nil {
		return fmt.Errorf("failed to setup accounts: %w", err)
	}
	err = c.setupErc20Contracts()
	if err != nil {
		return fmt.Errorf("failed to setup ERC20 contracts: %w", err)
	}
	return nil
}

// Prepopulate the database with the minimum number of accounts.
func (c *CryptoSim) setupAccounts() error {

	requiredNumberOfAccounts := c.config.NumberOfHotAccounts +
		c.config.MinimumNumberOfColdAccounts +
		c.config.MinimumNumberOfDormantAccounts

	if c.dataGenerator.NextAccountID() >= int64(requiredNumberOfAccounts) {
		return nil
	}

	fmt.Printf("Benchmark is configured to run with a minimum of %s accounts. Creating %s new accounts.\n",
		int64Commas(int64(requiredNumberOfAccounts)),
		int64Commas(int64(requiredNumberOfAccounts)-c.dataGenerator.NextAccountID()))

	for c.dataGenerator.NextAccountID() < int64(requiredNumberOfAccounts) {
		if c.ctx.Err() != nil {
			fmt.Printf("benchmark aborted during account creation\n")
			break
		}

		_, _, _, err := c.dataGenerator.CreateNewAccount(c.config.PaddedAccountSize, true)
		if err != nil {
			return fmt.Errorf("failed to create new account: %w", err)
		}
		c.database.IncrementTransactionCount()
		finalized, err := c.database.MaybeFinalizeBlock(
			c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
		if err != nil {
			return fmt.Errorf("failed to maybe commit batch: %w", err)
		}
		if finalized {
			c.dataGenerator.ReportAccountCounts()
			c.dataGenerator.ReportFinalizeBlock()
		}

		if c.dataGenerator.NextAccountID()%c.config.SetupUpdateIntervalCount == 0 {
			fmt.Printf("Created %s of %s accounts.      \r",
				int64Commas(c.dataGenerator.NextAccountID()), int64Commas(int64(requiredNumberOfAccounts)))
		}
	}
	if c.dataGenerator.NextAccountID() >= c.config.SetupUpdateIntervalCount {
		fmt.Printf("\n")
	}
	fmt.Printf("Created %s of %s accounts.      \n",
		int64Commas(c.dataGenerator.NextAccountID()), int64Commas(int64(requiredNumberOfAccounts)))

	err := c.database.FinalizeBlock(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID(), true)
	if err != nil {
		return fmt.Errorf("failed to finalize block: %w", err)
	}
	c.dataGenerator.ReportAccountCounts()
	c.dataGenerator.ReportFinalizeBlock()

	fmt.Printf("There are now %s accounts in the database.\n", int64Commas(c.dataGenerator.NextAccountID()))

	return nil
}

// Prepopulate the database with the minimum number of ERC20 contracts.
func (c *CryptoSim) setupErc20Contracts() error {

	// Ensure that we at least have as many ERC20 contracts as the hot set + 1. This simplifies logic elsewhere.
	if c.config.MinimumNumberOfErc20Contracts < c.config.HotErc20ContractSetSize+1 {
		c.config.MinimumNumberOfErc20Contracts = c.config.HotErc20ContractSetSize + 1
	}

	if c.dataGenerator.NextErc20ContractID() >= int64(c.config.MinimumNumberOfErc20Contracts) {
		return nil
	}

	fmt.Printf("Benchmark is configured to run with a minimum of %s simulated ERC20 contracts. "+
		"Creating %s new ERC20 contracts.\n",
		int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)),
		int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)-c.dataGenerator.NextErc20ContractID()))

	for c.dataGenerator.NextErc20ContractID() < int64(c.config.MinimumNumberOfErc20Contracts) {
		if c.ctx.Err() != nil {
			fmt.Printf("benchmark aborted during ERC20 contract creation\n")
			break
		}

		c.database.IncrementTransactionCount()

		_, _, err := c.dataGenerator.CreateNewErc20Contract(c.config.Erc20ContractSize, true)
		if err != nil {
			return fmt.Errorf("failed to create new ERC20 contract: %w", err)
		}
		finalized, err := c.database.MaybeFinalizeBlock(
			c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
		if err != nil {
			return fmt.Errorf("failed to maybe commit batch: %w", err)
		}
		if finalized {
			c.dataGenerator.ReportFinalizeBlock()
			c.metrics.SetTotalNumberOfERC20Contracts(c.dataGenerator.NextErc20ContractID())
		}

		if c.dataGenerator.NextErc20ContractID()%c.config.SetupUpdateIntervalCount == 0 {
			fmt.Printf("Created %s of %s simulated ERC20 contracts.      \r",
				int64Commas(c.dataGenerator.NextErc20ContractID()),
				int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)))
		}
	}

	if c.dataGenerator.NextErc20ContractID() >= c.config.SetupUpdateIntervalCount {
		fmt.Printf("\n")
	}

	fmt.Printf("Created %s of %s simulated ERC20 contracts.      \n",
		int64Commas(c.dataGenerator.NextErc20ContractID()), int64Commas(int64(c.config.MinimumNumberOfErc20Contracts)))

	err := c.database.FinalizeBlock(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID(), true)
	if err != nil {
		return fmt.Errorf("failed to finalize block: %w", err)
	}
	c.dataGenerator.ReportFinalizeBlock()
	c.metrics.SetTotalNumberOfERC20Contracts(c.dataGenerator.NextErc20ContractID())

	fmt.Printf("There are now %s simulated ERC20 contracts in the database.\n",
		int64Commas(c.dataGenerator.NextErc20ContractID()))

	return nil
}

// The main loop of the benchmark.
func (c *CryptoSim) run() {

	defer c.teardown()

	haltTime := time.Now().Add(time.Duration(c.config.MaxRuntimeSeconds) * time.Second)

	c.metrics.SetMainThreadPhase("executing")

	for {
		select {
		case <-c.ctx.Done():
			if c.database.TransactionCount() > 0 {
				c.generateConsoleReport(true)
				fmt.Printf("\nTransaction workload halted.\n")
			}
			return
		case isSuspended := <-c.suspendChan:
			if isSuspended {
				c.suspend()
			}
		default:
			c.handleNextCycle(haltTime)
		}
	}
}

// Process the next benchmark cycle, creating a new transaction and executing it.
func (c *CryptoSim) handleNextCycle(haltTime time.Time) {
	txn, err := BuildTransaction(c.dataGenerator)
	if err != nil {
		fmt.Printf("\nfailed to build transaction: %v\n", err)
		c.cancel()
		return
	}

	c.executors[c.nextExecutorIndex].ScheduleForExecution(txn)
	c.nextExecutorIndex = (c.nextExecutorIndex + 1) % len(c.executors)

	finalized, err := c.database.MaybeFinalizeBlock(
		c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
	if err != nil {
		fmt.Printf("error finalizing block: %v\n", err)
		c.cancel()
		return
	}
	if finalized {
		c.dataGenerator.ReportAccountCounts()
		c.dataGenerator.ReportFinalizeBlock()

		if c.config.MaxRuntimeSeconds > 0 && time.Now().After(haltTime) {
			c.cancel()
		}
	}

	c.database.IncrementTransactionCount()
	c.generateConsoleReport(false)
}

// Suspends the benchmark. This method blocks until the benchmark is resumed or shut down.
func (c *CryptoSim) suspend() {

	err := c.database.FinalizeBlock(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID(), true)
	if err != nil {
		fmt.Printf("failed to finalize block: %v\n", err)
		c.cancel()
		return
	}

	fmt.Printf("Benchmark suspended.\n")

	for {
		select {
		case <-c.ctx.Done():
			return
		case suspended := <-c.suspendChan:

			if suspended {
				break
			}

			// Reset console metrics
			c.database.ResetTransactionCount()
			c.startTimestamp = time.Now()

			fmt.Printf("Benchmark resumed.\n")
			return
		}
	}
}

// Clean up the benchmark and release any resources.
func (c *CryptoSim) teardown() {
	err := c.database.Close(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
	if err != nil {
		fmt.Printf("failed to close database: %v\n", err)
	}

	c.dbCancel()
	c.dataGenerator.Close()

	c.closeChan <- struct{}{}
}

// Generates a human readable report of the benchmark's progress.
func (c *CryptoSim) generateConsoleReport(force bool) {

	// Future work: determine overhead of measuring time each cycle and change accordingly.

	now := time.Now()
	timeSinceLastUpdate := now.Sub(c.lastConsoleUpdateTime)
	transactionsSinceLastUpdate := c.database.TransactionCount() - c.lastConsoleUpdateTransactionCount

	if !force &&
		timeSinceLastUpdate < c.consoleUpdatePeriod &&
		transactionsSinceLastUpdate < int64(c.config.ConsoleUpdateIntervalTransactions) {

		// Not yet time to update the console.
		return
	}

	c.lastConsoleUpdateTime = now
	c.lastConsoleUpdateTransactionCount = c.database.TransactionCount()

	totalElapsedTime := now.Sub(c.startTimestamp)
	transactionsPerSecond := float64(c.database.TransactionCount()) / totalElapsedTime.Seconds()

	// Generate the report.
	fmt.Printf("%s txns executed in %s (%s txns/sec), total number of accounts: %s      \r",
		int64Commas(c.database.TransactionCount()),
		formatDuration(totalElapsedTime, 1),
		formatNumberFloat64(transactionsPerSecond, 2),
		int64Commas(c.dataGenerator.NextAccountID()))
}

// Shut down the benchmark and release any resources.
func (c *CryptoSim) Close() error {
	c.cancel()
	<-c.closeChan

	// "reload" closeChan in case other goroutines are waiting on it.
	c.closeChan <- struct{}{}

	fmt.Printf("Benchmark terminated successfully.\n")

	return nil
}

// Blocks until all pending transactions sent to the executors have been executed.
func (c *CryptoSim) flushExecutors() {
	for _, executor := range c.executors {
		executor.Flush()
	}
}

// Blocks until the benchmark has halted.
func (c *CryptoSim) BlockUntilHalted() {
	<-c.closeChan

	// "reload" closeChan in case other goroutines are waiting on it.
	c.closeChan <- struct{}{}
}

// Suspend the benchmark. Stops all transactional load. Calling this while the benchmark is
// suspended will have no effect. Call Resume() to resume the benchmark.
func (c *CryptoSim) Suspend() {
	select {
	case <-c.ctx.Done():
		return
	case c.suspendChan <- true:
		return
	}

}

// Resume the benchmark. Calling this while the benchmark is not suspended will have no effect.
func (c *CryptoSim) Resume() {
	select {
	case <-c.ctx.Done():
		return
	case c.suspendChan <- false:
		return
	}
}
