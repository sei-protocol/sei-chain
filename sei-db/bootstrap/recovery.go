package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/sei-db/archive"
	"github.com/sei-protocol/sei-chain/sei-db/archive/chstore"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	"github.com/sei-protocol/sei-chain/sei-db/receipt_db/littidx"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// ErrReceiptStoreDisabledWithHistory is returned when the receipt store is disabled while its
// directory still holds receipts.
var ErrReceiptStoreDisabledWithHistory = errors.New("receipt store is disabled but holds receipts")

// storeHeads holds the newest block each store that recovery aligns holds, read from their files before they open.
type storeHeads struct {
	// The oldest block the state WAL holds, or 0 when it holds none.
	stateWALFirst uint64

	// The newest block the state WAL holds, or 0 when it holds none.
	stateWAL uint64

	// The version the live state DB opens at, 0 when it holds no state.
	liveStateDB uint64

	// The newest block the historical state DB holds, or 0 when it holds none or is disabled.
	historicalStateDB uint64

	// The newest block the receipt store holds, or 0 when it holds none or is disabled.
	receiptStore uint64

	// The newest block whose app hash the vault holds, or the block before the giga activation height when it
	// holds none.
	appHashVault uint64
}

// readStoreHeads reads the head of every store recovery aligns. It reads their files, so it must run before they
// open.
func readStoreHeads(cfg *config.GigaStorageConfig) (storeHeads, error) {
	var heads storeHeads
	var err error
	logger.Info("Reading a store head", "store", "state WAL")
	heads.stateWALFirst, heads.stateWAL, err = stateWALRange(cfg)
	if err != nil {
		return storeHeads{}, fmt.Errorf("read state WAL range: %w", err)
	}
	logger.Info("Reading a store head", "store", "live state DB")
	liveStateVersion, _, err := flatkv.StoredVersions(cfg.FlatKVConfig.DataDir)
	if err != nil {
		return storeHeads{}, fmt.Errorf("read live state DB version: %w", err)
	}
	heads.liveStateDB = uint64(liveStateVersion) //nolint:gosec // live state DB versions are never negative
	if cfg.SSConfig.Enable {
		logger.Info("Reading a store head", "store", "historical state DB")
		_, heads.historicalStateDB, err = undo.GetLatestBlock(cfg.SSConfig.EVMDBDirectory)
		if err != nil {
			return storeHeads{}, fmt.Errorf("read historical state DB head: %w", err)
		}
	}
	if cfg.ReceiptDBConfig.Enable {
		logger.Info("Reading a store head", "store", "receipt store")
		heads.receiptStore, err = littidx.GetLatestBlock(cfg.ReceiptDBConfig)
		if err != nil {
			return storeHeads{}, fmt.Errorf("read receipt store head: %w", err)
		}
	}
	logger.Info("Reading a store head", "store", "app hash vault")
	heads.appHashVault, err = builder.GetLatestBlock(*cfg.AppHashBuilderConfig)
	if err != nil {
		return storeHeads{}, fmt.Errorf("read app hash vault head: %w", err)
	}
	logger.Info("Read the store heads",
		"state_wal_first", heads.stateWALFirst,
		"state_wal", heads.stateWAL,
		"live_state_db", heads.liveStateDB,
		"historical_state_db", heads.historicalStateDB,
		"receipt_store", heads.receiptStore,
		"app_hash_vault", heads.appHashVault)
	return heads, nil
}

// stateWALRange returns the first and last blocks the state WAL holds, both 0 when it holds none.
//
// It reads the WAL directory rather than an open WAL, which takes that directory's exclusive lock, so
// it must run before the StateDB opens it.
func stateWALRange(cfg *config.GigaStorageConfig) (first uint64, last uint64, err error) {
	stored, first, last, err := statewal.GetRange(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir))
	if err != nil {
		return 0, 0, fmt.Errorf("read the state WAL's stored range: %w", err)
	}
	if !stored {
		return 0, 0, nil
	}
	return first, last, nil
}

// assertInvariants errors when the stores on disk break an invariant startup relies on: a disabled receipt store or
// historical state DB that still holds data, a store ahead of the block store, or a state WAL the live state DB
// cannot replay from.
func assertInvariants(
	cfg *config.GigaStorageConfig,
	// The newest block the block store holds, or 0 when it holds none.
	blockStoreHead uint64,
	heads storeHeads,
) error {
	if err := requireNoReceiptStoreWhileDisabled(cfg); err != nil {
		return fmt.Errorf("check the disabled receipt store: %w", err)
	}
	if err := requireNoHistoricalStateDBWhileDisabled(cfg); err != nil {
		return fmt.Errorf("check the disabled historical state DB: %w", err)
	}
	err := requireNoStoreAheadOfTheBlockStore(blockStoreHead, heads, cfg.AppHashBuilderConfig.GigaActivationHeight)
	if err != nil {
		return fmt.Errorf("check the stores against the block store: %w", err)
	}
	if err := requireStateWALReplayableFromTheLiveStateDB(heads); err != nil {
		return fmt.Errorf("check the state WAL against the live state DB: %w", err)
	}
	return nil
}

// requireNoReceiptStoreWhileDisabled refuses to run with receipts disabled while a receipt store with
// history sits on disk. Blocks finalized meanwhile would leave a hole the store refuses to write across,
// and re-enabling it would then converge every store on its stale head.
func requireNoReceiptStoreWhileDisabled(cfg *config.GigaStorageConfig) error {
	if cfg.ReceiptDBConfig.Enable {
		return nil
	}
	head, err := littidx.GetLatestBlock(cfg.ReceiptDBConfig)
	if err != nil {
		return fmt.Errorf("read disabled receipt store head: %w", err)
	}
	if head == 0 {
		return nil
	}
	return fmt.Errorf("%w: head %d at %s; move that directory away to run without receipts, or enable them again",
		ErrReceiptStoreDisabledWithHistory, head, cfg.ReceiptDBConfig.DBDirectory)
}

// requireNoHistoricalStateDBWhileDisabled refuses to run with the historical state DB disabled while its
// directory holds anything. Re-enabling it would converge every store on its stale head.
func requireNoHistoricalStateDBWhileDisabled(cfg *config.GigaStorageConfig) error {
	if cfg.SSConfig.Enable {
		return nil
	}
	dir := cfg.SSConfig.EVMDBDirectory
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read disabled historical state DB directory: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}
	return fmt.Errorf("historical state DB is disabled but %s is not empty; move that directory away to run "+
		"without it, or enable it again", dir)
}

// requireNoStoreAheadOfTheBlockStore errors if any store holds a block the block store does not. An empty block
// store passes, since a home bootstrapped from copied state or restored from a snapshot starts with one.
//
// The block store is fsynced, and every other store is written asynchronously behind it, so a store ahead of it
// means that guarantee broke. Startup refuses rather than act on it.
func requireNoStoreAheadOfTheBlockStore(
	// The newest block the block store holds, or 0 when it holds none.
	blockStoreHead uint64,
	heads storeHeads,
	// The first block whose app hash the vault stores.
	gigaActivationHeight uint64,
) error {
	if blockStoreHead == 0 {
		return nil
	}
	ahead := func(store string, head uint64) error {
		return fmt.Errorf("the %s holds block %d, but the block store ends at block %d. No store can be ahead "+
			"of the block store, which is durable before any other store is written; investigate before "+
			"restarting", store, head, blockStoreHead)
	}
	if heads.stateWAL > blockStoreHead {
		return ahead("state WAL", heads.stateWAL)
	}
	if heads.historicalStateDB > blockStoreHead {
		return ahead("historical state DB", heads.historicalStateDB)
	}
	if heads.receiptStore > blockStoreHead {
		return ahead("receipt store", heads.receiptStore)
	}
	// An empty vault reports the block before the giga activation height, which is not a record it holds.
	if heads.appHashVault >= gigaActivationHeight && heads.appHashVault > blockStoreHead {
		return ahead("app hash vault", heads.appHashVault)
	}
	return nil
}

// requireStateWALReplayableFromTheLiveStateDB errors when the state WAL holds blocks but starts above the block
// after the live state DB's version, so the blocks between them are missing.
func requireStateWALReplayableFromTheLiveStateDB(heads storeHeads) error {
	if heads.stateWAL == 0 || heads.stateWALFirst <= heads.liveStateDB+1 {
		return nil
	}
	return fmt.Errorf("the live state DB stands at block %d, but the state WAL starts at block %d, so blocks "+
		"%d-%d are missing and the live state DB cannot replay the WAL", heads.liveStateDB, heads.stateWALFirst,
		heads.liveStateDB+1, heads.stateWALFirst-1)
}

// recoveryTarget returns the height the stores align on: the lowest of the live state's reachable height, the
// app hash vault's head, and the heads of the historical state DB and receipt store that hold blocks.
func recoveryTarget(heads storeHeads) uint64 {

	heights := make([]uint64, 0)

	// The live state and hash vault must have data for the target block number.
	heights = append(heights, heads.liveStateReachableHeight())
	heights = append(heights, heads.appHashVault)

	// The remaining stores can legally be empty at any height, and so we can ignore them when deciding
	// the recovery target when they are empty.
	for _, head := range []uint64{heads.historicalStateDB, heads.receiptStore} {
		if head > 0 {
			heights = append(heights, head)
		}
	}
	return slices.Min(heights)
}

// liveStateReachableHeight returns the highest block the live state can stand at: the state WAL's head, or the
// live state DB's version when the WAL holds no blocks to replay.
func (h storeHeads) liveStateReachableHeight() uint64 {
	if h.stateWAL > 0 {
		// The live state DB can replay up to the WAL's head: requireStateWALReplayableFromTheLiveStateDB()
		// already refused a WAL that starts above the block after the live state DB's version.
		return h.stateWAL
	}
	return h.liveStateDB
}

// alignStores brings the state WAL, the live state DB, the historical state DB and the receipt store onto target,
// and returns with every store closed. Some roll back to target and others replay up to it.
//
// The block store and the app hash vault are not touched: the vault keeps its app hashes above target for
// auditing. An empty receipt store or historical state DB stays empty. Running it again with the same target is a
// no-op, and alignment that failed or was interrupted completes when run again.
func alignStores(ctx context.Context, cfg *config.GigaStorageConfig, target uint64) error {
	logger.Info("Aligning the stores on the recovery target", "target", target)
	timer := dbutils.NewLoggingPhaseTimer(logger)

	timer.SetPhase("live_state_db")
	if err := flatkv.RollBackLiveStateDB(cfg.FlatKVConfig, target); err != nil {
		return fmt.Errorf("roll back the live state DB: %w", err)
	}
	if cfg.SSConfig.Enable {
		timer.SetPhase("historical_state_db")
		if err := undo.DiscardStateAbove(cfg.SSConfig.EVMDBDirectory, cfg.SSConfig, target); err != nil {
			return fmt.Errorf("roll back the historical state DB: %w", err)
		}
	}
	timer.SetPhase("state_wal")
	if err := statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), target); err != nil {
		return fmt.Errorf("roll back the state WAL: %w", err)
	}
	if cfg.ReceiptDBConfig.Enable {
		timer.SetPhase("receipt_store")
		if err := littidx.PruneAfter(cfg.ReceiptDBConfig, target); err != nil {
			return fmt.Errorf("roll back the receipt store: %w", err)
		}
	}
	timer.Log("Aligned the stores", "target", target)
	return nil
}

// openReceiptStore opens the receipt store, returning nil when receipts are disabled.
func openReceiptStore(cfg *config.GigaStorageConfig) (receiptdb.ReceiptStore, error) {
	if !cfg.ReceiptDBConfig.Enable {
		return nil, nil
	}
	// Giga has no legacy receipt KVStore.
	receiptDB, err := littidx.NewReceiptStore(cfg.ReceiptDBConfig, nil)
	if err != nil {
		return nil, fmt.Errorf("open the receipt store's files under %s: %w",
			cfg.ReceiptDBConfig.DBDirectory, err)
	}
	return receiptDB, nil
}

// openArchive opens the history archive, returning nil when no archive is configured.
func openArchive(ctx context.Context, cfg *config.GigaStorageConfig) (archive.ArchivalStore, error) {
	if cfg.ArchiveConfig == nil {
		return nil, nil
	}
	exporter, err := chstore.StartExporter(ctx, *cfg.ArchiveConfig)
	if err != nil {
		// Returned as a nil Store, not a Store holding a nil exporter.
		return nil, err
	}
	return exporter, nil
}
