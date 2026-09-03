package evmonly

import (
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigastore "github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
)

type readOnlyTestStore struct {
	*MemoryStore
}

func (*readOnlyTestStore) CommitStateChanges(int64, []*proto.NamedChangeSet) error {
	return nil
}

// withTestState keeps executor unit tests focused on execution behavior while
// production code exposes only giga StateDB configuration.
func withTestState(state StateReader) Option {
	store := &readOnlyTestStore{MemoryStore: NewMemoryStore(state)}
	return withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet)
}

func withTestStores(store gigastore.StateDB, receiptStore receipt.ReceiptStore, encoder NamedChangeSetEncoder) Option {
	manager := bootstrap.NewGigaStorageManagerWithStores(nil, store, receiptStore)
	return WithStorageManager(manager, encoder)
}
