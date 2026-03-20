package cryptosim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

const (
	minPaddedAccountSize        = 8
	minErc20StorageSlotSize     = 32
	minErc20InteractionsPerAcct = 1
)

// Defines the configuration for the cryptosim benchmark.
type CryptoSimConfig struct {

	// The number of hot accounts. Hot accounts are very frequently used. The number of hot accounts does
	// not change after the benchmark starts.
	//
	// Future work: add different distributions of hot account access. Currently, distribution is flat.
	NumberOfHotAccounts int

	// The minimum number of cold accounts that should be in the DB prior to the start of the benchmark.
	// Cold accounts are occasionally used, but not frequently.
	MinimumNumberOfColdAccounts int

	// The minimum number of dormant accounts that should be in the DB prior to the start of the benchmark.
	// Dormant accounts are not used after they are created.
	MinimumNumberOfDormantAccounts int

	// When creating a new account, this is the probability that the number of dormant accounts will be increased
	// by one. Should be a value between 0.0 and 1.0. A value of 1.0 means that all new account creation will increase
	// the number of dormant accounts. A value of 0.0 means all new account creation will increase the number of
	// cold accounts.
	NewAccountDormancyProbability float64

	// When selecting an account for a transaction, select a hot account with this probability. Should be
	// a value between 0.0 and 1.0.
	HotAccountProbability float64

	// When selecting a non-hot account for a transaction, the benchmark will create a new account with this
	// probability. Should be a value between 0.0 and 1.0.
	NewAccountProbability float64

	// Each account contains an integer value used to track a balance, plus a bunch of random
	// bytes for padding. This is the total size of the account after padding is added.
	PaddedAccountSize int

	// The minimum number of ERC20 contracts that should be in the DB prior to the start of the benchmark.
	// If there are fewer than this number of contracts, the benchmark will first create the necessary
	// contracts before starting its regular operations.
	MinimumNumberOfErc20Contracts int

	// When selecting an ERC20 contract for a transaction, select a hot ERC20 contract with this probability.
	// Should be a value between 0.0 and 1.0.
	HotErc20ContractProbability float64

	// The number of hot ERC20 contracts.
	HotErc20ContractSetSize int

	// The size of the a simulated ERC20 contract, in bytes.
	Erc20ContractSize int

	// The size of a simulated ERC20 storage slot, in bytes.
	Erc20StorageSlotSize int

	// The size of a simulated account balance, in bytes.
	AccountBalanceSize int

	// The number of ERC20 tokens that each account will interact with.
	// Each account will have an eth storage slot for tracking the balance of each ERC20 token it owns.
	// It is not legal to modify this value after the benchmark has started.
	Erc20InteractionsPerAccount int

	// The number of transactions that will be processed in each "block".
	TransactionsPerBlock int

	// Commit is called on the database after this many blocks have been processed.
	BlocksPerCommit int

	// The directory to store the benchmark data.
	DataDir string

	// The seed to use for the random number generator. Altering this seed for a pre-existing DB will result
	// in undefined behavior, don't change the seed unless you are starting a new run from scratch.
	Seed int64

	// The size of the CannedRandom buffer. Similar to the seed, altering this size for a pre-existing DB will result
	// in undefined behavior, don't change the size unless you are starting a new run from scratch.
	CannedRandomSize int

	// The backend to use for the benchmark database.
	Backend wrappers.DBType

	// This field is ignored, but allows for a comment to be added to the config file.
	// Something, something, why in the name of all things holy doesn't json support comments?
	Comment string

	// If this many seconds go by without a console update, the benchmark will print a report to the console.
	ConsoleUpdateIntervalSeconds float64

	// If this many transactions are executed without a console update, the benchmark will print a report to the console.
	ConsoleUpdateIntervalTransactions float64

	// When setting up the benchmark, print a console update after adding this many accounts to the DB.
	SetupUpdateIntervalCount int64

	// Run a number of threads equal to the number of cores on the host machine, multiplied by this value.
	ThreadsPerCore float64

	// Increase or decrease the thread count by this many threads. Total thread count is a function of
	// ThreadsPerCore and ConstantThreadCount.
	ConstantThreadCount int

	// The size of the queue for each transaction executor.
	ExecutorQueueSize int

	// The amount of time to run the benchmark for. If 0, the benchmark will run until it is stopped.
	MaxRuntimeSeconds int

	// Address for the Prometheus metrics HTTP server (e.g. ":9090"). If empty, metrics are disabled.
	MetricsAddr string

	// The probability of capturing detailed metrics about a transaction. Should be a value between 0.0 and 1.0.
	TransactionMetricsSampleRate float64

	// How often (in seconds) to scrape background metrics (data dir size, process I/O).
	// If 0, background metrics are disabled.
	BackgroundMetricsScrapeInterval int

	// If true, pressing Enter in the terminal will toggle suspend/resume of the benchmark.
	// If false, Enter has no effect.
	EnableSuspension bool

	// If true, the data directory and log directory will be deleted on startup if they exist.
	DeleteDataDirOnStartup bool

	// If true, the data directory and log directory will be deleted on a clean shutdown.
	DeleteDataDirOnShutdown bool

	// Configures the FlatKV database. Ignored if Backend is not "FlatKV".
	FlatKVConfig *flatkv.Config

	// The capacity of the channel that holds blocks awaiting execution.
	BlockChannelCapacity int

	// If true, the benchmark will generate receipts for each transaction in each block and
	// feed those receipts into the receipt store.
	GenerateReceipts bool

	// The capacity of the channel that holds blocks sent to the receipt store.
	RecieptChannelCapacity int

	// If true, disables simulation of transaction execution, and writes very little to the database. This is
	// potentially useful when benchmarking things other than state storage (e.g. the receipt store).
	//
	// Note that switching execution on after previously running with execution disabled may result in buggy behavior,
	// as the benchmark will not be properly maintaining DB state when transaction execution is disabled. In order
	// to switch transaction execution back on, it is necessary to delete the on-disk database and start over.
	DisableTransactionExecution bool

	// If greater than 0, the benchmark will throttle the transaction rate to this value, in hertz.
	MaxTPS float64

	// Number of recent blocks to keep before pruning parquet files. 0 disables pruning.
	ReceiptKeepRecent int64

	// Interval in seconds between prune checks. 0 disables pruning.
	ReceiptPruneIntervalSeconds int64

	// Directory for seilog output files. Independent of DataDir so logs and data
	// live in separate trees. Supports ~ expansion and relative paths (resolved
	// from cwd). Must be set, there is no default.
	LogDir string

	// Log level for seilog output. Valid values: debug, info, warn, error.
	LogLevel string
}

// Returns the default configuration for the cryptosim benchmark.
func DefaultCryptoSimConfig() *CryptoSimConfig {

	// Note: if you add new fields or modify default values, be sure to keep config/basic-config.json in sync.
	// That file should contain every available config set to its default value, as a reference.

	cfg := &CryptoSimConfig{
		NumberOfHotAccounts:               100,
		MinimumNumberOfColdAccounts:       1_000_000,
		MinimumNumberOfDormantAccounts:    1_000_000,
		NewAccountDormancyProbability:     1.0,
		HotAccountProbability:             0.1,
		NewAccountProbability:             0.001,
		PaddedAccountSize:                 32,
		MinimumNumberOfErc20Contracts:     10_000,
		HotErc20ContractProbability:       0.5,
		HotErc20ContractSetSize:           100,
		Erc20ContractSize:                 1024 * 2, // 2kb
		Erc20StorageSlotSize:              32,
		AccountBalanceSize:                32,
		Erc20InteractionsPerAccount:       10,
		TransactionsPerBlock:              1024,
		BlocksPerCommit:                   1,
		Seed:                              1337,
		CannedRandomSize:                  1024 * 1024 * 1024, // 1GB
		Backend:                           wrappers.FlatKV,
		ConsoleUpdateIntervalSeconds:      1,
		ConsoleUpdateIntervalTransactions: 1_000_000,
		SetupUpdateIntervalCount:          1_000, // TODO
		ThreadsPerCore:                    2.0,
		ConstantThreadCount:               0,
		ExecutorQueueSize:                 1024,
		MaxRuntimeSeconds:                 0,
		MetricsAddr:                       ":9090",
		TransactionMetricsSampleRate:      0.001,
		BackgroundMetricsScrapeInterval:   60,
		EnableSuspension:                  true,
		DeleteDataDirOnStartup:            false,
		DeleteDataDirOnShutdown:           false,
		FlatKVConfig:                      flatkv.DefaultConfig(),
		BlockChannelCapacity:              8,
		GenerateReceipts:                  false,
		RecieptChannelCapacity:            32,
		DisableTransactionExecution:       false,
		MaxTPS:                            0,
		ReceiptKeepRecent:                 100_000,
		ReceiptPruneIntervalSeconds:       600,
		LogLevel:                          "info",
	}

	return cfg
}

// StringifiedConfig returns the config as human-readable, multi-line JSON.
func (c *CryptoSimConfig) StringifiedConfig() (string, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Validate checks that the configuration is sane and returns an error if not.
func (c *CryptoSimConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("DataDir is required")
	}
	if c.LogDir == "" {
		return fmt.Errorf("LogDir is required")
	}
	if c.PaddedAccountSize < minPaddedAccountSize {
		return fmt.Errorf("PaddedAccountSize must be at least %d (got %d)", minPaddedAccountSize, c.PaddedAccountSize)
	}
	if c.MinimumNumberOfColdAccounts+c.MinimumNumberOfDormantAccounts < 2 {
		return fmt.Errorf("MinimumNumberOfColdAccounts+MinimumNumberOfDormantAccounts must be at least 2 (got %d)",
			c.MinimumNumberOfColdAccounts+c.MinimumNumberOfDormantAccounts)
	}
	if c.NewAccountDormancyProbability < 0 || c.NewAccountDormancyProbability > 1 {
		return fmt.Errorf("NewAccountDormancyProbability must be in [0, 1] (got %f)", c.NewAccountDormancyProbability)
	}
	if c.MinimumNumberOfErc20Contracts < c.HotErc20ContractSetSize+1 {
		return fmt.Errorf("MinimumNumberOfErc20Contracts must be at least HotErc20ContractSetSize+1 (%d)",
			c.HotErc20ContractSetSize+1)
	}
	if c.HotAccountProbability < 0 || c.HotAccountProbability > 1 {
		return fmt.Errorf("HotAccountProbability must be in [0, 1] (got %f)", c.HotAccountProbability)
	}
	if c.NewAccountProbability < 0 || c.NewAccountProbability > 1 {
		return fmt.Errorf("NewAccountProbability must be in [0, 1] (got %f)", c.NewAccountProbability)
	}
	if c.HotErc20ContractProbability < 0 || c.HotErc20ContractProbability > 1 {
		return fmt.Errorf("HotErc20ContractProbability must be in [0, 1] (got %f)", c.HotErc20ContractProbability)
	}
	if c.Erc20StorageSlotSize < minErc20StorageSlotSize {
		return fmt.Errorf("Erc20StorageSlotSize must be at least %d (got %d)",
			minErc20StorageSlotSize, c.Erc20StorageSlotSize)
	}
	if c.Erc20InteractionsPerAccount < minErc20InteractionsPerAcct {
		return fmt.Errorf("Erc20InteractionsPerAccount must be at least %d (got %d)",
			minErc20InteractionsPerAcct, c.Erc20InteractionsPerAccount)
	}
	if c.TransactionsPerBlock < 1 {
		return fmt.Errorf("TransactionsPerBlock must be at least 1 (got %d)", c.TransactionsPerBlock)
	}
	if c.BlocksPerCommit < 1 {
		return fmt.Errorf("BlocksPerCommit must be at least 1 (got %d)", c.BlocksPerCommit)
	}
	if c.CannedRandomSize < 8 {
		return fmt.Errorf("CannedRandomSize must be at least 8 (got %d)", c.CannedRandomSize)
	}
	if c.ExecutorQueueSize < 1 {
		return fmt.Errorf("ExecutorQueueSize must be at least 1 (got %d)", c.ExecutorQueueSize)
	}
	if c.SetupUpdateIntervalCount < 1 {
		return fmt.Errorf("SetupUpdateIntervalCount must be at least 1 (got %d)", c.SetupUpdateIntervalCount)
	}
	if c.MaxRuntimeSeconds < 0 {
		return fmt.Errorf("MaxRuntimeSeconds must be at least 0 (got %d)", c.MaxRuntimeSeconds)
	}
	if c.TransactionMetricsSampleRate < 0 || c.TransactionMetricsSampleRate > 1 {
		return fmt.Errorf("TransactionMetricsSampleRate must be in [0, 1] (got %f)", c.TransactionMetricsSampleRate)
	}
	if c.BackgroundMetricsScrapeInterval < 0 {
		return fmt.Errorf("BackgroundMetricsScrapeInterval must be non-negative (got %d)", c.BackgroundMetricsScrapeInterval)
	}
	if c.BlockChannelCapacity < 1 {
		return fmt.Errorf("BlockChannelCapacity must be at least 1 (got %d)", c.BlockChannelCapacity)
	}
	if c.RecieptChannelCapacity < 1 {
		return fmt.Errorf("RecieptChannelCapacity must be at least 1 (got %d)", c.RecieptChannelCapacity)
	}
	if c.MaxTPS < 0 {
		return fmt.Errorf("MaxTPS must be non-negative (got %f)", c.MaxTPS)
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LogLevel must be one of debug, info, warn, error (got %q)", c.LogLevel)
	}

	return nil
}

// LoadConfigFromFile parses a JSON config file at the given path.
// Returns defaults with file values overlaid. Fails if the file contains
// unrecognized configuration keys.
func LoadConfigFromFile(path string) (*CryptoSimConfig, error) {
	cfg := DefaultCryptoSimConfig()
	//nolint:gosec // G304 - path comes from config file, filepath.Clean used to mitigate traversal
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open config file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("failed to close config file: %v\n", err)
		}
	}()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}
