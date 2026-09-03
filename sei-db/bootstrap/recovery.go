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
//  2. Every store other than the block store is on the same height.
//
// The target is the lowest of the block store, the state WAL, and the receipt store. A target of 0
// means a fresh node with nothing to converge on, and nothing is moved.
//
// The two halves of state and the WAL they share belong to the StateDB, which opens them where it
// finds them. recoverState is the step that puts them on the target and readies them for the block
// after it.
func (m *GigaStorageManager) OpenDBWithRecovery(ctx context.Context) error {
	if err := m.openBlockStore(); err != nil {
		return err
	}
	if err := m.openReceiptStore(); err != nil {
		return err
	}
	targetHeight, err := m.findTargetRecoveryHeight()
	if err != nil {
		return err
	}
	if err := m.recoverReceipt(targetHeight); err != nil {
		return err
	}
	if err := m.openStateDB(ctx); err != nil {
		return err
	}
	return m.recoverState(targetHeight)
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

// findTargetRecoveryHeight returns the height every store is recovered to: the lowest head among the
// block store, the state WAL and the receipt store, receipts being skipped when disabled. It returns 0
// when there is no height to converge on.
func (m *GigaStorageManager) findTargetRecoveryHeight() (int64, error) {
	blockHeight, err := m.blockStore.GetLatestBlock()
	if err != nil {
		return 0, fmt.Errorf("read block store head: %w", err)
	}
	stored, _, last, err := statewal.GetRange(flatkv.StateWALPath(m.cfg.FlatKVConfig.DataDir))
	if err != nil {
		return 0, fmt.Errorf("read state WAL head: %w", err)
	}
	var stateHeight uint64
	if stored {
		stateHeight = last
	}
	if blockHeight == 0 || stateHeight == 0 {
		return 0, nil
	}
	target := min(blockHeight, stateHeight)
	if m.receiptDB != nil {
		receiptHeight, err := m.receiptDB.GetLatestBlock()
		if err != nil {
			return 0, fmt.Errorf("read receipt store head: %w", err)
		}
		target = min(target, receiptHeight)
	}
	return int64(target), nil //nolint:gosec // block heights fit within int64
}

// openStateDB opens the two halves of state and the WAL they share, converged on the WAL's own head.
func (m *GigaStorageManager) openStateDB(ctx context.Context) error {
	stateDB, err := giga.NewStateDB(ctx, m.cfg.FlatKVConfig, m.cfg.SSConfig, m.cfg.CheckpointConfig)
	if err != nil {
		return err
	}
	m.stateDB = stateDB
	return nil
}

// recoverState puts the two halves of state and their WAL on target, which the StateDB opened at or
// above. It is the step that performs state recovery.
//
// A target of 0 is no height to converge on and is left alone: rolling back to it would discard every
// block the WAL holds, and the stores that produced a target of 0 have no history to disagree with.
func (m *GigaStorageManager) recoverState(target int64) error {
	if target == 0 {
		return nil
	}
	return m.stateDB.RollbackTo(target)
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
