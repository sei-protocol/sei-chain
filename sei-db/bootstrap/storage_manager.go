// Package bootstrap opens and owns the set of databases a Giga node runs on.
package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "giga")

// GigaStorageManager owns every database a Giga node reads and writes, plus the
// checkpoint schedule and prune cycle that run above them.
type GigaStorageManager struct {
	cfg *config.GigaStorageConfig

	// blockStore.Close closes the block database it was built over.
	blockStore *blockstore.Store

	// receiptDB is nil when ReceiptDBConfig.Enable is false.
	receiptDB receipt.ReceiptStore

	// stateDB owns the state commit store, the EVM state store and the state WAL they share, along
	// with the checkpoint schedule the two halves run on.
	stateDB *giga.StateDB
	// stateStore is stateDB for configured storage and may be another implementation for injected stores.
	stateStore gigatypes.StateDB

	// gc is nil until startGarbageCollector succeeds.
	gc *controller.StorageGarbageCollector
}

// NewGigaStorageManagerWithStores returns a manager that owns the supplied stores.
func NewGigaStorageManagerWithStores(
	blockStore *blockstore.Store,
	stateStore gigatypes.StateDB,
	receiptDB receipt.ReceiptStore,
) *GigaStorageManager {
	return &GigaStorageManager{
		blockStore: blockStore,
		stateStore: stateStore,
		receiptDB:  receiptDB,
	}
}

// NewGigaStorageManager runs the steps that bring storage up:
//  1. Perform a config validation.
//  2. Construct and open all DBs with the config.
//  3. OpenDBWithRecovery: bring every store onto one height.
//  4. Start the garbage collector.
func NewGigaStorageManager(ctx context.Context, cfg *config.GigaStorageConfig) (*GigaStorageManager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("cannot open storage with invalid configs: %w", err)
	}
	m := &GigaStorageManager{cfg: cfg}
	err := m.OpenDBWithRecovery(ctx)
	if err != nil {
		if closeErr := m.Close(); closeErr != nil {
			logger.Error("failed to close a partially started storage manager", "err", closeErr)
		}
		return nil, err
	}
	if err := m.startGarbageCollector(ctx, cfg.PruningConfig); err != nil {
		if closeErr := m.Close(); closeErr != nil {
			logger.Error("failed to close a partially started storage manager", "err", closeErr)
		}
		return nil, err
	}
	return m, nil
}

// startGarbageCollector runs the prune cycle over the opened stores.
func (m *GigaStorageManager) startGarbageCollector(ctx context.Context, pruningConfig *config.StorageGarbageCollectorConfig) error {
	gc, err := controller.NewStorageGarbageCollector(ctx, pruningConfig, m.prunableStores())
	if err != nil {
		return fmt.Errorf("start storage garbage collector: %w", err)
	}
	m.gc = gc
	return nil
}

// prunableStores returns the opened stores that can join the shared prune cycle. The state stores come
// from the StateDB that owns them.
func (m *GigaStorageManager) prunableStores() []controller.PrunableStore {
	stores := make([]controller.PrunableStore, 0, 5)
	if m.stateDB != nil {
		stores = append(stores, m.stateDB.PrunableStores()...)
	}
	if m.receiptDB != nil {
		stores = append(stores, m.receiptDB)
	}
	if m.blockStore != nil {
		stores = append(stores, m.blockStore)
	}
	return stores
}

// BlockStore returns the block ledger consensus reads and writes.
func (m *GigaStorageManager) BlockStore() *blockstore.Store { return m.blockStore }

// ReceiptDB returns the receipt store, or nil when receipts are disabled.
func (m *GigaStorageManager) ReceiptDB() receipt.ReceiptStore { return m.receiptDB }

// StateDB returns the Giga state DB over the WAL and the two halves of state, or nil when the open did
// not reach it.
func (m *GigaStorageManager) StateDB() *giga.StateDB { return m.stateDB }

// StateStore returns the state store used for execution, or nil when none is configured.
func (m *GigaStorageManager) StateStore() gigatypes.StateDB { return m.stateStore }

// StateWAL returns the state WAL that StateDB writes, or nil before the StateDB is open.
func (m *GigaStorageManager) StateWAL() statewal.StateWAL {
	if m.stateDB == nil {
		return nil
	}
	return m.stateDB.WAL()
}

// SC returns the state commit store, or nil before the StateDB is open.
func (m *GigaStorageManager) SC() *flatkv.CommitStore {
	if m.stateDB == nil {
		return nil
	}
	return m.stateDB.SC()
}

// SS returns the EVM state store, or nil when it is disabled or the StateDB is not open.
func (m *GigaStorageManager) SS() *evm.EVMStateStore {
	if m.stateDB == nil {
		return nil
	}
	return m.stateDB.SS()
}

// Close shuts down the collector and then every store, reporting every failure rather than
// stopping at the first. It tolerates a manager whose open did not finish, and may be called on
// one that was never opened.
func (m *GigaStorageManager) Close() error {
	var errs error
	if m.gc != nil {
		if err := m.gc.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close storage garbage collector: %w", err))
		}
	}
	if m.blockStore != nil {
		// blockStore.Close closes the block database it was built over, so closing that here as well
		// would be a double close.
		if err := m.blockStore.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close block store: %w", err))
		}
	}
	if m.receiptDB != nil {
		if err := m.receiptDB.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close receipt store: %w", err))
		}
	}
	return errors.Join(errs, m.closeState())
}

// closeState closes the state store owned by the manager.
func (m *GigaStorageManager) closeState() error {
	if m.stateStore == nil {
		return nil
	}
	return m.stateStore.Close()
}
