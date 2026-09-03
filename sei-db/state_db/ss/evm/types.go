package evm

import (
	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// EVMStoreKey is the cosmos store key for EVM module.
const EVMStoreKey = commonevm.EVMStoreKey

// EVMStoreType identifies the type of EVM sub-database.
// Alias to EVMKeyKind from common/evm - use commonevm.ParseEVMKey for routing.
type EVMStoreType = commonevm.EVMKeyKind

// NumEVMStoreTypes is the number of active EVM store key namespaces.
// Used for pre-allocating maps. Types: Nonce, CodeHash, Code, Storage, Legacy, Balance.
const NumEVMStoreTypes = 6

// Re-export EVMKeyKind constants for convenience
const (
	StoreEmpty    = commonevm.EVMKeyEmpty
	StoreNonce    = commonevm.EVMKeyNonce
	StoreCodeHash = commonevm.EVMKeyCodeHash
	StoreCode     = commonevm.EVMKeyCode
	StoreStorage  = commonevm.EVMKeyStorage
	StoreMisc     = commonevm.EVMKeyMisc // Catch-all: codesize, address mappings, receipts, etc.
	StoreBalance  = commonevm.EVMKeyBalance
)

// AllEVMStoreTypes returns all EVM store types that have separate DBs.
func AllEVMStoreTypes() []EVMStoreType {
	return []EVMStoreType{
		StoreNonce,
		StoreCodeHash,
		StoreCode,
		StoreStorage,
		StoreMisc,
		StoreBalance,
	}
}

// StoreTypeName returns a human-readable name for the store type (used for DB directories)
func StoreTypeName(st EVMStoreType) string {
	switch st {
	case StoreNonce:
		return "nonce"
	case StoreCodeHash:
		return "codehash"
	case StoreCode:
		return "code"
	case StoreStorage:
		return "storage"
	case StoreMisc:
		return "legacy"
	case StoreBalance:
		return "balance"
	default:
		return "unknown"
	}
}
