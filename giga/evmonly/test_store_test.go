package evmonly

import (
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

type readOnlyTestStore struct {
	*MemoryStore
}

func (*readOnlyTestStore) CommitStateChanges(int64, *gigatypes.StateChangeSet) error {
	return nil
}

// withTestState keeps executor unit tests focused on execution behavior while
// supplying the test-only in-memory stores.
func withTestState(state StateReader) Option {
	store := &readOnlyTestStore{MemoryStore: NewMemoryStore(state)}
	return withTestStores(store, NewMemoryReceiptStore())
}

func withTestStores(store gigatypes.StateDB, receiptStore receiptdb.ReceiptStore) Option {
	return func(executor *Executor) {
		WithStore(store)(executor)
		WithReceiptStore(receiptStore)(executor)
	}
}
