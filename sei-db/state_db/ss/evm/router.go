package evm

import (
	"bytes"
)

// EVM key prefixes from x/evm/types/keys.go
var (
	StateKeyPrefix = []byte{0x03} // Storage
	CodeKeyPrefix  = []byte{0x07}
	NonceKeyPrefix = []byte{0x0a}
)

// EVMStoreKey is the cosmos store key for EVM module
const EVMStoreKey = "evm"

// BankStoreKey is the cosmos store key for bank module (for balances)
const BankStoreKey = "bank"

// KeyRouter determines which EVM database should handle a given key
type KeyRouter struct{}

// NewKeyRouter creates a new key router
func NewKeyRouter() *KeyRouter {
	return &KeyRouter{}
}

// RouteKey determines the EVM store type for a given store key and data key
// Returns (storeType, strippedKey, isEVM)
// strippedKey has the prefix removed for EVM stores
func (r *KeyRouter) RouteKey(storeKey string, key []byte) (EVMStoreType, []byte, bool) {
	if storeKey == EVMStoreKey && len(key) > 0 {
		switch {
		case bytes.HasPrefix(key, StateKeyPrefix):
			return StorageStore, key[len(StateKeyPrefix):], true
		case bytes.HasPrefix(key, CodeKeyPrefix):
			return CodeStore, key[len(CodeKeyPrefix):], true
		case bytes.HasPrefix(key, NonceKeyPrefix):
			return NonceStore, key[len(NonceKeyPrefix):], true
		}
	}

	// Balance routing: bank module balance keys
	// Bank balance keys are typically: prefix + address
	if storeKey == BankStoreKey && len(key) > 0 {
		// We can route specific bank prefixes to BalanceStore if needed
		// For now, leave bank in Cosmos_SS to avoid complexity
	}

	return "", nil, false
}

// IsEVMStore checks if a store key is routed to EVM stores
func (r *KeyRouter) IsEVMStore(storeKey string) bool {
	return storeKey == EVMStoreKey
}

// RestoreKey prepends the original prefix back to the key
func (r *KeyRouter) RestoreKey(storeType EVMStoreType, key []byte) []byte {
	var prefix []byte
	switch storeType {
	case StorageStore:
		prefix = StateKeyPrefix
	case CodeStore:
		prefix = CodeKeyPrefix
	case NonceStore:
		prefix = NonceKeyPrefix
	case BalanceStore:
		return key // No prefix for balance (different store)
	default:
		return key
	}
	return append(prefix, key...)
}
