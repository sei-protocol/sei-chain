package config

import (
	"fmt"

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
// RollbackWindow and RetentionBeyondRollbackWindow have a single source of truth in
// gc.DefaultStorageGarbageCollectorConfig.
type GigaStorageConfig struct {
	HomePath        string
	FlatKVConfig    *flatkvConfig.Config
	SSConfig        StateStoreConfig
	ReceiptDBConfig ReceiptStoreConfig
	BlockDBConfig   *littblock.LittBlockConfig
	PruningConfig   *gc.StorageGarbageCollectorConfig
}

// DefaultGigaStorageConfig returns a GigaStorageConfig whose store directories match the
// layout below, and whose pruning knobs are gc.DefaultStorageGarbageCollectorConfig():
//
//	data/state_commit/flatkv
//	data/state_store/evm/{backend}   (sole SS; no Cosmos SS in Giga)
//	data/ledger/receipt/{backend}
//	data/ledger/block
//
// Giga opens SS via evm.NewEVMStateStore(dir, ssConfig) directly — not through
// composite.NewCompositeStateStore — so the EVM path lives on EVMDBDirectory (the
// argument callers pass as dir). DBDirectory and EVMSplit are left at their defaults:
// they only matter for the composite path, which Giga does not use.
//
// Unlike the other Default*Config helpers in this package, this can fail: the block-store
// default wraps littdb.DefaultConfig, which validates the directory path.
func DefaultGigaStorageConfig(homePath string) (GigaStorageConfig, error) {
	blockDBConfig, err := littblock.DefaultConfig(utils.GetBlockStorePath(homePath))
	if err != nil {
		return GigaStorageConfig{}, fmt.Errorf("failed to build block db config: %w", err)
	}

	flatKV := flatkvConfig.DefaultConfig()
	flatKV.DataDir = utils.GetFlatKVPath(homePath)

	ssConfig := DefaultStateStoreConfig()
	ssConfig.EVMDBDirectory = utils.GetEVMStateStorePath(homePath, ssConfig.Backend)

	receiptConfig := DefaultReceiptStoreConfig()
	receiptConfig.DBDirectory = utils.GetReceiptStorePath(homePath, receiptConfig.Backend)

	return GigaStorageConfig{
		HomePath:        homePath,
		FlatKVConfig:    flatKV,
		SSConfig:        ssConfig,
		ReceiptDBConfig: receiptConfig,
		BlockDBConfig:   blockDBConfig,
		PruningConfig:   gc.DefaultStorageGarbageCollectorConfig(),
	}, nil
}
