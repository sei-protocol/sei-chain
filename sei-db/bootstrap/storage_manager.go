// Package bootstrap opens and owns the set of databases a Giga node runs on.
//
// It cannot live in sei-db/controller, which is where the prune cycle it drives is defined:
// flatkv, statewal, receipt and ss/evm all import that package to declare themselves
// PrunableStore, so a composition root there would be an import cycle.
//
// The block ledger is held as the consensus block store rather than as the block database beneath
// it, which is why a package under sei-db reaches up into autobahn: the prune policy for that ledger
// is only expressible where the record codec is.
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
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
)

var logger = seilog.NewLogger("db", "giga")

// GigaStorageManager owns every database a Giga node reads and writes: the block ledger, the
// receipt ledger, the state WAL, and the SC and SS halves of state. It also owns the two cycles that
// run above them — the schedule they checkpoint on and the collector that prunes them.
//
// It opens them as one unit so their heights can be reconciled against each other, which is what
// CrashRecover does.
//
// Each store is typed as the constructor that fills it returns, which is why some fields are
// interfaces and some are pointers: receipts and the state WAL are opened through factories that
// choose a backend, while the block store, SC and SS have one implementation each. Narrowing those
// three to an interface would drop methods the open path calls — types.StateStore, for one, carries
// neither StartSnapshots nor Dir.
type GigaStorageManager struct {
	// blockStore owns the block database it was built over, and its Close is what closes it. Close
	// relies on that. The manager holds the store rather than the database because the database
	// cannot be pruned safely: where a QC's covered range begins and how far the application has
	// committed are encoded in record values it holds as opaque bytes. The store owns that codec,
	// so it is the block ledger's PrunableStore and the layer that can answer for its head.
	blockStore *blockstore.Store

	// receiptDB is nil when ReceiptDBConfig.Enable is false.
	receiptDB receipt.ReceiptStore

	// stateWAL is injected into sc, and sc.Close is what closes it. Close relies on that.
	stateWAL statewal.StateWAL

	sc *flatkv.CommitStore

	// ss is nil when SSConfig.Enable is false.
	ss *evm.EVMStateStore

	// gc is the shared prune cycle. Nil until startGarbageCollector succeeds.
	gc *controller.StorageGarbageCollector

	// checkpointer holds the halves of state this node opened to the same checkpoint heights. It has
	// no goroutine of its own: the stores drive it from their commit paths.
	checkpointer *controller.CheckpointScheduler
}

// NewGigaStorageManager brings a Giga node's storage up: it opens every database named by cfg,
// reconciles their heights, and starts the two cycles that run above them — the checkpoint schedule
// and the prune cycle.
//
// The manager is ready to serve on return. A partially started one is closed before the error is
// returned, so a failure here leaks no file locks.
func NewGigaStorageManager(ctx context.Context, cfg config.GigaStorageConfig) (*GigaStorageManager, error) {
	m := &GigaStorageManager{}
	if err := m.start(ctx, cfg); err != nil {
		if closeErr := m.Close(); closeErr != nil {
			logger.Error("failed to close a partially started storage manager", "err", closeErr)
		}
		return nil, err
	}
	return m, nil
}

// start runs the steps that bring storage up, in the only order they permit.
//
// Validation comes first so a config no store will accept costs nothing to reject. Recovery comes
// after every store is open, because it is a comparison across all of them, and
// before either cycle starts, because both read heights that recovery moves: a prune cycle overlapping
// a rollback derives its cut line from heights that are still falling.
func (m *GigaStorageManager) start(ctx context.Context, cfg config.GigaStorageConfig) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("cannot open storage: %w", err)
	}
	if err := m.openDBs(ctx, cfg); err != nil {
		return err
	}
	if err := m.CrashRecover(); err != nil {
		return err
	}
	m.startCheckpointSchedule(cfg.CheckpointConfig)
	return m.startGarbageCollector(ctx, cfg.PruningConfig)
}

// openDBs fills in the stores this config asks for, in an order chosen so that each one is reachable
// by Close as soon as it exists: the block ledger, receipts, the state WAL, SC (which adopts the
// WAL), then SS.
//
// Receipts and SS each have an Enable of their own and are left nil when it is false. The block
// ledger, the state WAL and SC have none: a Giga node is not a node without them.
//
// Each store recovers itself to whatever height its own durable state reaches, so on return they can
// disagree with each other. CrashRecover is what closes that gap.
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
		// The nil store key is the legacy KVStore the receipt store falls back to for receipts
		// written before it existed. A Giga node has none, since Giga requires a state sync.
		receiptDB, err := receipt.NewReceiptStore(cfg.ReceiptDBConfig, nil)
		if err != nil {
			return fmt.Errorf("open receipt store: %w", err)
		}
		m.receiptDB = receiptDB
	}

	stateWAL, err := flatkv.OpenStateWAL(cfg.FlatKVConfig)
	if err != nil {
		return fmt.Errorf("open state WAL: %w", err)
	}
	m.stateWAL = stateWAL

	// NewCommitStore adopts the WAL, so m.sc is assigned before LoadLatest can fail: from here on
	// closing the WAL is sc's job and Close must not do it twice.
	sc, err := flatkv.NewCommitStore(ctx, cfg.FlatKVConfig, stateWAL)
	if err != nil {
		return fmt.Errorf("open state commit store: %w", err)
	}
	m.sc = sc
	// NewCommitStore returns an unopened store. Until it is loaded it holds no file lock, has no
	// snapshot writer, and reports no version — so a manager that skipped this would hand recovery a
	// state height of 0 and roll every other store back to genesis.
	if err := m.sc.LoadLatest(); err != nil {
		return fmt.Errorf("load state commit store: %w", err)
	}
	if err := m.sc.CleanupOrphanedReadOnlyDirs(); err != nil {
		return fmt.Errorf("clean up orphaned state commit read-only dirs: %w", err)
	}

	if cfg.SSConfig.Enable {
		ss, err := evm.NewEVMStateStore(cfg.SSConfig.EVMDBDirectory, cfg.SSConfig)
		if err != nil {
			return fmt.Errorf("open EVM state store: %w", err)
		}
		m.ss = ss
		// SS is the only snapshot member on a Giga node, so it holds the height a restore starts from
		// on its own and shares no floor. The root is the one the composite path uses, which is where
		// the rollback tooling looks for these snapshots.
		if err := m.ss.StartSnapshots(utils.GetStateStoreSnapshotsSiblingPath(m.ss.Dir()), cfg.SSConfig, nil); err != nil {
			return fmt.Errorf("start EVM state store snapshot manager: %w", err)
		}
	}

	// TODO: pass wal, sc and ss to GigaStateDB constructor

	return nil
}

// startCheckpointSchedule puts the halves of state this node opened on one checkpoint cadence, in
// place of the per-store intervals they would otherwise each keep.
//
// Holding them to the same heights is what makes a restore resolve one height rather than the newest
// each of them happens to hold: SC restores from its snapshot and SS from its own, and a rollback can
// only target a height both of them have. With SS disabled there is only SC to hold, and the schedule
// still replaces its own interval.
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

// StateWAL returns the state WAL that SC replays from.
func (m *GigaStorageManager) StateWAL() statewal.StateWAL { return m.stateWAL }

// SC returns the state commit store.
func (m *GigaStorageManager) SC() *flatkv.CommitStore { return m.sc }

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
	switch {
	case m.sc != nil:
		// sc.Close closes the WAL it adopted, so closing the WAL here as well would be a
		// double close.
		if err := m.sc.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state commit store: %w", err))
		}
	case m.stateWAL != nil:
		// open failed before SC adopted the WAL, so nothing else will close it.
		if err := m.stateWAL.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state WAL: %w", err))
		}
	}
	return errs
}
