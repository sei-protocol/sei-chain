package config

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	flatkvConfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// GigaStorageConfig composes the store configs a Giga node opens. It is not read from
// app.toml; callers build it with DefaultGigaStorageConfig.
type GigaStorageConfig struct {
	HomePath         string
	FlatKVConfig     *flatkvConfig.Config           // required
	SSConfig         StateStoreConfig               // optional via Enable
	ReceiptDBConfig  ReceiptStoreConfig             // optional via Enable
	BlockDBConfig    *littblock.BlockDBConfig       // required
	PruningConfig    *StorageGarbageCollectorConfig // required
	CheckpointConfig CheckpointConfig
	// BlockRetention is the minimum number of blocks behind its head the block store keeps,
	// whatever PruningConfig would prune. 0 leaves the block store to PruningConfig alone.
	BlockRetention uint64
}

// DefaultAutobahnBlockRetention is 12 hours of blocks at 100 blocks per second.
const DefaultAutobahnBlockRetention uint64 = 12 * 60 * 60 * 100

// gigaReceiptBackend is the receipt backend Giga opens (littidx).
const gigaReceiptBackend = "littidx"

// DefaultGigaStorageConfig returns a config rooted at homePath:
//
//	data/state_commit/flatkv
//	data/state_store/evm/{backend}
//	data/ledger/receipt/{backend}
//	data/ledger/block
//
// SS is opened at EVMDBDirectory. Every store sets ExternalPruning so PruningConfig owns retention.
func DefaultGigaStorageConfig(homePath string) (*GigaStorageConfig, error) {
	blockDBConfig, err := littblock.DefaultConfig(utils.GetBlockStorePath(homePath))
	if err != nil {
		return nil, fmt.Errorf("failed to build block db config: %w", err)
	}

	flatKV := flatkvConfig.DefaultConfig()
	flatKV.DataDir = utils.GetFlatKVPath(homePath)
	flatKV.ExternalPruning = true

	ssConfig := DefaultStateStoreConfig()
	ssConfig.EVMDBDirectory = utils.GetEVMStateStorePath(homePath, ssConfig.Backend)
	ssConfig.ExternalPruning = true
	ssConfig.DisableInternalWAL = true

	receiptConfig := DefaultReceiptStoreConfig()
	receiptConfig.Backend = gigaReceiptBackend
	receiptConfig.DBDirectory = utils.GetReceiptStorePath(homePath, receiptConfig.Backend)
	receiptConfig.ExternalPruning = true

	return &GigaStorageConfig{
		HomePath:         homePath,
		FlatKVConfig:     flatKV,
		SSConfig:         ssConfig,
		ReceiptDBConfig:  receiptConfig,
		BlockDBConfig:    blockDBConfig,
		PruningConfig:    DefaultStorageGarbageCollectorConfig(),
		CheckpointConfig: DefaultCheckpointConfig(),
	}, nil
}

func (c *GigaStorageConfig) WithValidatorMode() *GigaStorageConfig {
	c.ReceiptDBConfig.Enable = false
	c.SSConfig.Enable = false
	return c
}

func (c *GigaStorageConfig) WithFullNodeMode() *GigaStorageConfig {
	c.ReceiptDBConfig.Enable = true
	c.SSConfig.Enable = true
	return c
}

// AutobahnStorageConfig is the disk-backed Giga layout Autobahn opens: FlatKV,
// receipts, and BlockDB. SS stays off, and BlockDB keeps DefaultAutobahnBlockRetention blocks.
func AutobahnStorageConfig(homePath string) (*GigaStorageConfig, error) {
	storageConfig, err := DefaultGigaStorageConfig(homePath)
	if err != nil {
		return nil, err
	}
	storageConfig.WithValidatorMode()
	storageConfig.ReceiptDBConfig.Enable = true
	storageConfig.BlockRetention = DefaultAutobahnBlockRetention
	return storageConfig, nil
}

func (c *GigaStorageConfig) WithAccountDBCacheSize(sizeInBytes uint64) *GigaStorageConfig {
	c.FlatKVConfig.AccountStoreConfig.MaxSize = sizeInBytes
	return c
}

func (c *GigaStorageConfig) WithStorageDBCacheSize(sizeInBytes uint64) *GigaStorageConfig {
	c.FlatKVConfig.StorageStoreConfig.MaxSize = sizeInBytes
	return c
}

func (c *GigaStorageConfig) WithCodeDBCacheSize(sizeInBytes uint64) *GigaStorageConfig {
	c.FlatKVConfig.CodeStoreConfig.MaxSize = sizeInBytes
	return c
}

// Validate checks fields no store checks for itself. Stores still validate as they open;
// this fails first so a bad config does not leave databases half-open.
func (c *GigaStorageConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("giga storage config is required")
	}
	if c.FlatKVConfig == nil {
		return fmt.Errorf("flatkv config is required")
	}
	// FlatKVConfig.Validate runs after the store fills nested dirs from DataDir, so a
	// correct-as-written config fails it. Only DataDir is checked here.
	if c.FlatKVConfig.DataDir == "" {
		return fmt.Errorf("flatkv data dir is required")
	}

	if c.BlockDBConfig == nil {
		return fmt.Errorf("block db config is required")
	}
	if err := c.BlockDBConfig.Validate(); err != nil {
		return fmt.Errorf("block db config is invalid: %w", err)
	}

	if c.SSConfig.Enable && c.SSConfig.EVMDBDirectory == "" {
		return fmt.Errorf("state store EVM db directory is required")
	}

	if c.PruningConfig == nil {
		return fmt.Errorf("pruning config is required")
	}
	if err := c.PruningConfig.Validate(); err != nil {
		return fmt.Errorf("pruning config is invalid: %w", err)
	}
	if !c.FlatKVConfig.ExternalPruning {
		return fmt.Errorf("flatkv ExternalPruning must be true")
	}
	if c.SSConfig.Enable && !c.SSConfig.ExternalPruning {
		return fmt.Errorf("state store ExternalPruning must be true")
	}
	if c.ReceiptDBConfig.Enable && !c.ReceiptDBConfig.ExternalPruning {
		return fmt.Errorf("receipt store ExternalPruning must be true")
	}

	if !c.CheckpointConfig.Enabled() {
		return fmt.Errorf("checkpoint config must set a time interval, a block interval, or both; " +
			"with neither the node takes no snapshots and its state WAL grows without bound")
	}
	return nil
}
