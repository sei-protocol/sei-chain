package evm

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
)

// EVMStoreConfig defines configuration for the EVM state stores.
type EVMStoreConfig = config.EVMStateStoreConfig

// DefaultEVMStoreConfig returns the default EVM store configuration.
func DefaultEVMStoreConfig() EVMStoreConfig {
	return config.DefaultEVMStateStoreConfig()
}
