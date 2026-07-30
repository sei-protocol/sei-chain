package config

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	flatkvConfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

const DefaultRollbackWindow = 1000
const DefaultRetention = 1000000

type GigaStorageConfig struct {
	RollbackWindow  uint64
	BlockRetention  uint64
	HomePath        string
	FlatKVConfig    *flatkvConfig.Config
	SSConfig        StateStoreConfig
	ReceiptDBConfig ReceiptStoreConfig
	BlockDBConfig   *littblock.LittBlockConfig
}

// DefaultGigaStorageConfig returns a GigaStorageConfig whose store directories match the
// below layout:
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

	return GigaStorageConfig{
		RollbackWindow:  DefaultRollbackWindow,
		BlockRetention:  DefaultRetention,
		HomePath:        homePath,
		FlatKVConfig:    flatKV,
		SSConfig:        ssConfig,
		ReceiptDBConfig: receiptConfig,
		BlockDBConfig:   blockDBConfig,
	}
}
