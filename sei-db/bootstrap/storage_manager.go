// Package bootstrap opens and owns the set of databases a Giga node runs on.
//
// It cannot live in sei-db/controller, which is where the prune cycle it drives is defined:
// flatkv, statewal, receipt and ss/evm all import that package to declare themselves
// PrunableStore, so a composition root there would be an import cycle.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

var logger = seilog.NewLogger("db", "giga")

// GigaStorageManager owns every database a Giga node reads and writes: the block ledger, the
// receipt ledger, the state WAL, the SC and SS halves of state, and the collector that prunes
// them. It opens them as one unit so their heights can be reconciled against each other, which
// is what RecoverFromCrash does.
type GigaStorageManager struct {
	blockDB blocktypes.BlockDB

	// receiptDB is nil when ReceiptDBConfig.Enable is false, which is the one store a Giga
	// node may run without.
	receiptDB receipt.ReceiptStore

	// stateWAL is injected into sc, and sc.Close is what closes it. Close relies on that.
	stateWAL statewal.StateWAL

	sc *flatkv.CommitStore
	ss *evm.EVMStateStore

	gc *controller.StorageGarbageCollector
}

// NewGigaStorageManager opens every database named by cfg and starts the prune cycle over the
// ones that participate in it.
//
// The stores are opened but not reconciled: each one recovers itself to whatever height its own
// durable state reaches, and they can disagree. Call RecoverFromCrash before serving.
//
// A partially opened manager is closed before the error is returned, so a failure here leaks no
// file locks.
func NewGigaStorageManager(ctx context.Context, cfg config.GigaStorageConfig) (*GigaStorageManager, error) {
	m := &GigaStorageManager{}
	if err := m.open(ctx, handRetentionToCollector(cfg)); err != nil {
		if closeErr := m.Close(); closeErr != nil {
			logger.Error("failed to close a partially opened storage manager", "err", closeErr)
		}
		return nil, err
	}
	return m, nil
}

// open fills in every store, in an order chosen so that each one is reachable by Close as soon
// as it exists: state WAL, SC (which adopts the WAL), SS, receipts, block ledger, collector.
func (m *GigaStorageManager) open(ctx context.Context, cfg config.GigaStorageConfig) error {
	stateWAL, err := flatkv.OpenStateWAL(cfg.FlatKVConfig)
	if err != nil {
		return fmt.Errorf("open state WAL: %w", err)
	}
	m.stateWAL = stateWAL

	// NewCommitStore adopts the WAL, so m.sc is assigned before LoadLatest can fail: from here
	// on closing the WAL is sc's job and Close must not do it twice.
	sc, err := flatkv.NewCommitStore(ctx, cfg.FlatKVConfig, stateWAL)
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

	ss, err := evm.NewEVMStateStore(cfg.SSConfig.EVMDBDirectory, cfg.SSConfig)
	if err != nil {
		return fmt.Errorf("open EVM state store: %w", err)
	}
	m.ss = ss

	if cfg.ReceiptDBConfig.Enable {
		// The nil store key is the legacy KVStore the receipt store falls back to for receipts
		// written before it existed. A Giga node has none, since Giga requires a state sync.
		receiptDB, err := receipt.NewReceiptStore(cfg.ReceiptDBConfig, nil)
		if err != nil {
			return fmt.Errorf("open receipt store: %w", err)
		}
		m.receiptDB = receiptDB
	}

	blockDB, err := littblock.NewBlockDB(cfg.BlockDBConfig)
	if err != nil {
		return fmt.Errorf("open block db: %w", err)
	}
	m.blockDB = blockDB

	return m.startGarbageCollector(ctx, cfg.PruningConfig)
}

// BlockDB returns the block ledger.
func (m *GigaStorageManager) BlockDB() blocktypes.BlockDB { return m.blockDB }

// ReceiptDB returns the receipt store, or nil when receipts are disabled.
func (m *GigaStorageManager) ReceiptDB() receipt.ReceiptStore { return m.receiptDB }

// StateWAL returns the state WAL that SC replays from.
func (m *GigaStorageManager) StateWAL() statewal.StateWAL { return m.stateWAL }

// SC returns the state commit store.
func (m *GigaStorageManager) SC() *flatkv.CommitStore { return m.sc }

// SS returns the EVM state store.
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
	if m.blockDB != nil {
		if err := m.blockDB.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close block db: %w", err))
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

// handRetentionToCollector returns cfg with the stores the collector will prune marked to stand
// their own pruners down.
//
// Only SC is marked. Its config is the one the collector fully replaces: with ExternalPruning
// set it stops pruning snapshots by count and stops truncating the state WAL, and the collector
// does both — snapshots through SC itself, the WAL through the state WAL store. Every other
// store keeps whatever pruner it has, because marking a store the collector does not manage
// stands a pruner down with nothing in its place, and the growth that follows is silent.
//
// The FlatKV config is copied rather than mutated: it reaches us as a pointer the caller still
// holds.
func handRetentionToCollector(cfg config.GigaStorageConfig) config.GigaStorageConfig {
	if cfg.PruningConfig == nil {
		return cfg
	}
	cfg.FlatKVConfig = cfg.FlatKVConfig.Copy()
	cfg.FlatKVConfig.ExternalPruning = true
	return cfg
}

// startGarbageCollector runs the prune cycle over the opened stores that can join it. A nil
// pruningConfig leaves every store on its own retention and starts no collector.
func (m *GigaStorageManager) startGarbageCollector(ctx context.Context, pruningConfig *config.StorageGarbageCollectorConfig) error {
	if pruningConfig == nil {
		logger.Info("no pruning config; every store keeps its own retention")
		return nil
	}
	gc, err := controller.NewStorageGarbageCollector(ctx, pruningConfig, m.prunableStores())
	if err != nil {
		return fmt.Errorf("start storage garbage collector: %w", err)
	}
	m.gc = gc
	return nil
}

// prunableStores returns the opened stores that implement controller.PrunableStore.
//
// The ones that do not are named in a warning rather than dropped quietly, because the cut line
// the collector prunes to is a minimum over the floors it is handed: a store left out cannot
// hold that line down to the height it can still restore to, so a rollback inside the configured
// window can find that store already pruned past the target.
func (m *GigaStorageManager) prunableStores() []controller.PrunableStore {
	candidates := []struct {
		name  string
		store any
	}{
		{"FlatKV", m.sc},
		{"StateWAL", m.stateWAL},
		{"ReceiptDB", m.receiptDB},
		{"EVM SS", m.ss},
		{"BlockDB", m.blockDB},
	}

	var stores []controller.PrunableStore
	var outside []string
	for _, candidate := range candidates {
		if candidate.store == nil {
			continue // receipts are off
		}
		prunable, ok := candidate.store.(controller.PrunableStore)
		if !ok {
			outside = append(outside, candidate.name)
			continue
		}
		stores = append(stores, prunable)
	}
	if len(outside) > 0 {
		logger.Warn("stores outside the shared prune cycle: they prune themselves, and the shared cut line does not account for what they can restore to",
			"stores", strings.Join(outside, ", "))
	}
	return stores
}
