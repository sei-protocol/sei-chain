package gigasim

import (
	"fmt"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	autobahn "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

var _ utils.Config = (*GigasimConfig)(nil)

// GigasimConfig is the full set of options a gigasim run takes. Fields in the JSON config file mirror
// these names exactly, and anything left out keeps its default.
type GigasimConfig struct {

	// The number of transactions in each simulated block. Every transaction is executed against the
	// state DB and contributes one entry to the block payload written to the block store.
	TransactionsPerBlock int

	// The size of each simulated transaction in the block payload, in bytes. This governs the block
	// store's write volume and is independent of the state each transaction touches.
	BytesPerTransaction int

	// Throttle block production to this many blocks per second. 0 means unthrottled.
	MaxBlocksPerSecond float64

	// The number of blocks finalized by each generated QC. One QC is written per batch of this many
	// blocks, matching how consensus commits a range at a time.
	BlocksPerQc uint64

	// The capacity of the queue holding generated blocks before the benchmark consumes them. A larger
	// queue lets the generator run further ahead of the pipeline.
	StagedBlockQueueSize int

	// How often to flush the block store, in blocks. 0 never flushes explicitly.
	FlushIntervalBlocks int

	// The number of hot accounts. Hot accounts are chosen far more often than any other account and
	// their count does not change once the benchmark starts.
	NumberOfHotAccounts int

	// The number of cold accounts to create before the benchmark starts. Cold accounts are chosen
	// occasionally, and the population grows as new accounts are created.
	MinimumNumberOfColdAccounts int

	// The number of dormant accounts to create before the benchmark starts. Dormant accounts are never
	// chosen for a transaction; they exist to give the state DB a realistic resident size.
	MinimumNumberOfDormantAccounts int

	// The probability in [0,1] that a transaction picks one of its accounts from the hot set.
	HotAccountProbability float64

	// The probability in [0,1] that a non-hot account selection creates a new account instead of
	// reusing a cold one.
	NewAccountProbability float64

	// The probability in [0,1] that a newly created account is dormant rather than cold.
	NewAccountDormancyProbability float64

	// The number of ERC20 contracts to create before the benchmark starts.
	MinimumNumberOfErc20Contracts int

	// The number of ERC20 contracts in the hot set, which are the low-numbered contracts.
	HotErc20ContractSetSize int

	// The probability in [0,1] that a transaction picks its ERC20 contract from the hot set.
	HotErc20ContractProbability float64

	// The size in bytes of an ERC20 contract's code record.
	Erc20ContractSize int

	// The number of distinct ERC20 storage slots each account may touch.
	Erc20InteractionsPerAccount int

	// How many blocks behind head the storage layer must remain able to roll back to. Every store keeps
	// at least this much history regardless of the lookback window.
	RollbackWindow uint64

	// How much queryable history is kept below the rollback window, in blocks. -1 keeps history forever.
	LookbackWindow int64

	// How often the storage garbage collector runs a prune cycle, in seconds. Must be positive.
	PruneIntervalSeconds int

	// The wall-clock gap between checkpoints, in seconds. 0 disables time-driven checkpoints.
	CheckpointIntervalSeconds int

	// Checkpoints are eligible only at heights that are multiples of this value. 0 accepts any height.
	CheckpointBlockInterval int64

	// If true, the historical EVM state store is opened and every committed block is written to it.
	// When false the store is never opened and nothing is written to it.
	EnableSS bool

	// If true, the receipt store is opened and each block's receipts are written to it. When false no
	// receipts are built and the store is never opened.
	EnableReceiptStore bool

	// The number of executor threads per CPU core. The total thread count is this times the core count,
	// plus ConstantThreadCount, floored at 1.
	ThreadsPerCore float64

	// A fixed number of executor threads added to the per-core count.
	ConstantThreadCount int

	// The fraction in [0,1] of transactions that record detailed per-phase timings. Sampling keeps the
	// cost of instrumentation off the hot path.
	TransactionMetricsSampleRate float64

	// The most block hashes the benchmark may run ahead of the state DB's hasher before it waits. 0
	// makes every block wait for its own hash, which is what a node does.
	MaxHashLagBlocks int

	// The seed for the random number generator. Changing this against an existing data directory gives
	// undefined behavior; only change it when starting a run from scratch.
	Seed int64

	// The size in bytes of the pre-generated random buffer that all simulated data is sliced from.
	CannedRandomSize int

	// The directory holding every database the benchmark opens.
	DataDir string

	// If this many seconds pass without a console update, the benchmark prints a report.
	ConsoleUpdateIntervalSeconds float64

	// If this many blocks are processed without a console update, the benchmark prints a report.
	ConsoleUpdateIntervalBlocks int

	// How long to run the benchmark for, in seconds. 0 runs until interrupted.
	MaxRuntimeSeconds int

	// Address for the Prometheus metrics HTTP server (e.g. ":9090"). Empty disables metrics serving.
	MetricsAddr string

	// How often to scrape background metrics such as data directory size, in seconds. 0 disables them.
	BackgroundMetricsScrapeInterval int

	// If true, pressing Enter in the terminal toggles suspend/resume.
	EnableSuspension bool

	// Directory for seilog output files. Supports ~ expansion and relative paths.
	LogDir string

	// Log level for seilog output. One of debug, info, warn, error.
	LogLevel string

	// If true, delete the contents of DataDir and LogDir before opening the databases.
	CleanDataOnStart bool

	// If true, delete the contents of DataDir and LogDir after the benchmark finishes.
	CleanDataOnExit bool

	// This field is ignored, but allows for a comment to be added to the config file.
	Comment string
}

// DefaultGigasimConfig returns the configuration a run takes when its file sets nothing: a full node's
// stack driven at the largest block consensus accepts.
func DefaultGigasimConfig() *GigasimConfig {
	return &GigasimConfig{
		TransactionsPerBlock:            2000,
		BytesPerTransaction:             1024,
		MaxBlocksPerSecond:              0,
		BlocksPerQc:                     1,
		StagedBlockQueueSize:            8,
		FlushIntervalBlocks:             10,
		NumberOfHotAccounts:             10_000,
		MinimumNumberOfColdAccounts:     1_000_000,
		MinimumNumberOfDormantAccounts:  100_000_000,
		HotAccountProbability:           0.5,
		NewAccountProbability:           0.01,
		NewAccountDormancyProbability:   0.5,
		MinimumNumberOfErc20Contracts:   1_000,
		HotErc20ContractSetSize:         10,
		HotErc20ContractProbability:     0.5,
		Erc20ContractSize:               4096,
		Erc20InteractionsPerAccount:     8,
		RollbackWindow:                  1_000,
		LookbackWindow:                  0,
		PruneIntervalSeconds:            300,
		CheckpointIntervalSeconds:       60,
		CheckpointBlockInterval:         0,
		EnableSS:                        true,
		EnableReceiptStore:              true,
		ThreadsPerCore:                  2,
		ConstantThreadCount:             0,
		TransactionMetricsSampleRate:    0.01,
		MaxHashLagBlocks:                1000,
		Seed:                            1337,
		CannedRandomSize:                64 * 1024 * 1024, // 64 MiB
		DataDir:                         "data",
		ConsoleUpdateIntervalSeconds:    1,
		ConsoleUpdateIntervalBlocks:     1_000,
		MaxRuntimeSeconds:               0,
		MetricsAddr:                     ":9090",
		BackgroundMetricsScrapeInterval: 60,
		EnableSuspension:                true,
		LogDir:                          "logs",
		LogLevel:                        "info",
	}
}

// blockPayloadBytes is the size of one generated block's payload: the transaction bytes a real block
// of this shape would carry.
func (c *GigasimConfig) blockPayloadBytes() int {
	return c.TransactionsPerBlock * c.BytesPerTransaction
}

// storageConfig builds the config the storage manager opens every database from. DataDir must already
// be resolved to an absolute path, since every store's location is derived from it.
func (c *GigasimConfig) storageConfig() (*config.GigaStorageConfig, error) {
	storage, err := config.DefaultGigaStorageConfig(c.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to build the storage config: %w", err)
	}

	storage.SSConfig.Enable = c.EnableSS
	storage.ReceiptDBConfig.Enable = c.EnableReceiptStore

	storage.PruningConfig.RollbackWindow = c.RollbackWindow
	storage.PruningConfig.LookbackWindow = c.LookbackWindow
	storage.PruningConfig.PruneInterval = time.Duration(c.PruneIntervalSeconds) * time.Second

	storage.CheckpointConfig.TimeInterval = time.Duration(c.CheckpointIntervalSeconds) * time.Second
	storage.CheckpointConfig.BlockInterval = c.CheckpointBlockInterval

	return storage, nil
}

// Validate reports the first unusable setting, or nil when the whole configuration is sound.
func (c *GigasimConfig) Validate() error {
	if err := c.validateBlockShape(); err != nil {
		return err
	}
	if err := c.validateAccountDistribution(); err != nil {
		return err
	}
	if err := c.validateRetention(); err != nil {
		return err
	}
	return c.validateRuntime()
}

// validateBlockShape checks the generated block against the ceilings consensus places on a real one,
// so the benchmark cannot be configured to write blocks the chain could never produce.
func (c *GigasimConfig) validateBlockShape() error {
	if c.TransactionsPerBlock < 1 || uint64(c.TransactionsPerBlock) > autobahn.MaxTxsPerBlock {
		return fmt.Errorf("TransactionsPerBlock must be in [1, %d] (got %d)",
			autobahn.MaxTxsPerBlock, c.TransactionsPerBlock)
	}
	// Each factor is bounded before the product is taken, both because a single oversized transaction is
	// its own error and because it keeps the multiplication below well inside the range of an int.
	if c.BytesPerTransaction < 1 || c.BytesPerTransaction > int(autobahn.MaxTxsBytesPerBlock) {
		return fmt.Errorf("BytesPerTransaction must be in [1, %d] (got %d)",
			autobahn.MaxTxsBytesPerBlock, c.BytesPerTransaction)
	}
	if c.blockPayloadBytes() > int(autobahn.MaxTxsBytesPerBlock) {
		return fmt.Errorf("TransactionsPerBlock*BytesPerTransaction must be at most %d (got %d)",
			autobahn.MaxTxsBytesPerBlock, c.blockPayloadBytes())
	}
	if c.BlocksPerQc < 1 {
		return fmt.Errorf("BlocksPerQc must be at least 1 (got %d)", c.BlocksPerQc)
	}
	if c.StagedBlockQueueSize < 1 {
		return fmt.Errorf("StagedBlockQueueSize must be at least 1 (got %d)", c.StagedBlockQueueSize)
	}
	if c.FlushIntervalBlocks < 0 {
		return fmt.Errorf("FlushIntervalBlocks must be non-negative (got %d)", c.FlushIntervalBlocks)
	}
	if c.MaxBlocksPerSecond < 0 {
		return fmt.Errorf("MaxBlocksPerSecond must be non-negative (got %f)", c.MaxBlocksPerSecond)
	}
	return nil
}

// validateAccountDistribution checks the account and contract populations, and the probabilities
// transactions select them with.
func (c *GigasimConfig) validateAccountDistribution() error {
	if c.NumberOfHotAccounts < 1 {
		return fmt.Errorf("NumberOfHotAccounts must be at least 1 (got %d)", c.NumberOfHotAccounts)
	}
	if c.MinimumNumberOfColdAccounts < 1 {
		return fmt.Errorf("MinimumNumberOfColdAccounts must be at least 1 (got %d)", c.MinimumNumberOfColdAccounts)
	}
	if c.MinimumNumberOfDormantAccounts < 0 {
		return fmt.Errorf("MinimumNumberOfDormantAccounts must be non-negative (got %d)",
			c.MinimumNumberOfDormantAccounts)
	}
	for _, p := range []struct {
		name  string
		value float64
	}{
		{"HotAccountProbability", c.HotAccountProbability},
		{"NewAccountProbability", c.NewAccountProbability},
		{"NewAccountDormancyProbability", c.NewAccountDormancyProbability},
		{"HotErc20ContractProbability", c.HotErc20ContractProbability},
		{"TransactionMetricsSampleRate", c.TransactionMetricsSampleRate},
	} {
		if p.value < 0 || p.value > 1 {
			return fmt.Errorf("%s must be in [0, 1] (got %f)", p.name, p.value)
		}
	}
	if c.HotErc20ContractSetSize < 1 {
		return fmt.Errorf("HotErc20ContractSetSize must be at least 1 (got %d)", c.HotErc20ContractSetSize)
	}
	// The cold selection path draws from the contracts above the hot set, so there has to be at least one.
	if c.MinimumNumberOfErc20Contracts <= c.HotErc20ContractSetSize {
		return fmt.Errorf("MinimumNumberOfErc20Contracts must exceed HotErc20ContractSetSize %d (got %d)",
			c.HotErc20ContractSetSize, c.MinimumNumberOfErc20Contracts)
	}
	if c.Erc20ContractSize < 1 {
		return fmt.Errorf("Erc20ContractSize must be at least 1 (got %d)", c.Erc20ContractSize)
	}
	if c.Erc20InteractionsPerAccount < 1 {
		return fmt.Errorf("Erc20InteractionsPerAccount must be at least 1 (got %d)", c.Erc20InteractionsPerAccount)
	}
	return nil
}

// validateRetention checks how much history the stores keep, and the cadence of the prune and
// checkpoint cycles that enforce it.
func (c *GigasimConfig) validateRetention() error {
	if c.LookbackWindow < -1 {
		return fmt.Errorf("LookbackWindow must be >= 0, or -1 for infinite retention (got %d)", c.LookbackWindow)
	}
	if c.PruneIntervalSeconds < 1 {
		return fmt.Errorf("PruneIntervalSeconds must be at least 1 (got %d)", c.PruneIntervalSeconds)
	}
	if c.CheckpointIntervalSeconds < 0 {
		return fmt.Errorf("CheckpointIntervalSeconds must be non-negative (got %d)", c.CheckpointIntervalSeconds)
	}
	if c.CheckpointBlockInterval < 0 {
		return fmt.Errorf("CheckpointBlockInterval must be non-negative (got %d)", c.CheckpointBlockInterval)
	}
	if c.MaxHashLagBlocks < 0 {
		return fmt.Errorf("MaxHashLagBlocks must be non-negative (got %d)", c.MaxHashLagBlocks)
	}
	return nil
}

// validateRuntime checks the executor pool, the directories, and the console and metrics settings.
func (c *GigasimConfig) validateRuntime() error {
	if c.ThreadsPerCore < 0 {
		return fmt.Errorf("ThreadsPerCore must be non-negative (got %f)", c.ThreadsPerCore)
	}
	if c.ConstantThreadCount < 0 {
		return fmt.Errorf("ConstantThreadCount must be non-negative (got %d)", c.ConstantThreadCount)
	}
	// Every simulated value is sliced out of the canned buffer, and the largest single draw is a whole
	// block payload, which the buffer panics on if it cannot serve.
	if minBuffer := c.blockPayloadBytes(); c.CannedRandomSize < minBuffer {
		return fmt.Errorf("CannedRandomSize must be at least %d, the size of one block payload (got %d)",
			minBuffer, c.CannedRandomSize)
	}
	if c.DataDir == "" {
		return fmt.Errorf("DataDir is required")
	}
	if c.LogDir == "" {
		return fmt.Errorf("LogDir is required")
	}
	if c.ConsoleUpdateIntervalSeconds < 0 {
		return fmt.Errorf("ConsoleUpdateIntervalSeconds must be non-negative (got %f)",
			c.ConsoleUpdateIntervalSeconds)
	}
	if c.ConsoleUpdateIntervalBlocks < 0 {
		return fmt.Errorf("ConsoleUpdateIntervalBlocks must be non-negative (got %d)", c.ConsoleUpdateIntervalBlocks)
	}
	if c.MaxRuntimeSeconds < 0 {
		return fmt.Errorf("MaxRuntimeSeconds must be non-negative (got %d)", c.MaxRuntimeSeconds)
	}
	if c.BackgroundMetricsScrapeInterval < 0 {
		return fmt.Errorf("BackgroundMetricsScrapeInterval must be non-negative (got %d)",
			c.BackgroundMetricsScrapeInterval)
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LogLevel must be one of debug, info, warn, error (got %q)", c.LogLevel)
	}
	return nil
}
