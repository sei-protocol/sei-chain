package blocksim

import (
	"context"
	"fmt"
	"os"
	"time"

	blockdb "github.com/sei-protocol/sei-chain/sei-db/block_db"
	"github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"golang.org/x/time/rate"
)

// The benchmark runner for the blocksim benchmark.
type BlockSim struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *BlocksimConfig

	db        blockdb.BlockDB
	generator *BlockGenerator
	metrics   *BlocksimMetrics

	// Console reporting state.
	consoleUpdatePeriod         time.Duration
	lastConsoleUpdateTime       time.Time
	lastConsoleUpdateBlockCount int64
	startTimestamp              time.Time
	totalBlocksWritten          int64
	totalTransactionsWritten    int64
	highestBlockHeight          uint64

	// A message is sent on this channel when the benchmark is fully stopped.
	closeChan chan struct{}

	// Suspend/resume toggle channel.
	suspendChan chan bool

	// Enforces a maximum block write rate (if enabled).
	rateLimiter *rate.Limiter
}

// Creates a new blocksim benchmark runner.
func NewBlockSim(
	ctx context.Context,
	config *BlocksimConfig,
	metrics *BlocksimMetrics,
) (*BlockSim, error) {

	var err error
	config.DataDir, err = utils.ResolveAndCreateDir(config.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve data directory: %w", err)
	}
	config.LogDir, err = utils.ResolveAndCreateDir(config.LogDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve log directory: %w", err)
	}

	if config.CleanDataOnStart {
		fmt.Printf("CleanDataOnStart is enabled, removing contents of: %s\n", config.DataDir)
		if err := removeContents(config.DataDir); err != nil {
			return nil, fmt.Errorf("failed to clean data directory: %w", err)
		}
	}
	if config.CleanLogsOnStart {
		fmt.Printf("CleanLogsOnStart is enabled, removing contents of: %s\n", config.LogDir)
		if err := removeContents(config.LogDir); err != nil {
			return nil, fmt.Errorf("failed to clean log directory: %w", err)
		}
	}

	fmt.Printf("Running blocksim benchmark from data directory: %s\n", config.DataDir)
	fmt.Printf("Logs are being routed to: %s\n", config.LogDir)

	db, err := openBlockDB(config.Backend, config.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	fmt.Printf("Initializing random number generator.\n")
	rng := rand.NewCannedRandom(int(config.CannedRandomSize), config.Seed) //nolint:gosec

	ctx, cancel := context.WithCancel(ctx)

	generator := NewBlockGenerator(ctx, config, rng, 1)

	consoleUpdatePeriod := time.Duration(config.ConsoleUpdateIntervalSeconds * float64(time.Second))

	var rateLimiter *rate.Limiter
	if config.MaxBlocksPerSecond > 0 {
		rateLimiter = rate.NewLimiter(rate.Limit(config.MaxBlocksPerSecond), 1)
	}

	start := time.Now()

	b := &BlockSim{
		ctx:                   ctx,
		cancel:                cancel,
		config:                config,
		db:                    db,
		generator:             generator,
		metrics:               metrics,
		consoleUpdatePeriod:   consoleUpdatePeriod,
		lastConsoleUpdateTime: start,
		startTimestamp:        start,
		closeChan:             make(chan struct{}, 1),
		suspendChan:           make(chan bool, 1),
		rateLimiter:           rateLimiter,
	}

	go b.run()
	return b, nil
}

// The main loop of the benchmark.
func (b *BlockSim) run() {
	defer b.teardown()

	var timeoutChan <-chan time.Time
	if b.config.MaxRuntimeSeconds > 0 {
		timeoutChan = time.After(time.Duration(b.config.MaxRuntimeSeconds) * time.Second)
	}

	for {
		select {
		case <-b.ctx.Done():
			b.generateConsoleReport(true)
			fmt.Printf("\nBenchmark halted.\n")
			return
		case isSuspended := <-b.suspendChan:
			if isSuspended {
				b.suspend()
			}
		case <-timeoutChan:
			fmt.Printf("\nBenchmark timed out after %s.\n",
				utils.FormatDuration(time.Since(b.startTimestamp), 1))
			b.cancel()
			return
		case blk := <-b.generator.blocksChan:
			b.maybeThrottle()
			b.handleNextBlock(blk)
		}

		b.generateConsoleReport(false)
	}
}

func (b *BlockSim) maybeThrottle() {
	if b.rateLimiter == nil {
		return
	}
	if err := b.rateLimiter.Wait(b.ctx); err != nil {
		return
	}
}

func (b *BlockSim) handleNextBlock(blk *blockdb.BinaryBlock) {
	if err := b.db.WriteBlock(b.ctx, blk); err != nil {
		fmt.Printf("failed to write block %d: %v\n", blk.Height, err)
		b.cancel()
		return
	}

	txCount := int64(len(blk.Transactions))
	b.totalBlocksWritten++
	b.totalTransactionsWritten += txCount
	b.highestBlockHeight = blk.Height
	b.metrics.ReportBlockWritten(txCount)

	// Periodic flush.
	if b.config.FlushIntervalBlocks > 0 && b.totalBlocksWritten%int64(b.config.FlushIntervalBlocks) == 0 {
		if err := b.db.Flush(b.ctx); err != nil {
			fmt.Printf("failed to flush: %v\n", err)
			b.cancel()
			return
		}
		b.metrics.ReportFlush()
	}

	// Periodic prune.
	if blk.Height > b.config.UnprunedBlocks {
		lowestToKeep := blk.Height - b.config.UnprunedBlocks
		if err := b.db.Prune(b.ctx, lowestToKeep); err != nil {
			fmt.Printf("failed to prune: %v\n", err)
			b.cancel()
			return
		}
		b.metrics.ReportPrune()
	}
}

func (b *BlockSim) suspend() {
	// Flush before suspending so state is durable.
	if err := b.db.Flush(b.ctx); err != nil {
		fmt.Printf("failed to flush on suspend: %v\n", err)
	}

	fmt.Printf("Benchmark suspended.\n")

	for {
		select {
		case <-b.ctx.Done():
			return
		case suspended := <-b.suspendChan:
			if suspended {
				break
			}
			// Reset console metrics on resume.
			b.totalBlocksWritten = 0
			b.totalTransactionsWritten = 0
			b.startTimestamp = time.Now()
			fmt.Printf("Benchmark resumed.\n")
			return
		}
	}
}

func (b *BlockSim) teardown() {
	fmt.Printf("Flushing and closing database.\n")
	if err := b.db.Flush(b.ctx); err != nil {
		fmt.Printf("failed to flush during teardown: %v\n", err)
	}
	if err := b.db.Close(b.ctx); err != nil {
		fmt.Printf("failed to close database: %v\n", err)
	}
	b.closeChan <- struct{}{}
}

func (b *BlockSim) generateConsoleReport(force bool) {
	now := time.Now()
	timeSinceLastUpdate := now.Sub(b.lastConsoleUpdateTime)
	blocksSinceLastUpdate := b.totalBlocksWritten - b.lastConsoleUpdateBlockCount

	if !force &&
		timeSinceLastUpdate < b.consoleUpdatePeriod &&
		blocksSinceLastUpdate < int64(b.config.ConsoleUpdateIntervalBlocks) {
		return
	}

	b.lastConsoleUpdateTime = now
	b.lastConsoleUpdateBlockCount = b.totalBlocksWritten

	elapsed := now.Sub(b.startTimestamp)
	blocksPerSecond := float64(b.totalBlocksWritten) / elapsed.Seconds()
	txnsPerSecond := float64(b.totalTransactionsWritten) / elapsed.Seconds()

	fmt.Printf("%s blocks (%s txns) in %s | %s blocks/sec | %s txns/sec      \r",
		utils.Int64Commas(b.totalBlocksWritten),
		utils.Int64Commas(b.totalTransactionsWritten),
		utils.FormatDuration(elapsed, 1),
		utils.FormatNumberFloat64(blocksPerSecond, 1),
		utils.FormatNumberFloat64(txnsPerSecond, 0))
}

// Blocks until the benchmark has halted.
func (b *BlockSim) BlockUntilHalted() {
	<-b.closeChan
	b.closeChan <- struct{}{}
}

// Close shuts down the benchmark and releases resources.
func (b *BlockSim) Close() error {
	b.cancel()
	<-b.closeChan
	b.closeChan <- struct{}{}
	fmt.Printf("Benchmark terminated successfully.\n")
	return nil
}

// Suspend the benchmark. Call Resume() to continue.
func (b *BlockSim) Suspend() {
	select {
	case <-b.ctx.Done():
	case b.suspendChan <- true:
	}
}

// Resume the benchmark after a Suspend().
func (b *BlockSim) Resume() {
	select {
	case <-b.ctx.Done():
	case b.suspendChan <- false:
	}
}

// openBlockDB creates a BlockDB for the given backend name.
func openBlockDB(backend string, _ string) (blockdb.BlockDB, error) {
	switch backend {
	case "mem":
		return blockdb.NewMemBlockDB(), nil
	default:
		return nil, fmt.Errorf("unknown BlockDB backend: %q", backend)
	}
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
		if err := os.RemoveAll(fmt.Sprintf("%s/%s", dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
