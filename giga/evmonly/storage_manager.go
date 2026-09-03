package evmonly

import (
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	gigastore "github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
)

// StorageManager provides the state and receipt stores used by an executor.
type StorageManager interface {
	StateDB() gigastore.StateDB
	ReceiptDB() receipt.ReceiptStore
}

var _ StorageManager = (*bootstrap.GigaStorageManager)(nil)

// WithStorageManager selects the stores used for state and receipt persistence.
// The encoder converts executor-native state changes into the state store's format.
func WithStorageManager(manager StorageManager, encoder NamedChangeSetEncoder) Option {
	return func(e *Executor) {
		e.storageManager = manager
		e.changeSetEncoder = encoder
	}
}

var _ StorageManager = (*MemoryStorageManager)(nil)

// MemoryStorageManager owns in-memory state and receipt stores.
type MemoryStorageManager struct {
	stateDB   *MemoryStore
	receiptDB *MemoryReceiptStore
}

// NewMemoryStorageManager constructs stores backed by source and process memory.
func NewMemoryStorageManager(source StateReader) *MemoryStorageManager {
	return &MemoryStorageManager{
		stateDB:   NewMemoryStore(source),
		receiptDB: NewMemoryReceiptStore(),
	}
}

// StateDB returns the manager's state store.
func (m *MemoryStorageManager) StateDB() gigastore.StateDB {
	return m.stateDB
}

// ReceiptDB returns the manager's receipt store.
func (m *MemoryStorageManager) ReceiptDB() receipt.ReceiptStore {
	return m.receiptDB
}

// StateStore returns the concrete in-memory state store.
func (m *MemoryStorageManager) StateStore() *MemoryStore {
	return m.stateDB
}

// ReceiptStore returns the concrete in-memory receipt store.
func (m *MemoryStorageManager) ReceiptStore() *MemoryReceiptStore {
	return m.receiptDB
}
