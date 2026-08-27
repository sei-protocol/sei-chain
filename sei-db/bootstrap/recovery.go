package bootstrap

import (
	"errors"
	"fmt"
	"math"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// ErrRollbackUnsupported reports that a store recovery has to rewind holds no rollback of its
// own.
//
// It is not a condition to route around here. Rewinding the stores that can and leaving the one
// that cannot ahead of them is the divergence recovery exists to close, so a node that hits this
// refuses to start and has to be rebuilt from a state sync.
var ErrRollbackUnsupported = errors.New("store has no rollback")

// HeightRollback is a store that can rewind itself to a committed height and keep committing
// from there, dropping everything above it.
type HeightRollback interface {
	Rollback(target int64) error
}

// StoreHeights is the height every managed store reported after opening itself.
type StoreHeights struct {
	Block    uint64
	SC       uint64
	SS       uint64
	StateWAL uint64

	// Receipt is meaningful only when ReceiptEnabled: a disabled receipt store has no height,
	// which is different from having height 0.
	Receipt        uint64
	ReceiptEnabled bool
}

// RecoverFromCrash brings the managed stores onto one height after an unclean shutdown, by
// rolling back whichever ones ran ahead of the rest.
//
// It executes no blocks. A store that is behind stays where it is and every other store comes
// down to it, so the node resumes from the highest height all of them can serve and re-executes
// the difference through normal block processing.
//
// Call it after NewGigaStorageManager and before serving anything. It is a no-op on a node that
// shut down cleanly.
func (m *GigaStorageManager) RecoverFromCrash() error {
	heights, err := m.readHeights()
	if err != nil {
		return err
	}
	plan, err := planRecovery(heights)
	if err != nil {
		return err
	}
	return m.executeRecovery(plan, heights)
}

// recoveryPlan names the height every store has to end at, and which of them has to be rolled
// back to reach it.
type recoveryPlan struct {
	target          uint64
	rollbackSC      bool
	rollbackSS      bool
	rollbackReceipt bool
}

// nothingToDo reports whether every store already agrees on the target.
func (p recoveryPlan) nothingToDo() bool {
	return !p.rollbackSC && !p.rollbackSS && !p.rollbackReceipt
}

// planRecovery derives the rollback set from the heights the stores opened at.
//
// The target is the lowest height SC, SS and the receipt store reached, because that is the
// highest height all of them can serve. Deriving it as one minimum rather than as a sequence of
// pairwise fixes is what makes recovery idempotent: a run interrupted partway through computes
// the same target on the next open, and rolling a store back to the height it already sits at is
// not attempted at all.
//
// The state WAL is not part of that minimum. SC restores from a snapshot and replays the WAL
// forward, so a WAL below the target still serves it, and SC's own rollback is what truncates
// the WAL above the target.
func planRecovery(heights StoreHeights) (recoveryPlan, error) {
	if err := requireBlockLedgerAhead(heights); err != nil {
		return recoveryPlan{}, err
	}

	target := min(heights.SC, heights.SS)
	if heights.ReceiptEnabled {
		target = min(target, heights.Receipt)
	}

	plan := recoveryPlan{
		target:          target,
		rollbackSC:      heights.SC > target,
		rollbackSS:      heights.SS > target,
		rollbackReceipt: heights.ReceiptEnabled && heights.Receipt > target,
	}
	if target == 0 && !plan.nothingToDo() {
		return recoveryPlan{}, fmt.Errorf(
			"cannot recover storage: one store has committed nothing while another is ahead of it (%+v); "+
				"rolling back to height 0 discards state rather than recovering it, so rebuild this node from a state sync",
			heights)
	}
	return plan, nil
}

// requireBlockLedgerAhead rejects a state or receipt height above the block ledger's head.
//
// Consensus stores a block before that block is executed, so every other store trails the
// ledger. One that is ahead holds data derived from a block this node does not have, and no
// rollback here reconstructs the missing block: only the ledger's own recovery, or a resync,
// can.
func requireBlockLedgerAhead(heights StoreHeights) error {
	trailing := []struct {
		name   string
		height uint64
	}{
		{"state commit", heights.SC},
		{"EVM state store", heights.SS},
		{"state WAL", heights.StateWAL},
	}
	if heights.ReceiptEnabled {
		trailing = append(trailing, struct {
			name   string
			height uint64
		}{"receipt store", heights.Receipt})
	}
	for _, store := range trailing {
		if store.height > heights.Block {
			return fmt.Errorf(
				"cannot recover storage: the %s is at height %d but the block ledger only reaches %d, "+
					"so the node holds data derived from a block it does not have",
				store.name, store.height, heights.Block)
		}
	}
	return nil
}

// executeRecovery performs the plan's rollbacks.
//
// SC and SS go first and in that order. They are one logical state store split in two, and the
// receipt store is only compared against a state height, so bringing state onto the target
// before touching receipts means a crash partway leaves the next run reading a state height that
// is already the target rather than a third distinct height.
func (m *GigaStorageManager) executeRecovery(plan recoveryPlan, heights StoreHeights) error {
	if plan.nothingToDo() {
		logger.Info("storage recovery: every store opened at the same height", "height", plan.target)
		return nil
	}

	logger.Info("storage recovery: rolling stores back onto a common height",
		"target", plan.target,
		"sc", heights.SC,
		"ss", heights.SS,
		"stateWAL", heights.StateWAL,
		"receipt", heights.Receipt,
		"receiptEnabled", heights.ReceiptEnabled,
		"block", heights.Block,
	)

	if plan.rollbackSC {
		if err := rollbackTo("state commit", m.sc, plan.target); err != nil {
			return err
		}
	}
	if plan.rollbackSS {
		if err := rollbackTo("EVM state store", m.ss, plan.target); err != nil {
			return err
		}
	}
	if plan.rollbackReceipt {
		if err := rollbackTo("receipt store", m.receiptDB, plan.target); err != nil {
			return err
		}
	}

	logger.Info("storage recovery complete", "height", plan.target)
	return nil
}

// rollbackTo rewinds one store, reporting ErrRollbackUnsupported when that store has no rollback
// to call.
func rollbackTo(name string, store any, target uint64) error {
	if target > math.MaxInt64 {
		return fmt.Errorf("cannot roll %s back to height %d: above the highest version a store can hold", name, target)
	}
	rollback, ok := store.(HeightRollback)
	if !ok {
		return fmt.Errorf("cannot roll %s back to height %d: %w (%T); rebuild this node from a state sync",
			name, target, ErrRollbackUnsupported, store)
	}
	if err := rollback.Rollback(int64(target)); err != nil {
		return fmt.Errorf("roll %s back to height %d: %w", name, target, err)
	}
	logger.Info("rolled a store back", "store", name, "target", target)
	return nil
}

// readHeights asks every opened store what height it recovered itself to.
func (m *GigaStorageManager) readHeights() (StoreHeights, error) {
	heights := StoreHeights{ReceiptEnabled: m.receiptDB != nil}

	blockHead, err := blockLedgerHead(m.blockDB)
	if err != nil {
		return heights, fmt.Errorf("read block ledger head: %w", err)
	}
	heights.Block = blockHead

	scVersion, err := m.sc.GetLatestVersion()
	if err != nil {
		return heights, fmt.Errorf("read state commit version: %w", err)
	}
	heights.SC = asHeight(scVersion)
	heights.SS = asHeight(m.ss.GetLatestVersion())

	stored, _, walHead, err := m.stateWAL.GetStoredRange()
	if err != nil {
		return heights, fmt.Errorf("read state WAL range: %w", err)
	}
	if stored {
		heights.StateWAL = walHead
	}

	if m.receiptDB != nil {
		heights.Receipt = asHeight(m.receiptDB.LatestVersion())
	}
	return heights, nil
}

// blockLedgerHead returns the highest block number the ledger holds, and 0 when it holds none.
//
// It walks the ledger newest-first and stops at the first block record. That scan is in write
// order rather than number order, so the first block it reaches is the highest-numbered one only
// because consensus stores blocks in ascending order — the same property the ledger's own
// watermark recovery relies on.
func blockLedgerHead(db blocktypes.BlockDB) (uint64, error) {
	it, err := db.Scan(true)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := it.Close(); closeErr != nil {
			logger.Error("failed to close the block ledger scan", "err", closeErr)
		}
	}()
	for {
		ok, err := it.Next()
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, nil
		}
		if it.Kind() == blocktypes.KindBlock {
			return it.Number(), nil
		}
	}
}

// asHeight reads a store version as a height, treating "nothing committed" — which stores spell
// as 0 or as a negative sentinel — as 0.
func asHeight(version int64) uint64 {
	if version <= 0 {
		return 0
	}
	return uint64(version)
}
