package config

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/controller"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	flatkvConfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// GigaStorageConfig is an in-process composition of store configs for Giga storage.
// It is intentionally not read from app.toml (no mapstructure tags, no TOML section):
// callers build it via DefaultGigaStorageConfig. Nested knobs live on the store and
// collector configs they already own (StateStoreConfig, ReceiptStoreConfig,
// gc.StorageGarbageCollectorConfig) rather than being redeclared here — in particular
// RollbackWindow and LookbackWindow have a single source of truth in
// gc.DefaultStorageGarbageCollectorConfig, and cover every managed store at once.
type GigaStorageConfig struct {
	HomePath        string
	FlatKVConfig    *flatkvConfig.Config
	SSConfig        StateStoreConfig
	ReceiptDBConfig ReceiptStoreConfig
	BlockDBConfig   *littblock.BlockDBConfig
	PruningConfig   *controller.StorageGarbageCollectorConfig
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
// Unlike the other Default*Config helpers in this package, this returns an error: the
// block-store default wraps littdb.DefaultConfig, whose signature is fallible. It only rejects
// an empty path list, so the error cannot fire from here — the return is kept so this helper
// does not have to change if littdb starts validating the path itself.
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
		PruningConfig:   controller.DefaultStorageGarbageCollectorConfig(),
	}, nil
}
