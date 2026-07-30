package config

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	flatkvConfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// GigaStorageConfig is an in-process composition of store configs for Giga storage.
// It is intentionally not read from app.toml (no mapstructure tags, no TOML section):
// callers build it via DefaultGigaStorageConfig. Nested knobs live on the store and
// collector configs they already own (StateStoreConfig, ReceiptStoreConfig,
// gc.StorageGarbageCollectorConfig) rather than being redeclared here — in particular
// RollbackWindow and StoreRetention have a single source of truth in
// gc.DefaultStorageGarbageCollectorConfig.
type GigaStorageConfig struct {
	HomePath        string
	FlatKVConfig    *flatkvConfig.Config
	SSConfig        StateStoreConfig
	ReceiptDBConfig ReceiptStoreConfig
	BlockDBConfig   *littblock.LittBlockConfig
	// PruningConfig is the garbage-collector config shared by every store under this
	// Giga layout. Defaults come from gc.DefaultStorageGarbageCollectorConfig; do not
	// reintroduce parallel RollbackWindow / StoreRetention fields on this struct.
	PruningConfig *gc.StorageGarbageCollectorConfig
}

// DefaultGigaStorageConfig returns a GigaStorageConfig whose store directories match the
// layout below, and whose pruning knobs are gc.DefaultStorageGarbageCollectorConfig():
//
//	data/state_commit/flatkv
//	data/state_store/evm/{backend}
//	data/ledger/receipt/{backend}
//	data/ledger/block
func DefaultGigaStorageConfig(homePath string) GigaStorageConfig {
	blockDBConfig, err := littblock.DefaultConfig(utils.GetBlockStorePath(homePath))
	if err != nil {
		panic(err)
	}

	flatKV := flatkvConfig.DefaultConfig()
	flatKV.DataDir = utils.GetFlatKVPath(homePath)

	ssConfig := DefaultStateStoreConfig()
	ssConfig.DBDirectory = utils.GetEVMStateStorePath(homePath, ssConfig.Backend)

	receiptConfig := DefaultReceiptStoreConfig()
	receiptConfig.DBDirectory = utils.GetReceiptStorePath(homePath, receiptConfig.Backend)

	pruningConfig := gc.DefaultStorageGarbageCollectorConfig()

	return GigaStorageConfig{
		HomePath:        homePath,
		FlatKVConfig:    flatKV,
		SSConfig:        ssConfig,
		ReceiptDBConfig: receiptConfig,
		BlockDBConfig:   blockDBConfig,
		PruningConfig:   pruningConfig,
	}
}
