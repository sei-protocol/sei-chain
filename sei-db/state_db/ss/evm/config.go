package evm

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
)

// EVMStoreType represents different EVM data stores
type EVMStoreType string

const (
	StorageStore EVMStoreType = "storage"
	BalanceStore EVMStoreType = "balance"
	NonceStore   EVMStoreType = "nonce"
	CodeStore    EVMStoreType = "code"
)

// EVMStoreConfig is an alias to the main config
type EVMStoreConfig = config.EVMStateStoreConfig

// DefaultEVMStoreConfig returns default configuration
func DefaultEVMStoreConfig() EVMStoreConfig {
	return config.DefaultEVMStateStoreConfig()
}

// AllEVMStoreTypes returns all EVM store types
func AllEVMStoreTypes() []EVMStoreType {
	return []EVMStoreType{StorageStore, BalanceStore, NonceStore, CodeStore}
}
