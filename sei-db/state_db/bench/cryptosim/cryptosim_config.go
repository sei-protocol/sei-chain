package cryptosim

// Defines the configuration for the cryptosim benchmark.
type CryptoSimConfig struct {

	// The minimum number of accounts that should be in the DB prior to the start of the benchmark.
	// If there are fewer than this number of accounts, the benchmark will first create the necessary
	// accounts before starting its regular operations.
	MinimumAccountNumber int

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

	// The directory to store the benchmark data. If unspecified, the benchmark will use a temporary directory
	// that is destroyed after the benchmark completes.
	DataDir string
}

// Returns the default configuration for the cryptosim benchmark.
func DefaultCryptoSimConfig() *CryptoSimConfig {
	return &CryptoSimConfig{
		MinimumAccountNumber: 1000,
		HotAccountWeight:     50,
		ColdAccountWeight:    49,
		NewAccountWeight:     1,
		HotSetSize:           100,
		AccountKeySize:       20,
		PaddedAccountSize:    100,
		TransactionsPerBlock: 100,
		DataDir:              "",
	}
}
