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

	// When selecting an account for a transaction, the benchmark will select a hot account with this
	// weight. The actual probability depends on the ratio of this weight to the other account weight configurations.
	HotAccountWeight float64

	// When selecting an account for a transaction, the benchmark will select a cold account with this
	// weight. The actual probability depends on the ratio of this weight to the other account weight configurations.
	ColdAccountWeight float64

	// When selecting an account for a transaction, the benchmark will create a new account with this
	// weight. The actual probability depends on the ratio of this weight to the other account weight configurations.
	NewAccountWeight float64

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

	// The directory to store the benchmark data.
	DataDir string

	// The seed to use for the random number generator. Altering this seed for a pre-existing DB will result
	// in undefined behavior, don't change the seed unless you are starting a new run from scratch.
	Seed int64

	// The size of the buffer for random data. Similar to the seed, altering this size for a pre-existing DB will result
	// in undefined behavior, don't change the size unless you are starting a new run from scratch.
	RandomBufferSize int

	// The backend to use for the benchmark database.
	Backend wrappers.DBType

	// This field is ignored, but allows for a comment to be added to the config file.
	// Something, something, why in the name of all things holy doesn't json support comments?
	Comment string
}

// Returns the default configuration for the cryptosim benchmark.
func DefaultCryptoSimConfig() *CryptoSimConfig {
	return &CryptoSimConfig{
		MinimumNumberOfAccounts: 1000,
		HotAccountWeight:        50,
		ColdAccountWeight:       49,
		NewAccountWeight:        1,
		HotSetSize:              100,
		AccountKeySize:          20,
		PaddedAccountSize:       100,
		TransactionsPerBlock:    100,
		DataDir:                 "",
		Seed:                    1337,
		RandomBufferSize:        1024 * 1024 * 1024, // 1GB
		Backend:                 wrappers.FlatKV,
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
