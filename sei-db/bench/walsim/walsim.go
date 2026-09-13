package walsim

import (
	"context"
	"fmt"
	"os"
	"time"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"golang.org/x/time/rate"
)

// WalSim is the benchmark runner for the walsim benchmark.
type WalSim struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *WalsimConfig

	store     walStore
	generator *RecordGenerator
	metrics   *WalsimMetrics

	// The index to assign to the next appended record.
	nextIndex uint64

	// The number of records to retain on disk, derived from TargetDiskSizeBytes. 0 disables pruning.
	retainedRecords uint64

	// Console reporting state.
	consoleUpdatePeriod          time.Duration
	lastConsoleUpdateTime        time.Time
	lastConsoleUpdateRecordCount int64
	startTimestamp               time.Time
	totalRecordsWritten          int64
	totalBytesWritten            int64
	highestIndex                 uint64

	// A message is sent on this channel when the benchmark is fully stopped.
	closeChan chan struct{}

	// Suspend/resume toggle channel.
	suspendChan chan bool

	// Enforces a maximum record write rate (if enabled).
	rateLimiter *rate.Limiter
}

// NewWalSim creates a new walsim benchmark runner and starts it.
func NewWalSim(
	ctx context.Context,
	config *WalsimConfig,
	metrics *WalsimMetrics,
) (*WalSim, error) {

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

	fmt.Printf("Running walsim benchmark (%s backend) from data directory: %s\n", config.Backend, config.DataDir)
	fmt.Printf("Logs are being routed to: %s\n", config.LogDir)

	fmt.Printf("Initializing random number generator.\n")
	// Pre-generate a random buffer once; all record data slices into it (zero-copy) so the generator
	// never runs math/rand on the hot path.
	cannedRand := crand.NewCannedRandom(int(config.RandomDataBufferSizeBytes), config.Seed) //nolint:gosec // buffer size is bounded by config

	ctx, cancel := context.WithCancel(ctx)

	store, err := openWALStore(ctx, config)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	// Resume after any existing history instead of colliding with on-disk records.
	ok, _, last, err := store.Bounds()
	if err != nil {
		cancel()
		_ = store.Close()
		return nil, fmt.Errorf("failed to read WAL bounds: %w", err)
	}
	var highest uint64
	var nextIndex uint64 = 1
	if ok {
		highest = last
		nextIndex = last + 1
		fmt.Printf("Resuming from index %d.\n", highest)
	}

	var retainedRecords uint64
	if config.TargetDiskSizeBytes > 0 {
		retainedRecords = config.TargetDiskSizeBytes / config.RecordSizeBytes
	}

	generator := NewRecordGenerator(
		ctx,
		cannedRand,
		int(config.RecordSizeBytes),       //nolint:gosec // record size is bounded by config
		int(config.StagedRecordQueueSize), //nolint:gosec // queue size is bounded by config
	)

	consoleUpdatePeriod := time.Duration(config.ConsoleUpdateIntervalSeconds * float64(time.Second))

	var rateLimiter *rate.Limiter
	if config.MaxRecordsPerSecond > 0 {
		rateLimiter = rate.NewLimiter(rate.Limit(config.MaxRecordsPerSecond), 1)
	}

	start := time.Now()

	b := &WalSim{
		ctx:                   ctx,
		cancel:                cancel,
		config:                config,
		store:                 store,
		generator:             generator,
		metrics:               metrics,
		nextIndex:             nextIndex,
		retainedRecords:       retainedRecords,
		consoleUpdatePeriod:   consoleUpdatePeriod,
		lastConsoleUpdateTime: start,
		startTimestamp:        start,
		highestIndex:          highest,
		closeChan:             make(chan struct{}, 1),
		suspendChan:           make(chan bool, 1),
		rateLimiter:           rateLimiter,
	}

	go b.run()
	return b, nil
}

// The main loop of the benchmark.
func (b *WalSim) run() {
	defer b.teardown()

	var timeoutChan <-chan time.Time
	if b.config.MaxRuntimeSeconds > 0 {
		timeoutChan = time.After(time.Duration(b.config.MaxRuntimeSeconds) * time.Second)
	}

	for {
		b.metrics.SetMainThreadPhase("get_record")

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
		case record := <-b.generator.recordChan:
			b.maybeThrottle()
			b.handleNextRecord(record)
		}

		b.generateConsoleReport(false)
	}
}

func (b *WalSim) maybeThrottle() {
	if b.rateLimiter == nil {
		return
	}
	b.metrics.SetMainThreadPhase("throttling")
	if err := b.rateLimiter.Wait(b.ctx); err != nil {
		return
	}
}

// handleNextRecord appends one record, then applies the periodic flush and size-targeted prune.
func (b *WalSim) handleNextRecord(record []byte) {
	b.metrics.SetMainThreadPhase("append")
	index := b.nextIndex
	if err := b.store.Append(index, record); err != nil {
		fmt.Printf("failed to append record %d: %v\n", index, err)
		b.cancel()
		return
	}
	b.nextIndex++
	b.totalRecordsWritten++
	b.totalBytesWritten += int64(len(record))
	b.highestIndex = index
	b.metrics.ReportRecordWritten(int64(len(record)))
	b.metrics.RecordHighestIndex(b.highestIndex)

	// Periodic flush.
	if b.config.FlushIntervalRecords > 0 && b.totalRecordsWritten%int64(b.config.FlushIntervalRecords) == 0 { //nolint:gosec
		b.metrics.SetMainThreadPhase("flush")
		if err := b.store.Flush(); err != nil {
			fmt.Printf("failed to flush: %v\n", err)
			b.cancel()
			return
		}
		b.metrics.ReportFlush()
	}

	// Size-targeted prune: hold the on-disk data as close to TargetDiskSizeBytes as possible by
	// keeping the most recent retainedRecords records.
	if b.retainedRecords > 0 && b.highestIndex >= b.retainedRecords {
		b.metrics.SetMainThreadPhase("prune")
		lowestToKeep := b.highestIndex - b.retainedRecords + 1
		if err := b.store.PruneBefore(lowestToKeep); err != nil {
			fmt.Printf("failed to prune: %v\n", err)
			b.cancel()
			return
		}
		b.metrics.ReportPruneRequest()
		b.metrics.RecordLowestIndex(lowestToKeep)
	}
}

func (b *WalSim) suspend() {
	// Flush before suspending so state is durable.
	if err := b.store.Flush(); err != nil {
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
			b.totalRecordsWritten = 0
			b.totalBytesWritten = 0
			b.startTimestamp = time.Now()
			fmt.Printf("Benchmark resumed.\n")
			return
		}
	}
}

func (b *WalSim) teardown() {
	fmt.Printf("Flushing and closing WAL.\n")
	if err := b.store.Flush(); err != nil {
		fmt.Printf("failed to flush during teardown: %v\n", err)
	}
	if err := b.store.Close(); err != nil {
		fmt.Printf("failed to close WAL: %v\n", err)
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

func (b *WalSim) generateConsoleReport(force bool) {
	now := time.Now()
	timeSinceLastUpdate := now.Sub(b.lastConsoleUpdateTime)
	recordsSinceLastUpdate := b.totalRecordsWritten - b.lastConsoleUpdateRecordCount

	if !force &&
		timeSinceLastUpdate < b.consoleUpdatePeriod &&
		recordsSinceLastUpdate < int64(b.config.ConsoleUpdateIntervalRecords) { //nolint:gosec
		return
	}

	b.lastConsoleUpdateTime = now
	b.lastConsoleUpdateRecordCount = b.totalRecordsWritten

	elapsed := now.Sub(b.startTimestamp)
	bytesPerSecond := float64(b.totalBytesWritten) / elapsed.Seconds()
	recordsPerSecond := float64(b.totalRecordsWritten) / elapsed.Seconds()

	fmt.Printf("%s records in %s | %s written | %s/sec | %s rec/sec      \r",
		utils.Int64Commas(b.totalRecordsWritten),
		utils.FormatDuration(elapsed, 1),
		utils.FormatBytes(b.totalBytesWritten),
		utils.FormatBytes(int64(bytesPerSecond)),
		utils.Int64Commas(int64(recordsPerSecond)))
}

// BlockUntilHalted blocks until the benchmark has halted.
func (b *WalSim) BlockUntilHalted() {
	<-b.closeChan
	b.closeChan <- struct{}{}
}

// Close shuts down the benchmark and releases resources.
func (b *WalSim) Close() error {
	b.cancel()
	<-b.closeChan
	b.closeChan <- struct{}{}
	fmt.Printf("Benchmark terminated successfully.\n")
	return nil
}

// Suspend the benchmark. Call Resume() to continue.
func (b *WalSim) Suspend() {
	select {
	case <-b.ctx.Done():
	case b.suspendChan <- true:
	}
}

// Resume the benchmark after a Suspend().
func (b *WalSim) Resume() {
	select {
	case <-b.ctx.Done():
	case b.suspendChan <- false:
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
