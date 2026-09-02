package bootstrap

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
)

// OpenDBWithRecovery opens every store onto one height: the lowest of the block store, the state
// WAL, and the receipt store. A target of 0 means a fresh node, and nothing is moved.
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
	if err := m.truncateStateWAL(targetHeight); err != nil {
		return err
	}
	if err := m.openStateWal(); err != nil {
		return err
	}
	if err := m.openSC(ctx, m.stateWAL); err != nil {
		return err
	}
	if err := m.recoverSC(targetHeight); err != nil {
		return err
	}
	if err := m.openSS(); err != nil {
		return err
	}
	if err := m.recoverSS(targetHeight); err != nil {
		return err
	}
	m.stateDB = giga.NewStateDB(m.stateWAL, m.sc, m.ss)
	return nil
}

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

func (m *GigaStorageManager) openReceiptStore() error {
	// Open ReceiptDB if enabled
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

func (m *GigaStorageManager) openStateWal() error {
	// Open StateWAL
	stateWAL, err := flatkv.OpenStateWAL(m.cfg.FlatKVConfig)
	if err != nil {
		return fmt.Errorf("open state WAL: %w", err)
	}
	m.stateWAL = stateWAL
	return nil
}

func (m *GigaStorageManager) openSC(ctx context.Context, stateWal statewal.StateWAL) error {
	// Open SC with external State WAL
	sc, err := flatkv.NewCommitStore(ctx, m.cfg.FlatKVConfig, stateWal)
	if err != nil {
		return fmt.Errorf("open state commit store: %w", err)
	}
	m.sc = sc
	return nil
}

func (m *GigaStorageManager) openSS() error {
	// Open SS if enabled
	if m.cfg.SSConfig.Enable {
		ss, err := evm.NewEVMStateStore(m.cfg.SSConfig.EVMDBDirectory, m.cfg.SSConfig)
		if err != nil {
			return fmt.Errorf("open EVM state store: %w", err)
		}
		m.ss = ss
		if err := m.ss.StartSnapshots(utils.GetStateStoreSnapshotsSiblingPath(ss.Dir()), m.cfg.SSConfig, nil); err != nil {
			return fmt.Errorf("start EVM state store snapshot manager: %w", err)
		}
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

// truncateStateWAL drops every state WAL block above target, so the first live write after startup
// is the block after target.
func (m *GigaStorageManager) truncateStateWAL(target int64) error {
	if err := statewal.PruneAfter(flatkv.StateWALPath(m.cfg.FlatKVConfig.DataDir), uint64(target)); err != nil { //nolint:gosec // target > 0}
		return fmt.Errorf("truncate state WAL to %d: %w", target, err)
	}
	return nil
}

// recoverSC loads the commit store from the truncated WAL, then rolls it back if latest height is above target.
func (m *GigaStorageManager) recoverSC(target int64) error {
	if err := m.sc.LoadLatest(); err != nil {
		return fmt.Errorf("load state commit store: %w", err)
	}
	if err := m.sc.CleanupOrphanedReadOnlyDirs(); err != nil {
		return fmt.Errorf("clean up orphaned state commit read-only dirs: %w", err)
	}
	if m.sc.Version() > target {
		if err := m.sc.Rollback(target); err != nil {
			return fmt.Errorf("recover state commit store: %w", err)
		}
	}
	return nil
}

// recoverSS brings the EVM state store onto target, replaying the WAL when it is behind and
// restoring a snapshot when it is ahead.
func (m *GigaStorageManager) recoverSS(target int64) error {
	if m.ss == nil {
		return nil
	}
	head := m.ss.GetLatestVersion()
	switch {
	case head < target:
		if err := m.ss.CatchUpFrom(m.stateWAL, target); err != nil {
			return fmt.Errorf("catch the EVM state store up from %d to %d: %w", head, target, err)
		}
	case head > target:
		if err := m.ss.RollbackTo(target, m.stateWAL); err != nil {
			return fmt.Errorf("roll the EVM state store back from %d to %d: %w", head, target, err)
		}
	}
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
