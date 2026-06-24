package blocksim

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmutils "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"golang.org/x/time/rate"
)

// Fixed committee genesis time; the simulator does not depend on wall-clock
// genesis, and a constant keeps committee construction deterministic.
var genesisTime = time.Unix(1_700_000_000, 0)

// The benchmark runner for the blocksim benchmark.
type BlockSim struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *BlocksimConfig

	db        types.BlockDB
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
	rng := tmutils.TestRngFromSeed(config.Seed)

	committee, keys, err := buildCommittee(rng, int(config.CommitteeSize)) //nolint:gosec // CommitteeSize is a small config value
	if err != nil {
		return nil, err
	}

	db, err := openBlockDB(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	blockCount, qcCount, err := countExistingState(db)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to read existing state: %w", err)
	}
	fmt.Printf("Loaded %d blocks and %d QCs from existing state.\n", blockCount, qcCount)

	// Recover the persisted tail so generation resumes after existing history
	// instead of restarting from global block 0 (which would collide with the
	// on-disk data).
	prev, highestOpt, err := recoverResumeState(db)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to recover resume state: %w", err)
	}
	var highest uint64
	if prevQC, ok := prev.Get(); ok {
		// A hard crash can leave the QC ahead of its blocks (the BlockDB
		// guarantees a persisted block is always covered by a persisted QC, never
		// the reverse). Backfill the missing tail so the store ends exactly at the
		// last QC's range — the next batch then appends contiguously. Block bytes
		// are irrelevant here (this is a DB stress test), so the backfill writes
		// freshly generated blocks under the already-persisted QC.
		qcRange := prevQC.GlobalRange()
		lastQCNext := uint64(qcRange.Next)
		firstMissing := uint64(qcRange.First)
		if h, ok := highestOpt.Get(); ok {
			firstMissing = h + 1
		}
		for n := firstMissing; n < lastQCNext; n++ {
			blk := types.GenBlock(rng)
			if err := db.WriteBlock(types.GlobalBlockNumber(n), blk); err != nil { //nolint:gosec // n < lastQCNext
				cancel()
				return nil, fmt.Errorf("failed to backfill block %d: %w", n, err)
			}
		}
		highest = lastQCNext - 1
		fmt.Printf("Resuming from block %d.\n", highest)
	}

	generator := NewBlockGenerator(ctx, config, rng, committee, keys, prev)

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

// countExistingState scans the block and QC iterators to count what is already
// persisted, exercising the replay path at startup.
func countExistingState(db types.BlockDB) (blocks int, qcs int, err error) {
	blockIt, err := db.Blocks(false)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open block iterator: %w", err)
	}
	defer func() { _ = blockIt.Close() }()
	for {
		ok, err := blockIt.Next()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to advance block iterator: %w", err)
		}
		if !ok {
			break
		}
		blocks++
	}

	qcIt, err := db.QCs(false)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open QC iterator: %w", err)
	}
	defer func() { _ = qcIt.Close() }()
	for {
		ok, err := qcIt.Next()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to advance QC iterator: %w", err)
		}
		if !ok {
			break
		}
		qcs++
	}
	return blocks, qcs, nil
}

// recoverResumeState reads the persisted tail so the benchmark resumes appending
// after existing history rather than restarting from global block 0. It returns
// the last persisted QC (to seed the generator's chain via BlockGenerator.prev)
// and the highest persisted block number (None if no blocks are persisted, which
// the caller must distinguish from block 0). Blocks and QCs are recovered
// independently with a single reverse-iterator step each, because a hard crash
// can leave the QC ahead of its blocks. An empty store yields (None, None, nil),
// preserving genesis-start behavior.
func recoverResumeState(
	db types.BlockDB,
) (tmutils.Option[*types.CommitQC], tmutils.Option[uint64], error) {
	prev := tmutils.None[*types.CommitQC]()
	highest := tmutils.None[uint64]()

	blockIt, err := db.Blocks(true)
	if err != nil {
		return prev, highest, fmt.Errorf("failed to open reverse block iterator: %w", err)
	}
	defer func() { _ = blockIt.Close() }()
	ok, err := blockIt.Next()
	if err != nil {
		return prev, highest, fmt.Errorf("failed to read newest block: %w", err)
	}
	if ok {
		highest = tmutils.Some(uint64(blockIt.Number()))
	}

	qcIt, err := db.QCs(true)
	if err != nil {
		return prev, highest, fmt.Errorf("failed to open reverse QC iterator: %w", err)
	}
	defer func() { _ = qcIt.Close() }()
	ok, err = qcIt.Next()
	if err != nil {
		return prev, highest, fmt.Errorf("failed to read newest QC: %w", err)
	}
	if ok {
		fqc, err := qcIt.QC()
		if err != nil {
			return prev, highest, fmt.Errorf("failed to decode newest QC: %w", err)
		}
		prev = tmutils.Some(fqc.QC())
	}

	return prev, highest, nil
}

// buildCommittee creates a round-robin committee of the given size along with
// the secret keys that sign its QCs, with global numbering starting at 0.
func buildCommittee(rng tmutils.Rng, size int) (*types.Committee, []types.SecretKey, error) {
	keys := make([]types.SecretKey, size)
	replicas := make([]types.PublicKey, size)
	for i := range keys {
		keys[i] = types.GenSecretKey(rng)
		replicas[i] = keys[i].Public()
	}
	committee, err := types.NewRoundRobinElection(replicas)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build committee: %w", err)
	}
	return committee, keys, nil
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

// handleNextBatch persists one batch: its QC, then all of its blocks. The QC is
// written first because the BlockDB contract requires a covering QC before any
// block it covers (WriteBlock rejects an uncovered block). It is deliberately
// atomic with respect to shutdown — it writes the whole batch and only returns
// early on a write *error*, never on context cancellation (run observes shutdown
// solely between batches, and the only mid-batch ctx use, maybeThrottle, merely
// stops throttling and proceeds with the write). Combined with flushes happening
// only after the full batch (here, in suspend, and in teardown), a cleanly
// shut-down store always ends at a complete batch boundary (highest block ==
// last QC's Next-1), so it resumes with no gap. A hard crash may leave the QC
// ahead of its blocks (never a block without its QC); resume backfills the gap.
// Do NOT add a mid-batch ctx abort: it would let a clean shutdown leave a
// partial batch.
func (b *BlockSim) handleNextBatch(batch *generatedBatch) {
	b.metrics.SetMainThreadPhase("write_qc")
	if err := b.db.WriteQC(batch.first, batch.next, batch.qc); err != nil {
		fmt.Printf("failed to write QC: %v\n", err)
		b.cancel()
		return
	}
	b.totalQCsWritten++
	b.metrics.ReportQCWritten()

	b.metrics.SetMainThreadPhase("write_block")
	for i, blk := range batch.blocks {
		b.maybeThrottle()
		n := batch.first + types.GlobalBlockNumber(i) //nolint:gosec // batch index is small and non-negative
		if err := b.db.WriteBlock(n, blk); err != nil {
			fmt.Printf("failed to write block %d: %v\n", n, err)
			b.cancel()
			return
		}
		blockBytes := payloadBytes(blk)
		b.totalBlocksWritten++
		b.totalBytesWritten += blockBytes
		b.highestBlockHeight = uint64(n)
		b.metrics.ReportBlockWritten(blockBytes)
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

	// Periodic prune.
	if b.highestBlockHeight > b.config.UnprunedBlocks &&
		b.highestBlockHeight-b.lastPrunedAt >= b.config.PruneIntervalBlocks {
		b.metrics.SetMainThreadPhase("prune")
		lowestToKeep := b.highestBlockHeight - b.config.UnprunedBlocks
		if err := b.db.PruneBefore(types.GlobalBlockNumber(lowestToKeep)); err != nil {
			fmt.Printf("failed to prune: %v\n", err)
			b.cancel()
			return
		}
		b.lastPrunedAt = b.highestBlockHeight
		b.metrics.ReportPrune()
		b.metrics.RecordLowestHeight(lowestToKeep)
	}
}

// payloadBytes returns the total size of a block's payload transactions.
func payloadBytes(blk *types.Block) int64 {
	var total int64
	for _, tx := range blk.Payload().ToBuilder().Txs {
		total += int64(len(tx))
	}
	return total
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

// openBlockDB creates a types.BlockDB for the configured backend.
func openBlockDB(config *BlocksimConfig) (types.BlockDB, error) {
	switch config.Backend {
	case "mem":
		return memblock.NewBlockDB(), nil
	case "litt":
		littConfig, err := littblock.DefaultConfig(config.DataDir)
		if err != nil {
			return nil, fmt.Errorf("failed to build litt block db config: %w", err)
		}
		littConfig.Retention = time.Duration(config.LittRetentionSeconds) * time.Second
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
