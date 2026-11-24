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
	"github.com/sei-protocol/sei-chain/sei-db/archive"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
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
	// cfg is the config the stores were opened with.
	cfg *config.GigaStorageConfig

	// blockStore.Close closes the block database it was built over.
	blockStore *blockstore.Store

	// receiptDB is nil when ReceiptDBConfig.Enable is false.
	receiptDB receiptdb.ReceiptStore

	// archive is nil when ArchiveConfig is nil.
	archive archive.ArchivalStore

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

	// stateSyncProofExport writes state sync proof files once StartStateSyncProofExport starts it.
	stateSyncProofExport stateSyncProofExport

	// stateSyncUpload uploads state sync archives once StartStateSyncUpload starts it.
	stateSyncUpload stateSyncUpload
}

// NewGigaStorageManager opens every store a Giga node runs on, first aligning the state stores and the receipt
// store on the lowest height they, the app hash vault and the history archive hold (see alignStores()). It
// refuses to start when any store is ahead of a non-empty block store.
//
// The app hash builder's setup is completed at the aligned height. The builder halts the node before setup
// completes if the aligned state disagrees with the app hash it already stores for that height. A failed call
// closes every store it had opened.
func NewGigaStorageManager(ctx context.Context, cfg *config.GigaStorageConfig) (_ *GigaStorageManager, retErr error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("cannot open storage with invalid configs: %w", err)
	}
	if err := removeStateSyncUploadStaging(cfg.FlatKVConfig.DataDir); err != nil {
		return nil, err
	}
	timer := utils.NewLoggingPhaseTimer(logger)

	timer.SetPhase("block_store")
	blockStore, err := blockstore.New(cfg.BlockDBConfig)
	if err != nil {
		return nil, fmt.Errorf("open block db: %w", err)
	}
	defer closeOnFailure(&retErr, "block store", blockStore.Close)

	blockStoreHead, err := blockStore.GetLatestBlock()
	if err != nil {
		return nil, fmt.Errorf("read block store head: %w", err)
	}
	timer.SetPhase("store_heads")
	if err := logPreviousShutdown(cfg.RunningMarkerPath); err != nil {
		return nil, fmt.Errorf("check how the previous node shut down: %w", err)
	}
	heads, err := readStoreHeads(cfg)
	if err != nil {
		return nil, fmt.Errorf("read the store heads: %w", err)
	}

	if err := assertInvariants(cfg, blockStoreHead, heads); err != nil {
		return nil, fmt.Errorf("assert storage invariants: %w", err)
	}
	target := recoveryTarget(heads)
	timer.SetPhase("archive")
	// An empty archive starts at the block the node executes next: the one after target, or on a fresh chain
	// the giga activation height.
	archiveStore, err := openArchive(ctx, cfg, max(target+1, cfg.AppHashBuilderConfig.GigaActivationHeight))
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	if archiveStore != nil {
		defer closeOnFailure(&retErr, "archive", archiveStore.Close)
		// The archive is never rolled back, so the stores align at or below its head and the node executes the
		// blocks it lacks again. Like the app hash vault's, its head counts even when it is 0.
		target = min(target, archiveStore.LatestBlock())
	}
	timer.SetPhase("align_stores")
	if err := alignStores(ctx, cfg, target); err != nil {
		return nil, fmt.Errorf("align stores: %w", err)
	}

	timer.SetPhase("app_hash_vault")
	hashVault, err := vault.NewStandardHashVault(cfg.AppHashBuilderConfig.HashVaultConfig,
		cfg.AppHashBuilderConfig.ChainID, cfg.AppHashBuilderConfig.GigaActivationHeight)
	if err != nil {
		return nil, fmt.Errorf("open hash vault: %w", err)
	}
	defer closeOnFailure(&retErr, "app hash vault", hashVault.Close)

	timer.SetPhase("app_hash_builder")
	appHashBuilder, err := builder.NewStandardAppHashBuilder(ctx, *cfg.AppHashBuilderConfig, hashVault)
	if err != nil {
		return nil, fmt.Errorf("open app hash builder: %w", err)
	}
	defer closeOnFailure(&retErr, "app hash builder", appHashBuilder.Close)

	timer.SetPhase("receipt_hasher")
	receiptHasher, err := receipthash.NewReceiptHasher(
		ctx, *cfg.ReceiptHasherConfig, appHashBuilder.ReportReceiptHash)
	if err != nil {
		return nil, fmt.Errorf("open receipt hasher: %w", err)
	}
	defer closeOnFailure(&retErr, "receipt hasher", receiptHasher.Close)

	timer.SetPhase("bud_hasher")
	budHasher, err := bud.NewStandardBUDHasher(ctx, *cfg.BUDHasherConfig, appHashBuilder.ReportBUDHash)
	if err != nil {
		return nil, fmt.Errorf("open BUD hasher: %w", err)
	}
	defer closeOnFailure(&retErr, "BUD hasher", budHasher.Close)

	timer.SetPhase("state_db")
	stateDB, err := openStateDB(ctx, cfg, budHasher)
	if err != nil {
		return nil, fmt.Errorf("open the state DB: %w", err)
	}
	defer closeOnFailure(&retErr, "state DB", stateDB.Close)

	timer.SetPhase("receipt_store")
	receiptDB, err := openReceiptStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("open receipt store: %w", err)
	}
	if receiptDB != nil {
		defer closeOnFailure(&retErr, "receipt store", receiptDB.Close)
	}

	timer.SetPhase("app_hash_builder_setup")
	height := uint64(stateDB.SC().Version()) //nolint:gosec // live state DB versions are never negative
	if err := connectLiveStateDBToAppHashBuilder(ctx, stateDB, appHashBuilder, height); err != nil {
		return nil, fmt.Errorf("connect the live state DB to the app hash builder: %w", err)
	}
	if err := appHashBuilder.SetupComplete(ctx, height); err != nil {
		return nil, fmt.Errorf("complete the app hash builder setup at block %d: %w", height, err)
	}

	timer.SetPhase("gc")
	gc, err := controller.NewStorageGarbageCollector(
		ctx, cfg.PruningConfig, prunableStores(stateDB, receiptDB, blockStore, hashVault))
	if err != nil {
		return nil, fmt.Errorf("start storage garbage collector: %w", err)
	}
	defer closeOnFailure(&retErr, "storage garbage collector", gc.Close)
	if err := createRunningMarker(cfg.RunningMarkerPath); err != nil {
		return nil, fmt.Errorf("mark the stores as held open: %w", err)
	}
	timer.Log("Opened storage", "height", height)

	return &GigaStorageManager{
		cfg:            cfg,
		blockStore:     blockStore,
		receiptDB:      receiptDB,
		archive:        archiveStore,
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
	timer := utils.NewLoggingPhaseTimer(logger)
	timer.SetPhase("live_state_db")
	liveStateDB, err := flatkv.OpenLiveStateDB(ctx, cfg.FlatKVConfig)
	if err != nil {
		return nil, fmt.Errorf("open the live state DB: %w", err)
	}
	defer closeOnFailure(&retErr, "live state DB", liveStateDB.Close)
	timer.SetPhase("state_wal")
	wal, err := flatkv.OpenStateWAL(cfg.FlatKVConfig)
	if err != nil {
		return nil, fmt.Errorf("open the state WAL: %w", err)
	}
	defer closeOnFailure(&retErr, "state WAL", wal.Close)

	timer.SetPhase("catch_up")
	if err := flatkv.CatchUpToStateWAL(ctx, liveStateDB, wal); err != nil {
		return nil, fmt.Errorf("catch the live state DB up to the state WAL: %w", err)
	}
	checkpointer := controller.NewCheckpointScheduler(cfg.CheckpointConfig)
	liveStateDB.SetCheckpointScheduler(checkpointer)

	var historicalStateDB *undo.Database
	if cfg.SSConfig.Enable {
		timer.SetPhase("historical_state_db")
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
	timer.Log("Opened the state DB")
	return stateDB, nil
}

// connectLiveStateDBToAppHashBuilder hands appHashBuilder the state hash of block height, which the live state DB
// stands at, then of every block it commits after. The app hash builder checks the first hash against the app hash
// it already stores for that block, and halts the node on a mismatch before its setup completes.
func connectLiveStateDBToAppHashBuilder(
	ctx context.Context,
	stateDB *giga.StateDB,
	appHashBuilder builder.AppHashBuilder,
	// The block the live state DB stands at.
	height uint64,
) error {
	reportStateHash := func(ctx context.Context, _ int64, hash *lthash.BlockHash) error {
		height := uint64(hash.BlockNumber) //nolint:gosec // block heights are never negative
		stateHash := hash.Global.Checksum()
		componentHashes := hashlog.FlatKVComponentHashes(hash)
		if err := appHashBuilder.ReportStateHash(ctx, height, stateHash, componentHashes); err != nil {
			return fmt.Errorf("report the state hash of block %d: %w", height, err)
		}
		return nil
	}
	// The blocks the live state DB replayed at open are hashed in the background. Registering before their hashes
	// are delivered would hand back an earlier block's hash than the one it stands at.
	if err := stateDB.SC().FlushHashes(); err != nil {
		return fmt.Errorf("wait for the live state DB's state hashes: %w", err)
	}
	current, err := stateDB.RegisterHashListener(reportStateHash)
	if err != nil {
		return fmt.Errorf("register the app hash builder for state hashes: %w", err)
	}
	//nolint:gosec // block heights are never negative
	if currentHeight := uint64(current.BlockNumber); currentHeight != height {
		return fmt.Errorf("the live state DB stands at block %d, but its newest state hash is of block %d",
			height, currentHeight)
	}
	// Nothing commits before the storage manager is returned, so no delivery can precede this one.
	if err := reportStateHash(ctx, current.BlockNumber, &current); err != nil {
		return fmt.Errorf("report the state hash of block %d: %w", height, err)
	}
	return nil
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
	receiptDB receiptdb.ReceiptStore,
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
func (m *GigaStorageManager) ReceiptDB() receiptdb.ReceiptStore { return m.receiptDB }

// Archive returns the history archive, or nil when no archive is configured.
func (m *GigaStorageManager) Archive() archive.ArchivalStore { return m.archive }

// StateDB returns the Giga state DB over the state WAL, the live state DB and the historical state DB.
func (m *GigaStorageManager) StateDB() *giga.StateDB { return m.stateDB }

// AppHashBuilder returns the app hash builder.
func (m *GigaStorageManager) AppHashBuilder() builder.AppHashBuilder { return m.appHashBuilder }

// ReceiptHasher returns the hasher that reports the receipt hash of each executed block to the app hash builder.
func (m *GigaStorageManager) ReceiptHasher() *receipthash.ReceiptHasher { return m.receiptHasher }

// Close brings every store to the same block, closes them, and records a clean shutdown if both succeed. No block
// may be committed during or after the call.
func (m *GigaStorageManager) Close() error {
	var errs error
	if err := m.flushStores(); err != nil {
		errs = fmt.Errorf("bring the stores to the same block: %w", err)
	}

	timer := utils.NewLoggingPhaseTimer(logger)
	timer.SetPhase("state_sync_upload")
	m.stateSyncUpload.stop()
	timer.SetPhase("state_sync_proof_export")
	// Before the block store, which the export reads.
	m.stateSyncProofExport.stop()
	timer.SetPhase("gc")
	if err := m.gc.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close storage garbage collector: %w", err))
	}
	timer.SetPhase("block_store")
	// blockStore.Close closes the block database it was built over, so closing that here as well
	// would be a double close.
	if err := m.blockStore.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close block store: %w", err))
	}
	if m.receiptDB != nil {
		timer.SetPhase("receipt_store")
		if err := m.receiptDB.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close receipt store: %w", err))
		}
	}
	if m.archive != nil {
		timer.SetPhase("archive")
		if err := m.archive.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close archive: %w", err))
		}
	}
	timer.SetPhase("state_db")
	if err := m.stateDB.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close state DB: %w", err))
	}
	timer.SetPhase("receipt_hasher")
	// Before the builder, which it reports into until it stops.
	if err := m.receiptHasher.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close receipt hasher: %w", err))
	}
	timer.SetPhase("bud_hasher")
	// After the state DB, which submits to it, and before the builder, which it reports into until it stops.
	if err := m.budHasher.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close BUD hasher: %w", err))
	}
	timer.SetPhase("app_hash_builder")
	// After the state DB, whose hash listeners report into the builder until they stop.
	if err := m.appHashBuilder.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close app hash builder: %w", err))
	}
	timer.SetPhase("app_hash_vault")
	// After the builder, which stores into it until it stops.
	if err := m.appHashVault.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close app hash vault: %w", err))
	}
	timer.Log("Closed storage")
	if errs == nil {
		if err := m.removeRunningMarker(); err != nil {
			errs = fmt.Errorf("record a clean shutdown: %w", err)
		}
	}
	return errs
}
