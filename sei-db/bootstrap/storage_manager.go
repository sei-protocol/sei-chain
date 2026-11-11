// Package bootstrap opens and owns the set of databases a Giga node runs on.
package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash/bud"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/giga/apphash/receipthash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "giga")

// GigaStorageManager owns every database a Giga node reads and writes, plus the
// checkpoint schedule and prune cycle that run above them.
type GigaStorageManager struct {
	// blockStore.Close closes the block database it was built over.
	blockStore *blockstore.Store

	// receiptDB is nil when ReceiptDBConfig.Enable is false.
	receiptDB receipt.ReceiptStore

	// stateDB owns the live state DB, the historical state DB and the state WAL they share, along
	// with the checkpoint schedule those stores run on.
	stateDB *giga.StateDB

	// gc runs the prune cycle over every store above.
	gc *controller.StorageGarbageCollector

	// appHashBuilder builds the app hash of each block from the hashes reported to it.
	appHashBuilder builder.AppHashBuilder

	// appHashVault holds the app hashes appHashBuilder stores.
	appHashVault vault.HashVault

	// receiptHasher reports the receipt hash of each executed block to appHashBuilder.
	receiptHasher *receipthash.ReceiptHasher

	// budHasher reports the BUD hash of each block the state DB commits to appHashBuilder.
	budHasher bud.BUDHasher
}

// NewGigaStorageManager opens every store a Giga node runs on, first aligning the state stores and the receipt
// store on the lowest height they and the app hash vault hold (see alignStores()). It refuses to start when any
// store is ahead of a non-empty block store.
//
// The app hash builder's setup is completed at the aligned height. The builder halts the node before setup
// completes if the aligned state disagrees with the app hash it already stores for that height. A failed call
// closes every store it had opened.
func NewGigaStorageManager(ctx context.Context, cfg *config.GigaStorageConfig) (_ *GigaStorageManager, retErr error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("cannot open storage with invalid configs: %w", err)
	}

	blockStore, err := blockstore.New(cfg.BlockDBConfig)
	if err != nil {
		return nil, fmt.Errorf("open block db: %w", err)
	}
	defer closeOnFailure(&retErr, "block store", blockStore.Close)

	blockStoreHead, err := blockStore.GetLatestBlock()
	if err != nil {
		return nil, fmt.Errorf("read block store head: %w", err)
	}
	heads, err := readStoreHeads(cfg)
	if err != nil {
		return nil, fmt.Errorf("read the store heads: %w", err)
	}

	if err := assertInvariants(cfg, blockStoreHead, heads); err != nil {
		return nil, fmt.Errorf("assert storage invariants: %w", err)
	}
	if err := alignStores(ctx, cfg, recoveryTarget(heads)); err != nil {
		return nil, fmt.Errorf("align stores: %w", err)
	}

	hashVault, err := vault.NewStandardHashVault(cfg.AppHashBuilderConfig.HashVaultConfig,
		cfg.AppHashBuilderConfig.ChainID, cfg.AppHashBuilderConfig.GigaActivationHeight)
	if err != nil {
		return nil, fmt.Errorf("open hash vault: %w", err)
	}
	defer closeOnFailure(&retErr, "app hash vault", hashVault.Close)

	appHashBuilder, err := builder.NewStandardAppHashBuilder(ctx, *cfg.AppHashBuilderConfig, hashVault)
	if err != nil {
		return nil, fmt.Errorf("open app hash builder: %w", err)
	}
	defer closeOnFailure(&retErr, "app hash builder", appHashBuilder.Close)

	receiptHasher, err := receipthash.NewReceiptHasher(
		ctx, *cfg.ReceiptHasherConfig, appHashBuilder.ReportReceiptHash)
	if err != nil {
		return nil, fmt.Errorf("open receipt hasher: %w", err)
	}
	defer closeOnFailure(&retErr, "receipt hasher", receiptHasher.Close)

	budHasher, err := bud.NewStandardBUDHasher(ctx, *cfg.BUDHasherConfig, appHashBuilder.ReportBUDHash)
	if err != nil {
		return nil, fmt.Errorf("open BUD hasher: %w", err)
	}
	defer closeOnFailure(&retErr, "BUD hasher", budHasher.Close)

	stateDB, err := openStateDB(ctx, cfg, budHasher)
	if err != nil {
		return nil, fmt.Errorf("open the state DB: %w", err)
	}
	defer closeOnFailure(&retErr, "state DB", stateDB.Close)

	receiptDB, err := openReceiptStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("open receipt store: %w", err)
	}
	if receiptDB != nil {
		defer closeOnFailure(&retErr, "receipt store", receiptDB.Close)
	}

	height, err := connectLiveStateDBToAppHashBuilder(ctx, stateDB, appHashBuilder)
	if err != nil {
		return nil, fmt.Errorf("connect the live state DB to the app hash builder: %w", err)
	}
	if err := appHashBuilder.SetupComplete(ctx, height); err != nil {
		return nil, fmt.Errorf("complete the app hash builder setup at block %d: %w", height, err)
	}

	gc, err := controller.NewStorageGarbageCollector(
		ctx, cfg.PruningConfig, prunableStores(stateDB, receiptDB, blockStore, hashVault))
	if err != nil {
		return nil, fmt.Errorf("start storage garbage collector: %w", err)
	}

	return &GigaStorageManager{
		blockStore:     blockStore,
		receiptDB:      receiptDB,
		stateDB:        stateDB,
		gc:             gc,
		appHashBuilder: appHashBuilder,
		appHashVault:   hashVault,
		receiptHasher:  receiptHasher,
		budHasher:      budHasher,
	}, nil
}

// openStateDB opens the live state DB, the state WAL and the historical state DB once each, replays the WAL into the
// live state DB up to the WAL's head, and assembles them into a StateDB that owns them.
func openStateDB(
	ctx context.Context,
	cfg *config.GigaStorageConfig,
	// The hasher each block the state DB commits is submitted to.
	budHasher bud.BUDHasher,
) (_ *giga.StateDB, retErr error) {
	liveStateDB, err := flatkv.OpenLiveStateDB(ctx, cfg.FlatKVConfig)
	if err != nil {
		return nil, fmt.Errorf("open the live state DB: %w", err)
	}
	defer closeOnFailure(&retErr, "live state DB", liveStateDB.Close)
	wal, err := flatkv.OpenStateWAL(cfg.FlatKVConfig)
	if err != nil {
		return nil, fmt.Errorf("open the state WAL: %w", err)
	}
	defer closeOnFailure(&retErr, "state WAL", wal.Close)

	// Before the replay, so the blocks it replays offer themselves to the schedule as committed blocks do.
	checkpointer := controller.NewCheckpointScheduler(cfg.CheckpointConfig)
	liveStateDB.SetCheckpointScheduler(checkpointer)
	if err := flatkv.CatchUpToStateWAL(ctx, liveStateDB, wal); err != nil {
		return nil, fmt.Errorf("catch the live state DB up to the state WAL: %w", err)
	}

	var historicalStateDB *undo.Database
	if cfg.SSConfig.Enable {
		historicalStateDB, err = undo.OpenDB(cfg.SSConfig.EVMDBDirectory, cfg.SSConfig)
		if err != nil {
			return nil, fmt.Errorf("open the historical state DB: %w", err)
		}
		defer closeOnFailure(&retErr, "historical state DB", historicalStateDB.Close)
	}

	stateDB, err := giga.NewStateDB(cfg.FlatKVConfig, liveStateDB, historicalStateDB, wal, checkpointer, budHasher)
	if err != nil {
		return nil, fmt.Errorf("assemble the state DB: %w", err)
	}
	return stateDB, nil
}

// connectLiveStateDBToAppHashBuilder hands appHashBuilder the state hash of the block the live state DB stands at,
// then of every block it commits after, and returns that first block's height. The builder checks the first hash
// against the app hash it already stores for that block, and halts the node on a mismatch before its setup
// completes.
func connectLiveStateDBToAppHashBuilder(
	ctx context.Context,
	stateDB *giga.StateDB,
	appHashBuilder builder.AppHashBuilder,
) (uint64, error) {
	reportStateHash := func(ctx context.Context, _ int64, hash *lthash.BlockHash) error {
		height := uint64(hash.BlockNumber) //nolint:gosec // block heights are never negative
		stateHash := hash.Global.Checksum()
		componentHashes := hashlog.FlatKVComponentHashes(hash)
		if err := appHashBuilder.ReportStateHash(ctx, height, stateHash, componentHashes); err != nil {
			return fmt.Errorf("report the state hash of block %d: %w", height, err)
		}
		return nil
	}
	current, err := stateDB.RegisterHashListener(reportStateHash)
	if err != nil {
		return 0, fmt.Errorf("register the app hash builder for state hashes: %w", err)
	}
	// Nothing commits before the storage manager is returned, so no delivery can precede this one.
	if err := reportStateHash(ctx, current.BlockNumber, &current); err != nil {
		return 0, fmt.Errorf("report the first state hash: %w", err)
	}
	return uint64(current.BlockNumber), nil //nolint:gosec // block heights are never negative
}

// closeOnFailure closes a store when the function that opened it goes on to fail.
func closeOnFailure(
	// The opening function's error. A close error is joined into it.
	retErr *error,
	// The store's name, for the close error.
	name string,
	// Closes the store.
	closeStore func() error,
) {
	if *retErr == nil {
		return
	}
	if err := closeStore(); err != nil {
		*retErr = errors.Join(*retErr, fmt.Errorf("close %s: %w", name, err))
	}
}

// prunableStores returns the stores that join the shared prune cycle. The state stores come from the
// StateDB that owns them.
func prunableStores(
	stateDB *giga.StateDB,
	// Nil when receipts are disabled.
	receiptDB receipt.ReceiptStore,
	blockStore *blockstore.Store,
	hashVault vault.HashVault,
) []controllertypes.PrunableStore {
	stores := make([]controllertypes.PrunableStore, 0, 6)
	stores = append(stores, stateDB.PrunableStores()...)
	if receiptDB != nil {
		stores = append(stores, receiptDB)
	}
	return append(stores, blockStore, hashVault)
}

// BlockStore returns the block ledger consensus reads and writes.
func (m *GigaStorageManager) BlockStore() *blockstore.Store { return m.blockStore }

// ReceiptDB returns the receipt store, or nil when receipts are disabled.
func (m *GigaStorageManager) ReceiptDB() receipt.ReceiptStore { return m.receiptDB }

// StateDB returns the Giga state DB over the state WAL, the live state DB and the historical state DB.
func (m *GigaStorageManager) StateDB() *giga.StateDB { return m.stateDB }

// AppHashBuilder returns the app hash builder.
func (m *GigaStorageManager) AppHashBuilder() builder.AppHashBuilder { return m.appHashBuilder }

// ReceiptHasher returns the hasher that reports the receipt hash of each executed block to the app hash builder.
func (m *GigaStorageManager) ReceiptHasher() *receipthash.ReceiptHasher { return m.receiptHasher }

// Close stops the garbage collector and closes every store.
func (m *GigaStorageManager) Close() error {
	var errs error
	var timer utils.CloseTimer
	if err := timer.Close("gc", m.gc.Close); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close storage garbage collector: %w", err))
	}
	// blockStore.Close closes the block database it was built over, so closing that here as well
	// would be a double close.
	if err := timer.Close("block_store", m.blockStore.Close); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close block store: %w", err))
	}
	if m.receiptDB != nil {
		if err := timer.Close("receipt_store", m.receiptDB.Close); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close receipt store: %w", err))
		}
	}
	if err := timer.Close("state_db", m.stateDB.Close); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close state DB: %w", err))
	}
	// Before the builder, which it reports into until it stops.
	if err := timer.Close("receipt_hasher", m.receiptHasher.Close); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close receipt hasher: %w", err))
	}
	// After the state DB, which submits to it, and before the builder, which it reports into until it stops.
	if err := timer.Close("bud_hasher", m.budHasher.Close); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close BUD hasher: %w", err))
	}
	// After the state DB, whose hash listeners report into the builder until they stop.
	if err := timer.Close("app_hash_builder", m.appHashBuilder.Close); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close app hash builder: %w", err))
	}
	// After the builder, which stores into it until it stops.
	if err := timer.Close("app_hash_vault", m.appHashVault.Close); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close app hash vault: %w", err))
	}
	logger.Info("Closed storage", timer.Fields()...)
	return errs
}
