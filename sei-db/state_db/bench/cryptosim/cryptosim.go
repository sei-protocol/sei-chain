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
}

// Creates a new cryptosim benchmark runner.
func NewCryptoSim(
	ctx context.Context,
	config *CryptoSimConfig,
) (*CryptoSim, error) {

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	metrics := NewCryptosimMetrics(ctx, config.MetricsAddr)
	// Server start deferred until after DataGenerator loads DB state and sets gauges,
	// avoiding rate() spikes when restarting with a preserved DB.

	dataDir, err := resolveAndCreateDataDir(config.DataDir)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Running cryptosim benchmark from data directory: %s\n", dataDir)

	db, err := wrappers.NewDBImpl(config.Backend, dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	fmt.Printf("Initializing random number generator.\n")
	rand := NewCannedRandom(config.CannedRandomSize, config.Seed)

	consoleUpdatePeriod := time.Duration(config.ConsoleUpdateIntervalSeconds * float64(time.Second))

	start := time.Now()

	database := NewDatabase(config, db, metrics)

	dataGenerator, err := NewDataGenerator(config, database, rand, metrics)
	if err != nil {
		cancel()
		db.Close()
		return nil, fmt.Errorf("failed to create data generator: %w", err)
	}
	metrics.StartServer(config.MetricsAddr)

	threadCount := int(config.ThreadsPerCore)*runtime.NumCPU() + config.ConstantThreadCount
	if threadCount < 1 {
		threadCount = 1
	}
	fmt.Printf("Running benchmark with %d threads.\n", threadCount)

	executors := make([]*TransactionExecutor, threadCount)
	for i := 0; i < threadCount; i++ {
		executors[i] = NewTransactionExecutor(
			ctx, database, dataGenerator.FeeCollectionAddress(), config.ExecutorQueueSize, metrics)
	}

	c := &CryptoSim{
		ctx:                               ctx,
		cancel:                            cancel,
		config:                            config,
		consoleUpdatePeriod:               consoleUpdatePeriod,
		lastConsoleUpdateTime:             start,
		lastConsoleUpdateTransactionCount: 0,
		closeChan:                         make(chan struct{}, 1),
		dataGenerator:                     dataGenerator,
		database:                          database,
		executors:                         executors,
		metrics:                           metrics,
	}

	database.SetFlushFunc(c.flushExecutors)

	err = c.setup()
	if err != nil {
		return nil, fmt.Errorf("failed to setup benchmark: %w", err)
	}

	c.database.ResetTransactionCount()
	c.startTimestamp = time.Now()

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
	// Ensure that we at least have as many accounts as the hot set + 2. This simplifies logic elsewhere.
	if c.config.MinimumNumberOfAccounts < c.config.HotAccountSetSize+2 {
		c.config.MinimumNumberOfAccounts = c.config.HotAccountSetSize + 2
	}

	if c.dataGenerator.NextAccountID() >= int64(c.config.MinimumNumberOfAccounts) {
		return nil
	}

	fmt.Printf("Benchmark is configured to run with a minimum of %s accounts. Creating %s new accounts.\n",
		int64Commas(int64(c.config.MinimumNumberOfAccounts)),
		int64Commas(int64(c.config.MinimumNumberOfAccounts)-c.dataGenerator.NextAccountID()))

	for c.dataGenerator.NextAccountID() < int64(c.config.MinimumNumberOfAccounts) {
		if c.ctx.Err() != nil {
			fmt.Printf("benchmark aborted during account creation\n")
			break
		}

		_, _, err := c.dataGenerator.CreateNewAccount(c.config.PaddedAccountSize, true)
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
			c.metrics.SetTotalNumberOfAccounts(c.dataGenerator.NextAccountID())
			c.dataGenerator.ReportFinalizeBlock()
		}

		if c.dataGenerator.NextAccountID()%c.config.SetupUpdateIntervalCount == 0 {
			fmt.Printf("Created %s of %s accounts.      \r",
				int64Commas(c.dataGenerator.NextAccountID()), int64Commas(int64(c.config.MinimumNumberOfAccounts)))
		}
	}
	if c.dataGenerator.NextAccountID() >= c.config.SetupUpdateIntervalCount {
		fmt.Printf("\n")
	}
	fmt.Printf("Created %s of %s accounts.      \n",
		int64Commas(c.dataGenerator.NextAccountID()), int64Commas(int64(c.config.MinimumNumberOfAccounts)))

	err := c.database.FinalizeBlock(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID(), true)
	if err != nil {
		return fmt.Errorf("failed to finalize block: %w", err)
	}
	c.metrics.SetTotalNumberOfAccounts(c.dataGenerator.NextAccountID())
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

	haltTime := time.Now().Add(time.Duration(c.config.MaxRuntimeSeconds * time.Second))

	c.metrics.SetMainThreadPhase("executing")

	for {
		select {
		case <-c.ctx.Done():
			if c.database.TransactionCount() > 0 {
				c.generateConsoleReport(true)
				fmt.Printf("\nTransaction workload halted.\n")
			}
			return
		default:

			txn, err := BuildTransaction(c.dataGenerator)
			if err != nil {
				fmt.Printf("\nfailed to build transaction: %v\n", err)
				continue
			}

			c.executors[c.nextExecutorIndex].ScheduleForExecution(txn)
			c.nextExecutorIndex = (c.nextExecutorIndex + 1) % len(c.executors)

			finalized, err := c.database.MaybeFinalizeBlock(
				c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
			if err != nil {
				fmt.Printf("error finalizing block: %v\n", err)
			}
			if finalized {
				c.dataGenerator.ReportFinalizeBlock()

				if c.config.MaxRuntimeSeconds > 0 && time.Now().After(haltTime) {
					c.cancel()
				}
			}

			c.database.IncrementTransactionCount()
			c.generateConsoleReport(false)
		}
	}
}

// Clean up the benchmark and release any resources.
func (c *CryptoSim) teardown() {
	err := c.database.Close(c.dataGenerator.NextAccountID(), c.dataGenerator.NextErc20ContractID())
	if err != nil {
		fmt.Printf("failed to close database: %v\n", err)
	}

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
