package benchmark

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/litt"
	"github.com/Layr-Labs/eigenda/litt/benchmark/config"
	"github.com/Layr-Labs/eigenda/litt/littbuilder"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
	"github.com/docker/go-units"
	"golang.org/x/time/rate"
)

// BenchmarkEngine is a tool for benchmarking LittDB performance.
type BenchmarkEngine struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger logging.Logger

	// The configuration for the benchmark.
	config *config.BenchmarkConfig

	// The database to be benchmarked.
	db litt.DB

	// The table in the database where data is stored.
	table litt.Table

	// Keeps track of data to read and write.
	dataTracker *DataTracker

	// The maximum write throughput in bytes per second for each worker thread.
	writeBytesPerSecondPerThread uint64

	// The maximum read throughput in bytes per second for each worker thread.
	readBytesPerSecondPerThread uint64

	// The burst size for write rate limiting.
	writeBurstSize uint64

	// The burst size for read rate limiting.
	readBurstSize uint64

	// Records benchmark metrics.
	metrics *metrics

	// errorMonitor is used to handle fatal errors in the benchmark engine.
	errorMonitor *util.ErrorMonitor
}

// NewBenchmarkEngine creates a new BenchmarkEngine with the given configuration.
func NewBenchmarkEngine(configPath string) (*BenchmarkEngine, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config file %s: %w", configPath, err)
	}

	cfg.LittConfig.Logger, err = common.NewLogger(cfg.LittConfig.LoggerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	cfg.LittConfig.ShardingFactor = uint32(len(cfg.LittConfig.Paths))

	db, err := littbuilder.NewDB(cfg.LittConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create db: %w", err)
	}

	table, err := db.GetTable("benchmark")
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	ttl := time.Duration(cfg.TTLHours * float64(time.Hour))
	err = table.SetTTL(ttl)
	if err != nil {
		return nil, fmt.Errorf("failed to set TTL for table: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	errorMonitor := util.NewErrorMonitor(ctx, cfg.LittConfig.Logger, nil)

	dataTracker, err := NewDataTracker(ctx, cfg, errorMonitor)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create data tracker: %w", err)
	}

	writeBytesPerSecond := uint64(cfg.MaximumWriteThroughputMB * float64(units.MiB))
	writeBytesPerSecondPerThread := writeBytesPerSecond / uint64(cfg.WriterParallelism)

	// If we set the write burst size smaller than an individual value, then the rate limiter will never
	// permit any writes. Ideally, we'd just set the burst size to 0 since we don't want bursty/volatile writes,
	// but since we are using the rate.Limiter utility, we are required to set a burst size, and a burst size
	// smaller than an individual value will cause the rate limiter to never permit writes.
	writeBurstSize := uint64(cfg.ValueSizeMB * float64(units.MiB))

	readBytesPerSecond := uint64(cfg.MaximumReadThroughputMB * float64(units.MiB))
	readBytesPerSecondPerThread := readBytesPerSecond / uint64(cfg.ReaderParallelism)

	// If we set the read burst size smaller than an individual value we need to read, then the rate limiter will
	// never permit us to read that value.
	readBurstSize := dataTracker.LargestReadableValueSize()

	return &BenchmarkEngine{
		ctx:                          ctx,
		cancel:                       cancel,
		logger:                       cfg.LittConfig.Logger,
		config:                       cfg,
		db:                           db,
		table:                        table,
		dataTracker:                  dataTracker,
		writeBytesPerSecondPerThread: writeBytesPerSecondPerThread,
		readBytesPerSecondPerThread:  readBytesPerSecondPerThread,
		writeBurstSize:               writeBurstSize,
		readBurstSize:                readBurstSize,
		metrics:                      newMetrics(ctx, cfg.LittConfig.Logger, cfg),
		errorMonitor:                 errorMonitor,
	}, nil
}

// Logger returns the logger used by the benchmark engine.
func (b *BenchmarkEngine) Logger() logging.Logger {
	return b.logger
}

// Run executes the benchmark. This method blocks forever, or until the benchmark is stopped via control-C or
// encounters an error.
func (b *BenchmarkEngine) Run() error {

	if b.config.TimeLimitSeconds > 0 {
		// If a time limit is set, create a timer to cancel the context after the specified duration
		timeLimit := time.Duration(b.config.TimeLimitSeconds * float64(time.Second))
		timer := time.NewTimer(timeLimit)

		b.logger.Infof("Benchmark will auto-terminate after %s", timeLimit)

		go func() {
			select {
			case <-timer.C:
				b.logger.Infof("Time limit reached, stopping benchmark.")
				b.cancel()
			case <-b.ctx.Done():
				timer.Stop()
			}
		}()
	}

	// multiply by 2 to make configured value the average
	sleepFactor := b.config.StartupSleepFactorSeconds * float64(time.Second) * 2.0

	for i := 0; i < b.config.WriterParallelism; i++ {
		// Sleep a short time to prevent all goroutines from starting in lockstep.
		time.Sleep(time.Duration(sleepFactor * rand.Float64()))

		go b.writer()
	}

	for i := 0; i < b.config.ReaderParallelism; i++ {
		// Sleep a short time to prevent all goroutines from starting in lockstep.
		time.Sleep(time.Duration(sleepFactor * rand.Float64()))

		go b.reader()
	}

	// Create a channel to listen for OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for signal
	select {
	case <-b.ctx.Done():
		b.logger.Infof("Received shutdown signal, stopping benchmark.")
		return nil
	case <-sigChan:
		// Cancel the context when signal is received
		b.cancel()
	}

	return nil
}

// writer runs on a goroutine and writes data to the database.
func (b *BenchmarkEngine) writer() {
	maxBatchSize := uint64(b.config.BatchSizeMB * float64(units.MiB))
	throttle := rate.NewLimiter(rate.Limit(b.writeBytesPerSecondPerThread), int(b.writeBurstSize))

	for {
		select {
		case <-b.errorMonitor.ImmediateShutdownRequired():
			return
		default:
			batchSize := uint64(0)

			writtenIndices := make([]uint64, 0)

			for batchSize < maxBatchSize {
				writeInfo := b.dataTracker.GetWriteInfo()
				batchSize += uint64(len(writeInfo.Value))

				reservation := throttle.ReserveN(time.Now(), len(writeInfo.Value))
				if !reservation.OK() {
					b.errorMonitor.Panic(fmt.Errorf("failed to reserve write quota for key %s", writeInfo.Key))
					return
				}
				if reservation.Delay() > 0 {
					time.Sleep(reservation.Delay())
				}

				start := time.Now()

				err := b.table.Put(writeInfo.Key, writeInfo.Value)
				if err != nil {
					b.errorMonitor.Panic(fmt.Errorf("failed to write data: %v", err))
					return
				}

				b.metrics.reportWrite(time.Since(start), uint64(len(writeInfo.Value)))
				writtenIndices = append(writtenIndices, writeInfo.KeyIndex)
			}

			start := time.Now()

			err := b.table.Flush()
			if err != nil {
				b.errorMonitor.Panic(fmt.Errorf("failed to flush data: %v", err))
				return
			}

			b.metrics.reportFlush(time.Since(start))

			for _, index := range writtenIndices {
				b.dataTracker.ReportWrite(index)
			}
		}
	}
}

// verifyValue checks if the actual value read from the database matches the expected value.
func (b *BenchmarkEngine) verifyValue(expected *ReadInfo, actual []byte) error {
	if len(actual) != len(expected.Value) {
		return fmt.Errorf("read value size %d does not match expected size %d for key %s",
			len(actual), len(expected.Value), expected.Key)
	}
	for i := range actual {
		if actual[i] != expected.Value[i] {
			return fmt.Errorf("read value does not match expected value for key %s", expected.Key)
		}
	}
	return nil
}

// reader runs on a goroutine and reads data from the database.
func (b *BenchmarkEngine) reader() {
	throttle := rate.NewLimiter(rate.Limit(b.readBytesPerSecondPerThread), int(b.readBurstSize))

	for {
		select {
		case <-b.errorMonitor.ImmediateShutdownRequired():
			return
		default:
			readInfo := b.dataTracker.GetReadInfo()
			if readInfo == nil {
				// This can happen when the context gets cancelled.
				return
			}

			reservation := throttle.ReserveN(time.Now(), len(readInfo.Value))
			if !reservation.OK() {
				b.errorMonitor.Panic(fmt.Errorf("failed to reserve read quota for key %s", readInfo.Key))
				return
			}
			if reservation.Delay() > 0 {
				time.Sleep(reservation.Delay())
			}

			start := time.Now()

			value, exists, err := b.table.Get(readInfo.Key)
			if err != nil {
				b.errorMonitor.Panic(fmt.Errorf("failed to read data: %v", err))
				return
			}

			b.metrics.reportRead(time.Since(start), uint64(len(readInfo.Value)))

			if !exists {
				if b.config.PanicOnReadFailure {
					b.errorMonitor.Panic(fmt.Errorf("key %s not found in database", readInfo.Key))
					return
				} else {
					b.logger.Errorf("key %s not found in database", readInfo.Key)
					continue
				}
			}
			err = b.verifyValue(readInfo, value)
			if err != nil {
				b.errorMonitor.Panic(err)
				return
			}
		}
	}
}
