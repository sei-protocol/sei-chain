package bootstrap

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
)

// OpenDBWithRecovery opens every store and brings them onto one height after an unclean shutdown.
// After recovery:
//  1. Every store is at or below the block store's height.
//  2. Every store other than the block store is on the same height, unless it holds no history at all,
//     in which case it is left empty to fill forward from that height.
//
// The target is the lowest head among the block store, the state WAL and the receipt store. A target of
// 0 means there is no height to converge on and nothing is moved.
func (m *GigaStorageManager) OpenDBWithRecovery(ctx context.Context) error {
	if err := m.openBlockStore(); err != nil {
		return err
	}
	if err := m.openReceiptStore(); err != nil {
		return err
	}
	// The target is read before the StateDB opens, because reading the state WAL's head offline takes
	// the directory lock an open WAL holds.
	targetHeight, err := m.findTargetRecoveryHeight()
	if err != nil {
		return err
	}
	if err := m.openStateDB(ctx); err != nil {
		return err
	}
	return m.recoverStores(targetHeight)
}

// recoverStores aligns the block height of the receipt store, the live state (SC) and the historical
// state (SS, if enabled) on target, cutting the state WAL back to target as it rolls state back.
//
// A target of 0 is no height to converge on, and every store is left as it was found: rolling back to
// it would drop every receipt the node holds along with every block in its WAL. This is the single
// guard for that, which is why the two rollbacks below it carry none of their own.
func (m *GigaStorageManager) recoverStores(target int64) error {
	if target == 0 {
		return nil
	}
	if err := m.recoverReceipt(target); err != nil {
		return err
	}
	return m.stateDB.RollbackTo(target)
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
		receiptDB, err := receipt.NewReceiptStore(m.cfg.ReceiptDBConfig, nil)
		if err != nil {
			return fmt.Errorf("open receipt store: %w", err)
		}
		m.receiptDB = receiptDB
	}
	return nil
}

// findTargetRecoveryHeight returns the height every store is recovered to, read from the heads of the
// block store, the state WAL and the receipt store. A disabled receipt store reads as 0, which is the
// same as an empty one: no opinion on the height.
func (m *GigaStorageManager) findTargetRecoveryHeight() (int64, error) {
	blockHeight, err := m.blockStore.GetLatestBlock()
	if err != nil {
		return 0, fmt.Errorf("read block store head: %w", err)
	}
	stateHeight, err := m.stateWALHead()
	if err != nil {
		return 0, err
	}
	var receiptHeight uint64
	if m.receiptDB != nil {
		receiptHeight, err = m.receiptDB.GetLatestBlock()
		if err != nil {
			return 0, fmt.Errorf("read receipt store head: %w", err)
		}
	}
	return int64(recoveryTarget(blockHeight, stateHeight, receiptHeight)), nil //nolint:gosec // heights fit within int64
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
func recoveryTarget(blockHeight, stateHeight, receiptHeight uint64) uint64 {
	if blockHeight == 0 || stateHeight == 0 {
		return 0
	}
	target := min(blockHeight, stateHeight)
	if receiptHeight > 0 {
		target = min(target, receiptHeight)
	}
	return target
}

// openStateDB opens the live state (SC), the historical state (SS, if enabled) and the state WAL they
// share, where it finds them. recoverStores is what puts them on a height.
func (m *GigaStorageManager) openStateDB(ctx context.Context) error {
	stateDB, err := giga.NewStateDB(ctx, m.cfg.FlatKVConfig, m.cfg.SSConfig, m.cfg.CheckpointConfig)
	if err != nil {
		return err
	}
	m.stateDB = stateDB
	return nil
}

// recoverReceipt rolls the receipt store back to target. The store has to be closed for the rollback
// to rewrite it, so this closes it, rolls it back offline, and reopens it.
func (m *GigaStorageManager) recoverReceipt(target int64) error {
	if m.receiptDB == nil {
		return nil
	}
	head := m.receiptDB.LatestVersion()
	if head <= target {
		return nil
	}
	if err := m.receiptDB.Close(); err != nil {
		return fmt.Errorf("close receipt store before rolling it back to %d: %w", target, err)
	}
	m.receiptDB = nil

	if err := receipt.Rollback(m.cfg.ReceiptDBConfig, target); err != nil {
		return fmt.Errorf("roll the receipt store back to %d: %w", target, err)
	}
	db, err := receipt.NewReceiptStore(m.cfg.ReceiptDBConfig, nil)
	if err != nil {
		return fmt.Errorf("reopen receipt store rolled back to %d: %w", target, err)
	}
	m.receiptDB = db
	return nil
}
