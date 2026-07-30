package config

import (
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	flatkvConfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

const DefaultRollbackWindow = 1000
const DefaultRetention = 1000000

type GigaStorageConfig struct {
	RollbackWindow  uint64
	BlockRetention  uint64
	DataDirectory   string
	FlatKVConfig    *flatkvConfig.Config
	SSConfig        StateStoreConfig
	ReceiptDBConfig ReceiptStoreConfig
	BlockDBConfig   *littblock.LittBlockConfig
}

func DefaultGigaStorageConfig(dbDir string) GigaStorageConfig {
	blockDBConfig, err := littblock.DefaultConfig(dbDir)
	if err != nil {
		panic(err)
	}
	return GigaStorageConfig{
		RollbackWindow:  DefaultRollbackWindow,
		BlockRetention:  DefaultRetention,
		DataDirectory:   dbDir,
		FlatKVConfig:    flatkvConfig.DefaultConfig(),
		SSConfig:        DefaultStateStoreConfig(),
		ReceiptDBConfig: DefaultReceiptStoreConfig(),
		BlockDBConfig:   blockDBConfig,
	}
}
