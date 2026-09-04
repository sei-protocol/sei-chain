package evmonly

import (
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

type readOnlyTestStore struct {
	*MemoryStore
}

func (*readOnlyTestStore) CommitStateChanges(int64, []*proto.NamedChangeSet) error {
	return nil
}

// withTestState keeps executor unit tests focused on execution behavior while
// exercising manager-owned stores.
func withTestState(state StateReader) Option {
	store := &readOnlyTestStore{MemoryStore: NewMemoryStore(state)}
	return withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet)
}

func withTestStores(store gigatypes.StateDB, receiptStore receipt.ReceiptStore, encoder NamedChangeSetEncoder) Option {
	manager := bootstrap.NewGigaStorageManagerWithStores(nil, store, receiptStore)
	return WithStorageManager(manager, encoder)
}
