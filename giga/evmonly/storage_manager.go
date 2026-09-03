package evmonly

import (
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
)

// WithStorageManager selects the stores used for state and receipt persistence.
// The encoder converts executor-native state changes into the state store's format.
func WithStorageManager(manager *bootstrap.GigaStorageManager, encoder NamedChangeSetEncoder) Option {
	return func(e *Executor) {
		e.storageManager = manager
		e.changeSetEncoder = encoder
	}
}
