package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/gigareceipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
)

// OpenDBWithRecovery opens every store and brings them onto one height after an unclean shutdown: the lowest
// head among the block store, the state WAL, the receipt store and the app hash vault. After recovery:
//  1. Every store is at or below the block store's height.
//  2. Every store other than the block store is on that height, unless it holds no history at all, in which
//     case it is left empty to fill forward from it.
//
// If the block store or the state WAL is empty, no store is moved.
func (m *GigaStorageManager) OpenDBWithRecovery(ctx context.Context) error {
	// Each stage is announced before it runs. Opening a store or reading its head loads that store's
	// index, which on a large one is minutes of work, and a stage that reported only on completion
	// would leave the longest part of a startup with nothing logged at all.
	logger.Info("Opening the block store")
	if err := m.openBlockStore(); err != nil {
		return err
	}
	if err := m.requireNoReceiptStoreWhileDisabled(); err != nil {
		return err
	}
	targetHeight, err := m.findTargetRecoveryHeight()
	if err != nil {
		return err
	}
	// After the vault head is read, which needs the vault closed, and before the state DB opens, which submits
	// to the BUD hasher.
	logger.Info("Opening the app hash builder")
	if err := m.openAppHashBuilder(ctx); err != nil {
		return err
	}
	if err := m.recoverStores(ctx, targetHeight); err != nil {
		return err
	}
	// The receipt store opens last because its rollback runs against its files: it is the one store
	// recovery reaches without opening, so opening it earlier would only be to close it again.
	logger.Info("Opening the receipt store")
	if err := m.openReceiptStore(); err != nil {
		return err
	}
	logger.Info("Completing the app hash builder setup")
	return m.completeAppHashBuilderSetup(ctx)
}

// recoverStores puts the receipt store, the state commit store and the EVM state store (when enabled)
// on target, cutting the state WAL back to it as well. The state stores are rolled back as they open,
// so this is what leaves the manager holding them.
//
// A target of 0 is no height to converge on, and every store is left as it was found: rolling back to
// it would drop every receipt the node holds along with every block in its WAL. This is the single
// guard for that, which is why the rollbacks below it carry none of their own.
//
// State goes first because it is the rollback that refuses: a target its snapshots and WAL cannot span
// leaves the node down for an operator to retry at a higher one, and receipts cut to the lower target
// would no longer be there to reach.
func (m *GigaStorageManager) recoverStores(ctx context.Context, target int64) error {
	if target == 0 {
		logger.Info("No height to converge on, opening the state DB where its files sit")
		return m.openStateDB(ctx)
	}
	logger.Info("Opening the state DB on the recovery target", "target", target)
	if err := m.openStateDBAt(ctx, target); err != nil {
		return err
	}
	return m.recoverReceipt(target)
}

// openBlockStore opens the block ledger consensus reads and writes.
func (m *GigaStorageManager) openBlockStore() error {
	blockDB, err := littblock.NewBlockDB(m.cfg.BlockDBConfig)
	if err != nil {
		return fmt.Errorf("open block db: %w", err)
	}
	blockStore, err := blockstore.New(blockDB)
	if err != nil {
		if closeErr := blockDB.Close(); closeErr != nil {
			logger.Error("failed to close the block db after the block store failed to open", "err", closeErr)
		}
		return fmt.Errorf("failed to open block store: %w", err)
	}
	m.blockStore = blockStore
	return nil
}

// openReceiptStore opens the receipt store, leaving it nil when receipts are disabled.
func (m *GigaStorageManager) openReceiptStore() error {
	if m.cfg.ReceiptDBConfig.Enable {
		// Giga has no legacy receipt KVStore.
		receiptDB, err := gigareceipt.NewReceiptStore(m.cfg.ReceiptDBConfig, nil)
		if err != nil {
			return fmt.Errorf("open receipt store: %w", err)
		}
		m.receiptDB = receiptDB
	}
	return nil
}

// findTargetRecoveryHeight returns the height recovery brings the stores onto, or 0 if no store is to be moved.
// It must run before the state DB, the receipt store and the app hash builder open.
func (m *GigaStorageManager) findTargetRecoveryHeight() (int64, error) {
	logger.Info("Reading a store head", "store", "block store")
	blockHeight, err := m.blockStore.GetLatestBlock()
	if err != nil {
		return 0, fmt.Errorf("read block store head: %w", err)
	}
	logger.Info("Reading a store head", "store", "state WAL")
	stateHeight, err := m.stateWALHead()
	if err != nil {
		return 0, err
	}
	var receiptHeight uint64
	if m.cfg.ReceiptDBConfig.Enable {
		logger.Info("Reading a store head", "store", "receipt store")
		receiptHeight, err = gigareceipt.GetLatestBlock(m.cfg.ReceiptDBConfig)
		if err != nil {
			return 0, fmt.Errorf("read receipt store head: %w", err)
		}
	}
	logger.Info("Reading a store head", "store", "app hash vault")
	appHashHeight, err := builder.GetLatestBlock(*m.cfg.AppHashBuilderConfig)
	if err != nil {
		return 0, fmt.Errorf("read app hash vault head: %w", err)
	}
	target, err := recoveryTarget(blockHeight, stateHeight, receiptHeight, appHashHeight)
	if err != nil {
		return 0, err
	}
	logger.Info("Read the store heads recovery converges on",
		"block_store", blockHeight,
		"state_wal", stateHeight,
		"receipt_store", receiptHeight,
		"app_hash_vault", appHashHeight,
		"target", target)
	return int64(target), nil //nolint:gosec // heights fit within int64
}

// ErrReceiptStoreDisabledWithHistory is returned when the receipt store is disabled while its
// directory still holds receipts.
var ErrReceiptStoreDisabledWithHistory = errors.New("receipt store is disabled but holds receipts")

// requireNoReceiptStoreWhileDisabled refuses to run with receipts disabled while a receipt store with
// history sits on disk. Blocks finalized meanwhile would leave a hole the store refuses to write across,
// and re-enabling it would then converge every store on its stale head.
func (m *GigaStorageManager) requireNoReceiptStoreWhileDisabled() error {
	if m.cfg.ReceiptDBConfig.Enable {
		return nil
	}
	head, err := gigareceipt.GetLatestBlock(m.cfg.ReceiptDBConfig)
	if err != nil {
		return fmt.Errorf("read disabled receipt store head: %w", err)
	}
	if head == 0 {
		return nil
	}
	return fmt.Errorf("%w: head %d at %s; move that directory away to run without receipts, or enable them again",
		ErrReceiptStoreDisabledWithHistory, head, m.cfg.ReceiptDBConfig.DBDirectory)
}

// stateWALHead returns the last block the state WAL holds, or 0 when it holds none.
//
// It reads the WAL directory rather than an open WAL, which takes that directory's exclusive lock, so
// it must run before the StateDB opens it.
func (m *GigaStorageManager) stateWALHead() (uint64, error) {
	stored, _, last, err := statewal.GetRange(flatkv.StateWALConfig(m.cfg.FlatKVConfig.DataDir))
	if err != nil {
		return 0, fmt.Errorf("read state WAL head: %w", err)
	}
	if !stored {
		return 0, nil
	}
	return last, nil
}

// recoveryTarget folds the store heads into the height they converge on: the lowest of them, with a
// receipt store that holds nothing left out rather than dragging the target down to 0. Receipts newly
// enabled on a node with history have nothing to disagree with, and start filling at the target.
//
// An empty block store or state WAL instead yields 0, which skips recovery. Neither is unambiguous the
// way an empty receipt store is: state whose WAL was pruned away behind a snapshot still exists with an
// empty WAL, and converging on a target derived from the other stores would discard it with no WAL left
// to replay it from.
//
// It errors when the app hash vault's head is 0 while the other stores converge above it, since no
// rollback reaches below block 1.
func recoveryTarget(
	blockHeight uint64,
	stateHeight uint64,
	receiptHeight uint64,
	// The newest block whose app hash the vault holds, or the block before the giga activation height when
	// it holds none.
	appHashHeight uint64,
) (uint64, error) {
	if blockHeight == 0 || stateHeight == 0 {
		return 0, nil
	}
	target := min(blockHeight, stateHeight)
	if receiptHeight > 0 {
		target = min(target, receiptHeight)
	}
	if appHashHeight == 0 {
		return 0, fmt.Errorf("the app hash vault holds no app hash, but the other stores converge on "+
			"block %d, and recovery cannot roll back to before block 1", target)
	}
	return min(target, appHashHeight), nil
}

// openStateDB opens the state commit store, the EVM state store (when enabled) and the state WAL,
// leaving them on the height the WAL holds.
func (m *GigaStorageManager) openStateDB(ctx context.Context) error {
	stateDB, err := giga.NewStateDB(ctx, m.cfg.FlatKVConfig, m.cfg.SSConfig, m.cfg.CheckpointConfig, m.budHasher)
	if err != nil {
		return err
	}
	m.stateDB = stateDB
	return nil
}

// openStateDBAt opens the same three stores on target, rolling them back to it first.
//
// The rollback is part of the open because cutting the state WAL's tail needs the WAL closed, so an
// already-open state DB would have to close and reopen it.
func (m *GigaStorageManager) openStateDBAt(ctx context.Context, target int64) error {
	stateDB, err := giga.NewStateDBWithRollback(
		ctx, m.cfg.FlatKVConfig, m.cfg.SSConfig, m.cfg.CheckpointConfig, m.budHasher, target)
	if err != nil {
		return err
	}
	m.stateDB = stateDB
	return nil
}

// recoverReceipt drops every receipt above target, working on the store's files rather than through an
// open store. A store already at or below target is left alone.
//
// It takes the locks an open receipt store holds, so it must run before openReceiptStore.
func (m *GigaStorageManager) recoverReceipt(target int64) error {
	if !m.cfg.ReceiptDBConfig.Enable {
		return nil
	}
	//nolint:gosec // recoverStores guards target > 0
	if err := gigareceipt.PruneAfter(m.cfg.ReceiptDBConfig, uint64(target)); err != nil {
		return fmt.Errorf("roll the receipt store back to %d: %w", target, err)
	}
	return nil
}
