package cryptosim

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

const (
	minPaddedAccountSize        = 8
	minErc20StorageSlotSize     = 32
	minErc20InteractionsPerAcct = 1
)

// Defines the configuration for the cryptosim benchmark.
type CryptoSimConfig struct {

	// The minimum number of accounts that should be in the DB prior to the start of the benchmark.
	// If there are fewer than this number of accounts, the benchmark will first create the necessary
	// accounts before starting its regular operations.
	MinimumNumberOfAccounts int

	// When selecting an account for a transaction, select a hot account with this probability. Should be
	// a value between 0.0 and 1.0.
	HotAccountProbability float64

	// When selecting a non-hot account for a transaction, the benchmark will create a new account with this
	// probability. Should be a value between 0.0 and 1.0.
	NewAccountProbability float64

	// The number of hot accounts.
	//
	// Future work: add different distributions of hot account access. Currently, distribution is flat.
	HotAccountSetSize int

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
}

// Validate checks that the configuration is sane and returns an error if not.
func (c *CryptoSimConfig) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("DataDir is required")
	}
	if c.PaddedAccountSize < minPaddedAccountSize {
		return fmt.Errorf("PaddedAccountSize must be at least %d (got %d)", minPaddedAccountSize, c.PaddedAccountSize)
	}
	if c.MinimumNumberOfAccounts < c.HotAccountSetSize+2 {
		return fmt.Errorf("MinimumNumberOfAccounts must be at least HotAccountSetSize+2 (%d)", c.HotAccountSetSize+2)
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
		return fmt.Errorf("Erc20StorageSlotSize must be at least %d (got %d)", minErc20StorageSlotSize, c.Erc20StorageSlotSize)
	}
	if c.Erc20InteractionsPerAccount < minErc20InteractionsPerAcct {
		return fmt.Errorf("Erc20InteractionsPerAccount must be at least %d (got %d)", minErc20InteractionsPerAcct, c.Erc20InteractionsPerAccount)
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
	return nil
}

// Returns the default configuration for the cryptosim benchmark.
func DefaultCryptoSimConfig() *CryptoSimConfig {
	return &CryptoSimConfig{
		MinimumNumberOfAccounts:           1_000_000,
		HotAccountProbability:             0.5,
		NewAccountProbability:             0.001,
		HotAccountSetSize:                 100,
		PaddedAccountSize:                 69, // Not a joke, this is the actual size
		MinimumNumberOfErc20Contracts:     10_000,
		HotErc20ContractProbability:       0.5,
		HotErc20ContractSetSize:           100,
		Erc20ContractSize:                 1024 * 2, // 2kb
		Erc20StorageSlotSize:              32,
		Erc20InteractionsPerAccount:       10,
		TransactionsPerBlock:              1024,
		BlocksPerCommit:                   32,
		Seed:                              1337,
		CannedRandomSize:                  1024 * 1024 * 1024, // 1GB
		Backend:                           wrappers.FlatKV,
		ConsoleUpdateIntervalSeconds:      1,
		ConsoleUpdateIntervalTransactions: 1_000_000,
		SetupUpdateIntervalCount:          100_000,
		ThreadsPerCore:                    1.0,
		ConstantThreadCount:               0,
		ExecutorQueueSize:                 64,
	}
}

// LoadConfigFromFile parses a JSON config file at the given path.
// Returns defaults with file values overlaid. Fails if the file contains
// unrecognized configuration keys.
func LoadConfigFromFile(path string) (*CryptoSimConfig, error) {
	cfg := DefaultCryptoSimConfig()
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config file: %w", err)
	}
	defer f.Close()

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
