package cryptosim

import (
	"iter"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// A simulated block of transactions.
type block struct {
	config *CryptoSimConfig

	// The transactions in the block.
	transactions []*transaction

	// If receipt generation is enabled, this will contain the receipts for each transaction in the block.
	reciepts []*evmtypes.Receipt

	// The block number. This is not currently preserved across benchmark restarts, but otherwise monotonically
	// increases as you'd expect.
	blockNumber int64

	// The next account ID to be used when creating a new account, as of the end of this block.
	nextAccountID int64

	// The number of cold accounts, as of the end of this block.
	numberOfColdAccounts int64

	// The next ERC20 contract ID to be used when creating a new ERC20 contract, as of the end of this block.
	nextErc20ContractID int64

	// The writes this block makes, in the form the DB accepts, so finalizing has nothing left to
	// convert. Built by the block builder before the block is published and not modified after.
	//
	// Only the DB reads this. Executor reads go to the DB, never here — see Database.Get.
	changeset []*proto.KVPair

	metrics *CryptosimMetrics
}

// Creates a new block with the given capacity.
func NewBlock(
	config *CryptoSimConfig,
	metrics *CryptosimMetrics,
	blockNumber int64,
	capacity int,
) *block {

	var reciepts []*evmtypes.Receipt
	if config.GenerateReceipts {
		reciepts = make([]*evmtypes.Receipt, 0, capacity)
	}

	return &block{
		config:       config,
		blockNumber:  blockNumber,
		transactions: make([]*transaction, 0, capacity),
		metrics:      metrics,
		reciepts:     reciepts,
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

// Transactions returns the block's transactions. The caller must not modify the slice or its contents:
// once the block has been dispatched the executors read it concurrently.
func (b *block) Transactions() []*transaction {
	return b.transactions
}

// Adds a transaction to the block.
func (b *block) AddTransaction(txn *transaction) {
	b.transactions = append(b.transactions, txn)
}

// Adds a receipt to the block.
func (b *block) AddReceipt(receipt *evmtypes.Receipt) {
	b.reciepts = append(b.reciepts, receipt)
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
func (b *block) ReportBlockMetrics() {
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

// SetWrites records the writes this block makes, collapsing the builder's keyed map into the slice
// the DB takes. Called by the block builder before the block is published, after which the changeset
// must not be modified.
func (b *block) SetWrites(writes map[string]*proto.KVPair) {
	// Room for the three counter keys FinalizeBlock appends, so appending them does not have to copy
	// the whole slice on the thread this design exists to keep idle.
	b.changeset = make([]*proto.KVPair, 0, len(writes)+3)
	for _, pair := range writes {
		b.changeset = append(b.changeset, pair)
	}
}

// Changeset returns the block's writes in the form the DB accepts, excluding the counter keys that
// FinalizeBlock appends.
func (b *block) Changeset() []*proto.KVPair {
	return b.changeset
}
