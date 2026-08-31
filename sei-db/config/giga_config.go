package config

import (
	"fmt"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	flatkvConfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// GigaStorageConfig is an in-process composition of store configs for Giga storage.
// It is intentionally not read from app.toml (no mapstructure tags, no TOML section):
// callers build it via DefaultGigaStorageConfig. Nested knobs live on the store and
// collector configs they already own (StateStoreConfig, ReceiptStoreConfig,
// StorageGarbageCollectorConfig) rather than being redeclared here — in particular
// RollbackWindow and LookbackWindow have a single source of truth in
// DefaultStorageGarbageCollectorConfig, and cover every managed store at once.
//
// Each field is shaped as the config it carries is both produced and consumed: flatkv and littblock
// hand out pointers and take pointers, while the state store, the receipt store and the checkpoint
// scheduler use values throughout. The mix is not a choice this struct makes, so what a nil means
// differs per field and is spelled out below.
type GigaStorageConfig struct {
	HomePath string

	// FlatKVConfig is required. It is a pointer because the commit store takes one, not because nil
	// carries a meaning: Validate rejects it.
	FlatKVConfig *flatkvConfig.Config

	// SSConfig and ReceiptDBConfig each carry an Enable of their own, which is how a node runs without
	// that store. Both are read either way, so the rest of their fields still have to be valid.
	SSConfig        StateStoreConfig
	ReceiptDBConfig ReceiptStoreConfig

	// BlockDBConfig is required, and a pointer for the same reason FlatKVConfig is.
	BlockDBConfig *littblock.BlockDBConfig

	// PruningConfig is the one pointer here whose nil is a setting rather than a mistake: with no
	// collector configured, every store keeps its own retention.
	PruningConfig *StorageGarbageCollectorConfig

	CheckpointConfig CheckpointConfig
}

// gigaReceiptBackend is the receipt store backend Giga runs. It is the only one that participates in
// the shared prune cycle, which every Giga store does.
//
// Spelled here rather than imported from the receipt store, which reads this package: the dependency
// cannot run the other way.
const gigaReceiptBackend = "littidx"

// DefaultGigaStorageConfig returns a GigaStorageConfig whose store directories match the
// layout below, and whose pruning knobs are DefaultStorageGarbageCollectorConfig():
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
// Every store that keeps retention of its own hands it to the StorageGarbageCollector, so one cut
// line covers the whole node. PruningConfig and ExternalPruning move together: a config that sets
// the second without the first stands each store's own pruner down with nothing in its place, which
// Validate refuses.
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
	flatKV.ExternalPruning = true

	ssConfig := DefaultStateStoreConfig()
	ssConfig.EVMDBDirectory = utils.GetEVMStateStorePath(homePath, ssConfig.Backend)
	ssConfig.ExternalPruning = true

	receiptConfig := DefaultReceiptStoreConfig()
	receiptConfig.Backend = gigaReceiptBackend
	receiptConfig.DBDirectory = utils.GetReceiptStorePath(homePath, receiptConfig.Backend)
	receiptConfig.ExternalPruning = true

	return GigaStorageConfig{
		HomePath:         homePath,
		FlatKVConfig:     flatKV,
		SSConfig:         ssConfig,
		ReceiptDBConfig:  receiptConfig,
		BlockDBConfig:    blockDBConfig,
		PruningConfig:    DefaultStorageGarbageCollectorConfig(),
		CheckpointConfig: DefaultCheckpointConfig(),
	}, nil
}

// Validate reports whether this config describes a node that can run: it checks the fields no store
// checks for itself, and delegates to the store configs that validate as written.
//
// Every store also validates itself as it opens, but they open in sequence, so without this a config
// the last of them rejects is only discovered once the others hold the home directory.
func (c GigaStorageConfig) Validate() error {
	if c.FlatKVConfig == nil {
		return fmt.Errorf("flatkv config is required")
	}
	// FlatKVConfig.Validate is deliberately not called here. It is a post-resolution check: the commit
	// store fills its four nested database directories in from DataDir before validating, so a config
	// that is correct as written fails it. DataDir is the field the caller owns, and the state WAL is
	// opened from it before any store validates anything.
	if c.FlatKVConfig.DataDir == "" {
		return fmt.Errorf("flatkv data dir is required")
	}

	if c.BlockDBConfig == nil {
		return fmt.Errorf("block db config is required")
	}
	if err := c.BlockDBConfig.Validate(); err != nil {
		return fmt.Errorf("block db config is invalid: %w", err)
	}

	// Giga opens SS by this path directly rather than through the composite store, so it is the only
	// field naming where SS lives, and nothing downstream rejects an empty one: it opens a database
	// under the working directory instead. A node with SS disabled opens no database, so the path it
	// would have used is not its concern.
	if c.SSConfig.Enable && c.SSConfig.EVMDBDirectory == "" {
		return fmt.Errorf("state store EVM db directory is required")
	}

	if c.PruningConfig != nil {
		if err := c.PruningConfig.Validate(); err != nil {
			return fmt.Errorf("pruning config is invalid: %w", err)
		}
	}
	if err := c.requireCollectorForExternalPruning(); err != nil {
		return err
	}

	// The schedule replaces each store's own snapshot interval, so one that picks no height at all
	// leaves the node taking no snapshots: an unbounded state WAL, and a restart that replays the
	// whole history. A store's interval is not a fallback here, it is superseded.
	if !c.CheckpointConfig.Enabled() {
		return fmt.Errorf("checkpoint config must set a time interval, a block interval, or both; " +
			"with neither the node takes no snapshots and its state WAL grows without bound")
	}
	return nil
}

// requireCollectorForExternalPruning rejects a config that hands a store's retention to a collector
// that will not be started.
//
// ExternalPruning stands a store's own pruner down. With no collector to take it over nothing prunes
// that store at all, and the growth that follows is silent until the disk fills, so the combination is
// refused rather than discovered later.
func (c GigaStorageConfig) requireCollectorForExternalPruning() error {
	if c.PruningConfig != nil {
		return nil
	}
	var stranded []string
	if c.FlatKVConfig.ExternalPruning {
		stranded = append(stranded, "state commit")
	}
	if c.SSConfig.Enable && c.SSConfig.ExternalPruning {
		stranded = append(stranded, "EVM state store")
	}
	if c.ReceiptDBConfig.Enable && c.ReceiptDBConfig.ExternalPruning {
		stranded = append(stranded, "receipt store")
	}
	if len(stranded) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%s hand retention to the storage garbage collector but no pruning config was given, "+
			"which stands their own pruners down with nothing in their place; set a pruning config or clear ExternalPruning",
		strings.Join(stranded, ", "))
}
