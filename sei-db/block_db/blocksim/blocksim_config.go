package blocksim

import (
	"fmt"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
)

const (
	minHashSize         = 20
	minCannedRandomSize = unit.MB
)

var _ utils.Config = (*BlocksimConfig)(nil)

// Configuration for the blocksim benchmark.
type BlocksimConfig struct {

	// The size of each simulated transaction, in bytes. Each transaction in a block will contain
	// this many bytes of random data.
	BytesPerTransaction uint64

	// The number of transactions included in each generated block.
	TransactionsPerBlock uint64

	// Additional bytes of random data added to the block itself, beyond the transaction data. This
	// simulates block-level metadata or other non-transaction payload.
	ExtraBytesPerBlock uint64

	// The size of each block hash, in bytes.
	BlockHashSize uint64

	// The size of each transaction hash, in bytes.
	TransactionHashSize uint64

	// The capacity of the queue that holds generated blocks before they are consumed by the
	// benchmark. A larger queue allows the block generator to run further ahead of the consumer.
	StagedBlockQueueSize uint64

	// The size of the CannedRandom buffer, in bytes. Altering this value for a pre-existing run
	// will change the random data generated, don't change it unless you are starting a new run
	// from scratch.
	CannedRandomSize uint64

	// The number of blocks to keep in the database after pruning.
	UnprunedBlocks uint64

	// The seed to use for the random number generator. Altering this seed for a pre-existing DB
	// will result in undefined behavior, don't change the seed unless you are starting a new run
	// from scratch.
	Seed int64

	// The directory to store the benchmark data.
	DataDir string

	// The BlockDB backend to use. Currently only "mem" is supported.
	Backend string

	// If this many seconds go by without a console update, the benchmark will print a report.
	ConsoleUpdateIntervalSeconds float64

	// If this many blocks are written without a console update, the benchmark will print a report.
	// Prevents console spam when throughput is very high.
	ConsoleUpdateIntervalBlocks uint64

	// The amount of time to run the benchmark for, in seconds. If 0, runs until interrupted.
	MaxRuntimeSeconds int

	// Address for the Prometheus metrics HTTP server (e.g. ":9090"). If empty, metrics are disabled.
	MetricsAddr string

	// If true, pressing Enter in the terminal will toggle suspend/resume of the benchmark.
	EnableSuspension bool

	// How often (in blocks) to call Flush() on the database. 0 means never explicitly flush.
	FlushIntervalBlocks uint64

	// How often (in seconds) to scrape background metrics (data dir size, uptime).
	// If 0, background metrics are disabled.
	BackgroundMetricsScrapeInterval int

	// Directory for seilog output files. Supports ~ expansion and relative paths.
	LogDir string

	// Log level for seilog output. Valid values: debug, info, warn, error.
	LogLevel string

	// If true, delete the contents of DataDir before opening the database.
	CleanDataOnStart bool

	// If true, delete the contents of LogDir before starting.
	CleanLogsOnStart bool

	// Throttle block write rate to this many blocks per second. 0 = disabled (unlimited throughput).
	// Useful for testing steady-state performance rather than max-throughput thrashing.
	MaxBlocksPerSecond float64

	// This field is ignored, but allows for a comment to be added to the config file.
	Comment string
}

// Returns the default configuration for the blocksim benchmark.
func DefaultBlocksimConfig() *BlocksimConfig {
	return &BlocksimConfig{
		BytesPerTransaction:             512,
		TransactionsPerBlock:            1024,
		ExtraBytesPerBlock:              256,
		BlockHashSize:                   32,
		TransactionHashSize:             32,
		StagedBlockQueueSize:            8,
		CannedRandomSize:                unit.GB,
		UnprunedBlocks:                  100_000,
		Seed:                            1337,
		DataDir:                         "data",
		Backend:                         "mem",
		ConsoleUpdateIntervalSeconds:    1,
		ConsoleUpdateIntervalBlocks:     10_000,
		MaxRuntimeSeconds:               0,
		MetricsAddr:                     ":9090",
		EnableSuspension:                true,
		FlushIntervalBlocks:             0,
		BackgroundMetricsScrapeInterval: 60,
		LogDir:                          "logs",
		LogLevel:                        "info",
		CleanDataOnStart:                false,
		CleanLogsOnStart:                false,
		MaxBlocksPerSecond:              0,
	}
}

// Validate checks that the configuration is sane and returns an error if not.
func (c *BlocksimConfig) Validate() error {
	if c.BytesPerTransaction < 1 {
		return fmt.Errorf("BytesPerTransaction must be at least 1 (got %d)", c.BytesPerTransaction)
	}
	if c.TransactionsPerBlock < 1 {
		return fmt.Errorf("TransactionsPerBlock must be at least 1 (got %d)", c.TransactionsPerBlock)
	}
	if c.BlockHashSize < minHashSize {
		return fmt.Errorf("BlockHashSize must be at least %d (got %d)", minHashSize, c.BlockHashSize)
	}
	if c.TransactionHashSize < minHashSize {
		return fmt.Errorf("TransactionHashSize must be at least %d (got %d)", minHashSize, c.TransactionHashSize)
	}
	if c.StagedBlockQueueSize < 1 {
		return fmt.Errorf("StagedBlockQueueSize must be at least 1 (got %d)", c.StagedBlockQueueSize)
	}
	if c.CannedRandomSize < minCannedRandomSize {
		return fmt.Errorf("CannedRandomSize must be at least %d (got %d)",
			minCannedRandomSize, c.CannedRandomSize)
	}
	if c.UnprunedBlocks < 1 {
		return fmt.Errorf("UnprunedBlocks must be at least 1 (got %d)", c.UnprunedBlocks)
	}
	if c.DataDir == "" {
		return fmt.Errorf("DataDir is required")
	}
	if c.LogDir == "" {
		return fmt.Errorf("LogDir is required")
	}
	if c.ConsoleUpdateIntervalSeconds < 0 {
		return fmt.Errorf("ConsoleUpdateIntervalSeconds must be non-negative (got %f)", c.ConsoleUpdateIntervalSeconds)
	}
	if c.MaxRuntimeSeconds < 0 {
		return fmt.Errorf("MaxRuntimeSeconds must be non-negative (got %d)", c.MaxRuntimeSeconds)
	}
	if c.BackgroundMetricsScrapeInterval < 0 {
		return fmt.Errorf("BackgroundMetricsScrapeInterval must be non-negative (got %d)", c.BackgroundMetricsScrapeInterval)
	}
	if c.MaxBlocksPerSecond < 0 {
		return fmt.Errorf("MaxBlocksPerSecond must be non-negative (got %f)", c.MaxBlocksPerSecond)
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LogLevel must be one of debug, info, warn, error (got %q)", c.LogLevel)
	}
	return nil
}
