// Package bootstrap opens and owns the set of databases a Giga node runs on.
package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
)

var logger = seilog.NewLogger("db", "giga")

// GigaStorageManager owns every database a Giga node reads and writes, plus the
// checkpoint schedule and prune cycle that run above them.
type GigaStorageManager struct {
	// blockStore.Close closes the block database it was built over.
	blockStore *blockstore.Store

	// receiptDB is nil when ReceiptDBConfig.Enable is false.
	receiptDB receipt.ReceiptStore

	// stateWAL is closed by Close. SC is opened with no WAL of its own; StateDB writes it.
	stateWAL statewal.StateWAL

	sc *flatkv.CommitStore

	// stateDB has no Close; Close shuts down the WAL and SC it is backed by.
	stateDB giga.StateDB

	// ss is nil when SSConfig.Enable is false.
	ss *evm.EVMStateStore

	// gc is nil until startGarbageCollector succeeds.
	gc *controller.StorageGarbageCollector

	checkpointer *controller.CheckpointScheduler
}

// NewGigaStorageManager runs the steps that bring storage up:
//  1. Perform a config validation.
//  2. Construct and open all DBs with the config.
//  3. CrashRecover (not implemented; currently a no-op).
//  4. Register checkpoint scheduler and start garbage collector.
func NewGigaStorageManager(ctx context.Context, cfg config.GigaStorageConfig) (*GigaStorageManager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("cannot open storage with invalid configs: %w", err)
	}

	m := &GigaStorageManager{}
	err := m.openDBs(ctx, cfg)
	if err == nil {
		err = m.CrashRecover()
	}
	if err == nil {
		m.startCheckpointSchedule(cfg.CheckpointConfig)
		err = m.startGarbageCollector(ctx, cfg.PruningConfig)
	}
	if err != nil {
		if closeErr := m.Close(); closeErr != nil {
			logger.Error("failed to close a partially started storage manager", "err", closeErr)
		}
		return nil, err
	}
	return m, nil
}

// openDBs opens the stores named by cfg. Receipts and SS are left nil when disabled.
func (m *GigaStorageManager) openDBs(ctx context.Context, cfg config.GigaStorageConfig) error {
	blockDB, err := littblock.NewBlockDB(cfg.BlockDBConfig)
	if err != nil {
		return fmt.Errorf("open block db: %w", err)
	}
	blockStore, err := blockstore.New(blockDB)
	if err != nil {
		// The store takes ownership of blockDB only once it is built, so nothing else will close it.
		if closeErr := blockDB.Close(); closeErr != nil {
			logger.Error("failed to close the block db after the block store failed to open", "err", closeErr)
		}
		return fmt.Errorf("open block store: %w", err)
	}
	m.blockStore = blockStore

	if cfg.ReceiptDBConfig.Enable {
		// Giga has no legacy receipt KVStore.
		receiptDB, err := receipt.NewReceiptStore(cfg.ReceiptDBConfig, nil)
		if err != nil {
			return fmt.Errorf("open receipt store: %w", err)
		}
		m.receiptDB = receiptDB
	}

	// StateDB writes the WAL; a store that held one would record every block twice.
	sc, err := flatkv.NewCommitStore(ctx, cfg.FlatKVConfig, nil)
	if err != nil {
		return fmt.Errorf("open state commit store: %w", err)
	}
	m.sc = sc
	if err := m.sc.LoadLatest(); err != nil {
		return fmt.Errorf("load state commit store: %w", err)
	}
	if err := m.sc.CleanupOrphanedReadOnlyDirs(); err != nil {
		return fmt.Errorf("clean up orphaned state commit read-only dirs: %w", err)
	}

	// LoadLatest reads the WAL directory out-of-band and takes the lock a live WAL holds.
	stateWAL, err := flatkv.OpenStateWAL(cfg.FlatKVConfig)
	if err != nil {
		return fmt.Errorf("open state WAL: %w", err)
	}
	m.stateWAL = stateWAL

	if cfg.SSConfig.Enable {
		ss, err := evm.NewEVMStateStore(cfg.SSConfig.EVMDBDirectory, cfg.SSConfig)
		if err != nil {
			return fmt.Errorf("open EVM state store: %w", err)
		}
		m.ss = ss
		if err := m.ss.StartSnapshots(utils.GetStateStoreSnapshotsSiblingPath(m.ss.Dir()), cfg.SSConfig, nil); err != nil {
			return fmt.Errorf("start EVM state store snapshot manager: %w", err)
		}
	}

	m.stateDB = giga.NewStateDB(m.stateWAL, m.sc)
	return nil
}

// startCheckpointSchedule puts the opened halves of state on one checkpoint cadence.
func (m *GigaStorageManager) startCheckpointSchedule(cfg config.CheckpointConfig) {
	m.checkpointer = controller.NewCheckpointScheduler(cfg)
	m.sc.SetCheckpointScheduler(m.checkpointer)
	if m.ss != nil {
		m.ss.SetCheckpointScheduler(m.checkpointer)
	}
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

// prunableStores returns the opened stores that can join the shared prune cycle.
func (m *GigaStorageManager) prunableStores() []controller.PrunableStore {
	stores := make([]controller.PrunableStore, 0, 5)
	if m.sc != nil {
		stores = append(stores, m.sc)
	}
	if m.stateWAL != nil {
		stores = append(stores, m.stateWAL)
	}
	if m.receiptDB != nil {
		stores = append(stores, m.receiptDB)
	}
	if m.ss != nil {
		stores = append(stores, m.ss)
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

// StateWAL returns the state WAL that StateDB writes.
func (m *GigaStorageManager) StateWAL() statewal.StateWAL { return m.stateWAL }

// SC returns the state commit store.
func (m *GigaStorageManager) SC() *flatkv.CommitStore { return m.sc }

// StateDB returns the Giga state DB over the WAL and live SC.
func (m *GigaStorageManager) StateDB() giga.StateDB { return m.stateDB }

// SS returns the EVM state store, or nil when the state store is disabled.
func (m *GigaStorageManager) SS() *evm.EVMStateStore { return m.ss }

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
	if m.ss != nil {
		if err := m.ss.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close EVM state store: %w", err))
		}
	}
	if m.sc != nil {
		if err := m.sc.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state commit store: %w", err))
		}
	}
	if m.stateWAL != nil {
		if err := m.stateWAL.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state WAL: %w", err))
		}
	}
	return errs
}
