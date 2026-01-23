package evm

import (
	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
)

// EVMStoreKey is the cosmos store key for EVM module
const EVMStoreKey = "evm"

// EVMStoreType identifies the type of EVM sub-database.
// Alias to EVMKeyKind from common/evm - use commonevm.ParseEVMKey for routing.
type EVMStoreType = commonevm.EVMKeyKind

// Re-export EVMKeyKind constants for convenience
const (
	StoreUnknown  = commonevm.EVMKeyUnknown
	StoreNonce    = commonevm.EVMKeyNonce
	StoreCodeHash = commonevm.EVMKeyCodeHash
	StoreCode     = commonevm.EVMKeyCode
	StoreCodeSize = commonevm.EVMKeyCodeSize
	StoreStorage  = commonevm.EVMKeyStorage
)

// AllEVMStoreTypes returns all EVM store types that have separate DBs
func AllEVMStoreTypes() []EVMStoreType {
	return []EVMStoreType{
		StoreNonce,
		StoreCodeHash,
		StoreCode,
		StoreCodeSize,
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
	case StoreCodeSize:
		return "codesize"
	case StoreStorage:
		return "storage"
	default:
		return "unknown"
	}
}
