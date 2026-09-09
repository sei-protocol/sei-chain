package blocksim

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/time/rate"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// The benchmark runner for the blocksim benchmark.
type BlockSim struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *BlocksimConfig

	db        blocktypes.BlockDB
	generator *BlockGenerator
	metrics   *BlocksimMetrics

	// Console reporting state.
	consoleUpdatePeriod         time.Duration
	lastConsoleUpdateTime       time.Time
	lastConsoleUpdateBlockCount int64
	startTimestamp              time.Time
	totalBlocksWritten          int64
	totalQCsWritten             int64
	totalBytesWritten           int64
	highestBlockHeight          uint64
	lastPrunedAt                uint64

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

	fmt.Printf("Initializing random number generator.\n")

	// Pre-generate a random buffer once; all block/QC data generation slices into it
	// (zero-copy) so the generator never runs math/rand on the hot path.
	cannedRand := crand.NewCannedRandom(int(config.RandomDataBufferSizeBytes), config.Seed) //nolint:gosec // buffer size is bounded by config

	db, err := openBlockDB(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	// Recover the persisted tail so generation resumes after existing history
	// instead of restarting from global block 0, whose records are already stored.
	resume, found, err := recoverResumeState(db)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to recover resume state: %w", err)
	}
	var highest, resumeAt uint64
	if found {
		// A hard crash can leave a QC ahead of its blocks, since a batch writes
		// its QC first. Backfill the missing tail so the store ends exactly at the
		// newest QC's range and the next batch appends contiguously. The bytes are
		// irrelevant to a DB stress test, so the backfill writes fresh values.
		firstMissing := resume.qcFirst
		if resume.hasBlocks {
			firstMissing = resume.highestBlock + 1
		}
		for n := firstMissing; n < resume.qcNext; n++ {
			value := cannedRand.Bytes(int(config.blockValueBytes())) //nolint:gosec // bounded by config validation
			if err := db.PutBlock(n, blockHash(n), value); err != nil {
				cancel()
				return nil, fmt.Errorf("failed to backfill block %d: %w", n, err)
			}
		}
		highest = resume.qcNext - 1
		resumeAt = resume.qcNext
		fmt.Printf("Resuming from block %d.\n", highest)
	}

	generator := NewBlockGenerator(ctx, config, cannedRand, resumeAt)

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
		highestBlockHeight:    highest,
		lastPrunedAt:          highest,
		closeChan:             make(chan struct{}, 1),
		suspendChan:           make(chan bool, 1),
		rateLimiter:           rateLimiter,
	}

	go b.run()
	return b, nil
}

// resumeState is where a reopened store left off: the range covered by its newest
// QC record, and the newest block record written under that QC.
type resumeState struct {
	qcFirst uint64
	qcNext  uint64

	// highestBlock is the newest block number written under the newest QC, valid
	// only when hasBlocks is set. A crash can leave a QC with none of its blocks.
	highestBlock uint64
	hasBlocks    bool
}

// recoverResumeState finds where generation should resume, reporting false for a
// store holding no QC record, which includes an empty one.
//
// Records are walked newest-first and the walk stops at the first QC, which bounds
// it to a single batch: a batch writes its QC before any of the blocks it covers,
// so the first QC met going backwards is the newest one, and every block seen
// before it belongs to that QC's range.
func recoverResumeState(db blocktypes.BlockDB) (resumeState, bool, error) {
	it, err := db.Scan(true)
	if err != nil {
		return resumeState{}, false, fmt.Errorf("failed to open resume iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	var state resumeState
	for {
		ok, err := it.Next()
		if err != nil {
			return resumeState{}, false, fmt.Errorf("failed to advance resume iterator: %w", err)
		}
		if !ok {
			return resumeState{}, false, nil
		}
		switch it.Kind() {
		case blocktypes.KindBlock:
			if !state.hasBlocks {
				state.highestBlock = it.Number()
				state.hasBlocks = true
			}
		case blocktypes.KindQC:
			value, err := it.Value()
			if err != nil {
				return resumeState{}, false, fmt.Errorf("failed to read newest QC: %w", err)
			}
			next, err := qcRangeEnd(value)
			if err != nil {
				return resumeState{}, false, fmt.Errorf("failed to read newest QC range: %w", err)
			}
			state.qcFirst = it.Number()
			state.qcNext = next
			return state, true, nil
		}
	}
}

// The main loop of the benchmark.
func (b *BlockSim) run() {
	defer b.teardown()

	var timeoutChan <-chan time.Time
	if b.config.MaxRuntimeSeconds > 0 {
		timeoutChan = time.After(time.Duration(b.config.MaxRuntimeSeconds) * time.Second)
	}

	for {
		b.metrics.SetMainThreadPhase("get_block")

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
		case batch := <-b.generator.batchChan:
			b.handleNextBatch(batch)
		}

		b.generateConsoleReport(false)
	}
}

func (b *BlockSim) maybeThrottle() {
	if b.rateLimiter == nil {
		return
	}
	b.metrics.SetMainThreadPhase("throttling")
	if err := b.rateLimiter.Wait(b.ctx); err != nil {
		return
	}
}

// handleNextBatch persists one batch: its QC, then all of its blocks. The QC goes
// first so that a crash truncating the write sequence can only ever leave a QC
// without its blocks, which resume repairs, and never a block whose QC is
// missing. It is deliberately atomic with respect to shutdown — it writes the
// whole batch and only returns early on a write *error*, never on context
// cancellation (run observes shutdown solely between batches, and the only
// mid-batch ctx use, maybeThrottle, merely stops throttling and proceeds with the
// write). Combined with flushes happening only after the full batch (here, in
// suspend, and in teardown), a cleanly shut-down store always ends at a complete
// batch boundary, so it resumes with no gap. Do NOT add a mid-batch ctx abort: it
// would let a clean shutdown leave a partial batch.
func (b *BlockSim) handleNextBatch(batch *generatedBatch) {
	b.metrics.SetMainThreadPhase("write_qc")
	if err := b.db.PutRecord(blocktypes.KindQC, batch.first, batch.next, batch.qc); err != nil {
		fmt.Printf("failed to write QC: %v\n", err)
		b.cancel()
		return
	}
	b.totalQCsWritten++
	b.metrics.ReportQCWritten()

	b.metrics.SetMainThreadPhase("write_block")
	for i, value := range batch.blocks {
		b.maybeThrottle()
		n := batch.first + uint64(i) //nolint:gosec // batch index is small and non-negative
		if err := b.db.PutBlock(n, blockHash(n), value); err != nil {
			fmt.Printf("failed to write block %d: %v\n", n, err)
			b.cancel()
			return
		}
		blockBytes := int64(len(value))
		b.totalBlocksWritten++
		b.totalBytesWritten += blockBytes
		b.highestBlockHeight = n
		b.metrics.ReportBlockWritten(blockBytes, int64(b.config.TransactionsPerBlock)) //nolint:gosec // bounded by config validation
	}
	b.metrics.RecordHighestHeight(b.highestBlockHeight)

	// Periodic flush.
	if b.config.FlushIntervalBlocks > 0 && b.totalBlocksWritten%int64(b.config.FlushIntervalBlocks) == 0 { //nolint:gosec
		b.metrics.SetMainThreadPhase("flush")
		if err := b.db.Flush(); err != nil {
			fmt.Printf("failed to flush: %v\n", err)
			b.cancel()
			return
		}
		b.metrics.ReportFlush()
	}

	// Periodic prune. The watermark is moved verbatim: choosing a boundary a whole
	// range of records can be dropped at is the consensus layer's job, and what is
	// under measurement here is the reclamation the watermark triggers.
	if b.highestBlockHeight > b.config.UnprunedBlocks &&
		b.highestBlockHeight-b.lastPrunedAt >= b.config.PruneIntervalBlocks {
		b.metrics.SetMainThreadPhase("prune")
		lowestToKeep := b.highestBlockHeight - b.config.UnprunedBlocks
		b.db.SetPruneWatermark(lowestToKeep)
		b.lastPrunedAt = b.highestBlockHeight
		b.metrics.ReportPrune()
		b.metrics.RecordLowestHeight(lowestToKeep)
	}
}

func (b *BlockSim) suspend() {
	// Flush before suspending so state is durable.
	if err := b.db.Flush(); err != nil {
		fmt.Printf("failed to flush on suspend: %v\n", err)
	}

	fmt.Printf("Benchmark suspended.\n")
	b.metrics.SetMainThreadPhase("suspended")

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
			b.totalBytesWritten = 0
			b.totalQCsWritten = 0
			b.startTimestamp = time.Now()
			fmt.Printf("Benchmark resumed.\n")
			return
		}
	}
}

func (b *BlockSim) teardown() {
	fmt.Printf("Flushing and closing database.\n")
	if err := b.db.Flush(); err != nil {
		fmt.Printf("failed to flush during teardown: %v\n", err)
	}
	if err := b.db.Close(); err != nil {
		fmt.Printf("failed to close database: %v\n", err)
	}

	if b.config.CleanDataOnExit {
		fmt.Printf("CleanDataOnExit is enabled, removing contents of: %s\n", b.config.DataDir)
		if err := removeContents(b.config.DataDir); err != nil {
			fmt.Printf("failed to clean data directory on exit: %v\n", err)
		}
	}
	if b.config.CleanLogsOnExit {
		fmt.Printf("CleanLogsOnExit is enabled, removing contents of: %s\n", b.config.LogDir)
		if err := removeContents(b.config.LogDir); err != nil {
			fmt.Printf("failed to clean log directory on exit: %v\n", err)
		}
	}

	b.closeChan <- struct{}{}
}

func (b *BlockSim) generateConsoleReport(force bool) {
	now := time.Now()
	timeSinceLastUpdate := now.Sub(b.lastConsoleUpdateTime)
	blocksSinceLastUpdate := b.totalBlocksWritten - b.lastConsoleUpdateBlockCount

	if !force &&
		timeSinceLastUpdate < b.consoleUpdatePeriod &&
		blocksSinceLastUpdate < int64(b.config.ConsoleUpdateIntervalBlocks) { //nolint:gosec
		return
	}

	b.lastConsoleUpdateTime = now
	b.lastConsoleUpdateBlockCount = b.totalBlocksWritten

	elapsed := now.Sub(b.startTimestamp)
	bytesPerSecond := float64(b.totalBytesWritten) / elapsed.Seconds()

	fmt.Printf("%s blocks in %s | %s written | %s/sec      \r",
		utils.Int64Commas(b.totalBlocksWritten),
		utils.FormatDuration(elapsed, 1),
		utils.FormatBytes(b.totalBytesWritten),
		utils.FormatBytes(int64(bytesPerSecond)))
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

// openBlockDB creates a BlockDB for the configured backend.
func openBlockDB(config *BlocksimConfig) (blocktypes.BlockDB, error) {
	switch config.Backend {
	case "mem":
		return memblock.NewBlockDB(), nil
	case "litt":
		littConfig, err := littblock.DefaultConfig(config.DataDir)
		if err != nil {
			return nil, fmt.Errorf("failed to build litt block db config: %w", err)
		}
		littConfig.RetentionTime = time.Duration(config.LittRetentionSeconds) * time.Second
		// Record litt_* metrics into blocksim's already-configured global OTel MeterProvider (set up in
		// main before the DB is opened). MetricsServeEndpoint stays false so LittDB does not stand up its
		// own registry/server; the metrics surface on blocksim's single /metrics endpoint.
		littConfig.Litt.MetricsEnabled = config.LittMetricsEnabled
		return littblock.NewBlockDB(littConfig)
	default:
		return nil, fmt.Errorf("unknown block store backend: %q", config.Backend)
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
