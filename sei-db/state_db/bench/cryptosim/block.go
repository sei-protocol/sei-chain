package cryptosim

import "iter"

// A simulated block of transactions.
type block struct {
	config *CryptoSimConfig

	// The transactions in the block.
	transactions []*transaction

	// The block number. This is not currently preserved across benchmark restarts, but otherwise monotonically
	// increases as you'd expect.
	blockNumber int64

	// The next account ID to be used when creating a new account, as of the end of this block.
	nextAccountID int64

	// The number of cold accounts, as of the end of this block.
	numberOfColdAccounts int64

	// The next ERC20 contract ID to be used when creating a new ERC20 contract, as of the end of this block.
	nextErc20ContractID int64

	metrics *CryptosimMetrics
}

// Creates a new block with the given capacity.
func NewBlock(
	config *CryptoSimConfig,
	metrics *CryptosimMetrics,
	blockNumber int64,
	capacity int,
) *block {
	return &block{
		config:       config,
		blockNumber:  blockNumber,
		transactions: make([]*transaction, 0, capacity),
		metrics:      metrics,
	}
}

// Returns an iterator over the transactions in the block.
func (b *block) Iterator() iter.Seq[*transaction] {
	return func(yield func(*transaction) bool) {
		for _, txn := range b.transactions {
			if !yield(txn) {
				return
			}
		}
	}
}

// Adds a transaction to the block.
func (b *block) AddTransaction(txn *transaction) {
	b.transactions = append(b.transactions, txn)
}

// Returns the block number.
func (b *block) BlockNumber() int64 {
	return b.blockNumber
}

// Sets information about account state as of the end of this block.
func (b *block) SetBlockAccountStats(
	nextAccountID int64,
	numberOfColdAccounts int64,
	nextErc20ContractID int64,
) {
	b.nextAccountID = nextAccountID
	b.numberOfColdAccounts = numberOfColdAccounts
	b.nextErc20ContractID = nextErc20ContractID
}

// This method should be called after a block is finished executing and finalized.
// Reports metrics about the block.
func (b *block) ReportBlockMetrcs() {
	b.metrics.SetTotalNumberOfAccounts(b.nextAccountID, int64(b.config.NumberOfHotAccounts), b.numberOfColdAccounts)
}

// Returns the next account ID to be used when creating a new account, as of the end of this block.
func (b *block) NextAccountID() int64 {
	return b.nextAccountID
}

// Returns the next ERC20 contract ID to be used when creating a new ERC20 contract, as of the end of this block.
func (b *block) NextErc20ContractID() int64 {
	return b.nextErc20ContractID
}

// Returns the number of transactions in the block.
func (b *block) TransactionCount() int64 {
	return int64(len(b.transactions))
}
