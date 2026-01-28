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
// Used for pre-allocating maps. Types: Nonce, CodeHash, Code, Storage.
const NumEVMStoreTypes = 4

// Re-export EVMKeyKind constants for convenience
const (
	StoreUnknown  = commonevm.EVMKeyUnknown
	StoreNonce    = commonevm.EVMKeyNonce
	StoreCodeHash = commonevm.EVMKeyCodeHash
	StoreCode     = commonevm.EVMKeyCode
	StoreStorage  = commonevm.EVMKeyStorage
	// StoreBalance is reserved for future migration; balances currently use tendermint store
	StoreBalance EVMStoreType = 100
)

// AllEVMStoreTypes returns all EVM store types that have separate DBs.
// Note: CodeSize is not included as it's not part of standard EVM state.
// Note: Balance is not included until migration from tendermint store.
func AllEVMStoreTypes() []EVMStoreType {
	return []EVMStoreType{
		StoreNonce,
		StoreCodeHash,
		StoreCode,
		StoreStorage,
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
	case StoreBalance:
		return "balance"
	default:
		return "unknown"
	}
}
