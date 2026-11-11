package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/gigareceipt"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ErrReceiptStoreDisabledWithHistory is returned when the receipt store is disabled while its
// directory still holds receipts.
var ErrReceiptStoreDisabledWithHistory = errors.New("receipt store is disabled but holds receipts")

// storeHeads holds the newest block each store that recovery aligns holds, read from their files before they open.
type storeHeads struct {
	// The newest block the state WAL holds, or 0 when it holds none.
	stateWAL uint64

	// The newest block the historical state DB holds, None when it holds none or is disabled.
	historicalStateDB utils.Option[uint64]

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
	heads.stateWAL, err = stateWALHead(cfg)
	if err != nil {
		return storeHeads{}, fmt.Errorf("read state WAL head: %w", err)
	}
	heads.historicalStateDB = utils.None[uint64]()
	if cfg.SSConfig.Enable {
		logger.Info("Reading a store head", "store", "historical state DB")
		present, head, err := undo.GetLatestBlock(cfg.SSConfig.EVMDBDirectory)
		if err != nil {
			return storeHeads{}, fmt.Errorf("read historical state DB head: %w", err)
		}
		if present {
			heads.historicalStateDB = utils.Some(head)
		}
	}
	if cfg.ReceiptDBConfig.Enable {
		logger.Info("Reading a store head", "store", "receipt store")
		heads.receiptStore, err = gigareceipt.GetLatestBlock(cfg.ReceiptDBConfig)
		if err != nil {
			return storeHeads{}, fmt.Errorf("read receipt store head: %w", err)
		}
	}
	logger.Info("Reading a store head", "store", "app hash vault")
	heads.appHashVault, err = builder.GetLatestBlock(*cfg.AppHashBuilderConfig)
	if err != nil {
		return storeHeads{}, fmt.Errorf("read app hash vault head: %w", err)
	}
	var historicalStateLogged any = "none"
	if head, present := heads.historicalStateDB.Get(); present {
		historicalStateLogged = head
	}
	logger.Info("Read the store heads",
		"state_wal", heads.stateWAL,
		"historical_state_db", historicalStateLogged,
		"receipt_store", heads.receiptStore,
		"app_hash_vault", heads.appHashVault)
	return heads, nil
}

// stateWALHead returns the last block the state WAL holds, or 0 when it holds none.
//
// It reads the WAL directory rather than an open WAL, which takes that directory's exclusive lock, so
// it must run before the StateDB opens it.
func stateWALHead(cfg *config.GigaStorageConfig) (uint64, error) {
	stored, _, last, err := statewal.GetRange(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir))
	if err != nil {
		return 0, fmt.Errorf("read the state WAL's stored range: %w", err)
	}
	if !stored {
		return 0, nil
	}
	return last, nil
}

// assertInvariants errors when the stores on disk break an invariant startup relies on: a disabled receipt store or
// historical state DB that still holds data, a store ahead of the block store, or state holding blocks beside a
// vault holding no app hash.
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
	if err := requireAppHashesForStateBlocks(heads); err != nil {
		return fmt.Errorf("check the app hash vault against the state: %w", err)
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
	head, err := gigareceipt.GetLatestBlock(cfg.ReceiptDBConfig)
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
	if head, present := heads.historicalStateDB.Get(); present && head > blockStoreHead {
		return ahead("historical state DB", head)
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

// requireAppHashesForStateBlocks errors when the state WAL holds blocks while the app hash vault holds no app hash.
// The stores would then align on block 0, which no rollback reaches.
func requireAppHashesForStateBlocks(heads storeHeads) error {
	if heads.stateWAL == 0 || heads.appHashVault > 0 {
		return nil
	}
	return fmt.Errorf("the app hash vault holds no app hash, but the state WAL holds blocks through %d, and "+
		"recovery cannot roll back to before block 1", heads.stateWAL)
}

// recoveryTarget folds the store heads into the height the stores align on: the lowest of them, with a
// historical state DB or receipt store that holds nothing left out rather than dragging the target down to
// 0. A store newly enabled on a node with history has nothing to disagree with, and starts filling at the
// target.
//
// An empty state WAL instead yields 0, which leaves every store where it is. It is not unambiguous the way an
// empty receipt store is: state whose WAL was pruned away behind a snapshot still exists with an empty WAL, and
// aligning on a target derived from the other stores would discard it with no WAL left to replay it from.
func recoveryTarget(heads storeHeads) uint64 {
	if heads.stateWAL == 0 {
		return 0
	}
	target := heads.stateWAL
	if head, present := heads.historicalStateDB.Get(); present {
		target = min(target, head)
	}
	if heads.receiptStore > 0 {
		target = min(target, heads.receiptStore)
	}
	return min(target, heads.appHashVault)
}

// alignStores brings the state WAL, the live state DB, the historical state DB and the receipt store onto target,
// and returns with every store closed. Some roll back to target and others replay up to it. A target of 0 leaves
// every store where it is.
//
// The block store and the app hash vault are not touched: the vault keeps its app hashes above target for
// auditing. An empty receipt store or historical state DB stays empty. Running it again with the same target is a
// no-op, and alignment that failed or was interrupted completes when run again.
func alignStores(ctx context.Context, cfg *config.GigaStorageConfig, target uint64) error {
	if target == 0 {
		// Rolling back to 0 would drop every receipt the node holds along with every block in its WAL. This is
		// the single guard for that, which is why the rollbacks below carry none of their own.
		logger.Info("No height to align the stores on, leaving them where their files sit")
		return nil
	}
	logger.Info("Aligning the stores on the recovery target", "target", target)

	if err := flatkv.RollBackLiveStateDB(ctx, cfg.FlatKVConfig, cfg.CheckpointConfig, target); err != nil {
		return fmt.Errorf("roll back the live state DB: %w", err)
	}
	if cfg.SSConfig.Enable {
		if err := undo.DiscardStateAbove(cfg.SSConfig.EVMDBDirectory, cfg.SSConfig, target); err != nil {
			return fmt.Errorf("roll back the historical state DB: %w", err)
		}
	}
	if err := statewal.PruneAfter(flatkv.StateWALConfig(cfg.FlatKVConfig.DataDir), target); err != nil {
		return fmt.Errorf("roll back the state WAL: %w", err)
	}
	if cfg.ReceiptDBConfig.Enable {
		if err := gigareceipt.PruneAfter(cfg.ReceiptDBConfig, target); err != nil {
			return fmt.Errorf("roll back the receipt store: %w", err)
		}
	}
	return nil
}

// openReceiptStore opens the receipt store, returning nil when receipts are disabled.
func openReceiptStore(cfg *config.GigaStorageConfig) (receipt.ReceiptStore, error) {
	if !cfg.ReceiptDBConfig.Enable {
		return nil, nil
	}
	// Giga has no legacy receipt KVStore.
	receiptDB, err := gigareceipt.NewReceiptStore(cfg.ReceiptDBConfig, nil)
	if err != nil {
		return nil, fmt.Errorf("open the receipt store's files under %s: %w",
			cfg.ReceiptDBConfig.DBDirectory, err)
	}
	return receiptDB, nil
}
