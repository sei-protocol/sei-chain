package evmonly

import (
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// WithStorageManager selects the stores used for state and receipt persistence, the receipt hasher that
// receives each block's receipts, and the archive that receives each block.
func WithStorageManager(manager *bootstrap.GigaStorageManager) Option {
	return func(e *Executor) {
		if manager != nil {
			e.stateStore = manager.StateDB()
			e.receiptStore = manager.ReceiptDB()
			e.receiptHasher = manager.ReceiptHasher()
			e.archive = manager.Archive()
		}
	}
}

// WithStore selects a state store independently of a storage manager.
func WithStore(store gigatypes.StateDB) Option {
	return func(e *Executor) {
		e.stateStore = store
	}
}

// WithReceiptStore selects a receipt store independently of a storage manager.
func WithReceiptStore(store receiptdb.ReceiptStore) Option {
	return func(e *Executor) {
		e.receiptStore = store
	}
}
