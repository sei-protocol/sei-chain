package cryptosim

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

// Defines the configuration for the cryptosim benchmark.
type CryptoSimConfig struct {

	// The minimum number of accounts that should be in the DB prior to the start of the benchmark.
	// If there are fewer than this number of accounts, the benchmark will first create the necessary
	// accounts before starting its regular operations.
	MinimumNumberOfAccounts int

	// When selecting an account for a transaction, select a hot account with this probability. Should be
	// a value between 0.0 and 1.0.
	HotAccountProbably float64

	// When selecting a non-hot account for a transaction, the benchmark will create a new account with this
	// probability. Should be a value between 0.0 and 1.0.
	NewAccountProbably float64

	// The number of hot accounts.
	//
	// Future work: add different distributions of hot account access. Currently, distribution is flat.
	HotSetSize int

	// The number of bytes in the account key.
	AccountKeySize int

	// Each account contains an integer value used to track a balance, plus a bunch of random
	// bytes for padding. This is the total size of the account after padding is added.
	PaddedAccountSize int

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
}

// Returns the default configuration for the cryptosim benchmark.
func DefaultCryptoSimConfig() *CryptoSimConfig {
	return &CryptoSimConfig{
		MinimumNumberOfAccounts:           1_000_000,
		HotAccountProbably:                0.5,
		NewAccountProbably:                0.001,
		HotSetSize:                        100,
		AccountKeySize:                    20,
		PaddedAccountSize:                 69, // Not a joke, this is the actual size
		TransactionsPerBlock:              1024,
		BlocksPerCommit:                   32,
		DataDir:                           "",
		Seed:                              1337,
		CannedRandomSize:                  1024 * 1024 * 1024, // 1GB
		Backend:                           wrappers.FlatKV,
		ConsoleUpdateIntervalSeconds:      1,
		ConsoleUpdateIntervalTransactions: 1_000_000,
		SetupUpdateIntervalCount:          100_000,
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
	return cfg, nil
}
