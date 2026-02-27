package evm

import (
	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
)

// EVMStoreKey is the cosmos store key for EVM module
const EVMStoreKey = "evm"

// EVMStoreType identifies the type of EVM sub-database.
// Alias to EVMKeyKind from common/evm - use commonevm.ParseEVMKey for routing.
type EVMStoreType = commonevm.EVMKeyKind

// NumEVMStoreTypes is the number of active EVM store types with separate DBs.
// Used for pre-allocating maps. Types: Nonce, CodeHash, Code, Storage, Legacy.
const NumEVMStoreTypes = 5

// Re-export EVMKeyKind constants for convenience
const (
	StoreEmpty    = commonevm.EVMKeyEmpty
	StoreNonce    = commonevm.EVMKeyNonce
	StoreCodeHash = commonevm.EVMKeyCodeHash
	StoreCode     = commonevm.EVMKeyCode
	StoreStorage  = commonevm.EVMKeyStorage
	StoreLegacy   = commonevm.EVMKeyLegacy // Catch-all: codesize, address mappings, receipts, etc.
	// StoreBalance is reserved for future migration; balances currently use tendermint store
	StoreBalance EVMStoreType = 100
)

// AllEVMStoreTypes returns all EVM store types that have separate DBs.
// Note: Balance is not included until migration from tendermint store.
func AllEVMStoreTypes() []EVMStoreType {
	return []EVMStoreType{
		StoreNonce,
		StoreCodeHash,
		StoreCode,
		StoreStorage,
		StoreLegacy,
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
	case StoreLegacy:
		return "legacy"
	case StoreBalance:
		return "balance"
	default:
		return "unknown"
	}
}
