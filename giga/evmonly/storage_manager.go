package evmonly

import (
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// WithStorageManager selects the stores used for state and receipt persistence.
// The encoder converts executor-native state changes into the state store's format.
func WithStorageManager(manager *bootstrap.GigaStorageManager, encoder NamedChangeSetEncoder) Option {
	return func(e *Executor) {
		if manager != nil {
			e.stateStore = manager.StateDB()
			e.receiptStore = manager.ReceiptDB()
		}
		e.changeSetEncoder = encoder
	}
}

// WithStore selects a state store independently of a storage manager.
func WithStore(store gigatypes.StateDB, encoder NamedChangeSetEncoder) Option {
	return func(e *Executor) {
		e.stateStore = store
		e.changeSetEncoder = encoder
	}
}

// WithReceiptStore selects a receipt store independently of a storage manager.
func WithReceiptStore(store receipt.ReceiptStore) Option {
	return func(e *Executor) {
		e.receiptStore = store
	}
}
