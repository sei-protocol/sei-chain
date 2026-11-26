package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
)

// flushStores fully processes all work sent to anything the storage manager manages before the call.
func (m *GigaStorageManager) flushStores() error {
	ctx := context.Background()
	height := uint64(m.stateDB.LiveStateDB().Version()) //nolint:gosec // live state DB versions are never negative
	timer := utils.NewLoggingPhaseTimer(logger)

	// Producers are flushed before their consumers, so each component has received all of its work by the time
	// it is flushed.
	timer.SetPhase("state_wal")
	if err := m.stateDB.WAL().Flush(); err != nil {
		return fmt.Errorf("flush the state WAL: %w", err)
	}
	if historicalStateDB := m.stateDB.HistoricalStateDB(); historicalStateDB != nil {
		timer.SetPhase("historical_state_db")
		historicalStateDB.WaitForPendingWrites()
	}
	timer.SetPhase("live_state_db")
	// Hands each block's state hash to the app hash builder. The live state DB writes its blocks to the data DBs
	// when it closes.
	if err := m.stateDB.LiveStateDB().FlushHashes(); err != nil {
		return fmt.Errorf("flush the live state DB's hashes: %w", err)
	}
	if m.receiptDB != nil {
		timer.SetPhase("receipt_store")
		if err := m.receiptDB.Flush(); err != nil {
			return fmt.Errorf("flush the receipt store: %w", err)
		}
	}
	timer.SetPhase("receipt_hasher")
	if err := m.receiptHasher.Flush(ctx); err != nil {
		return fmt.Errorf("flush the receipt hasher: %w", err)
	}
	timer.SetPhase("bud_hasher")
	if err := m.budHasher.Flush(ctx); err != nil {
		return fmt.Errorf("flush the BUD hasher: %w", err)
	}
	timer.SetPhase("app_hash_builder")
	if err := m.appHashBuilder.Flush(ctx); err != nil {
		return fmt.Errorf("flush the app hash builder: %w", err)
	}
	timer.Log("Flushed the stores for shutdown", "height", height)

	// Every store must now stand at the live state DB's version.
	if err := m.verifyStoreAlignment(height); err != nil {
		return fmt.Errorf("after flushing, not every store stands at block %d: %w", height, err)
	}
	return nil
}

// verifyStoreAlignment errors unless every open store stands at height. A state WAL, historical state DB or receipt
// store that holds no block also passes while height is the height the stores opened at.
func (m *GigaStorageManager) verifyStoreAlignment(height uint64) error {
	// A home restored from a snapshot opens with these stores empty, which recovery accepts at any height, and they
	// stay empty until the node commits a block.
	standsAtHeight := func(head uint64) bool {
		return head == height || (head == 0 && height == m.openHeight)
	}
	walPresent, _, walHead, err := m.stateDB.WAL().GetStoredRange()
	if err != nil {
		return fmt.Errorf("read the state WAL's range: %w", err)
	}
	if !walPresent {
		walHead = 0
	}
	if !standsAtHeight(walHead) {
		return fmt.Errorf("the state WAL stands at block %d", walHead)
	}
	if historicalStateDB := m.stateDB.HistoricalStateDB(); historicalStateDB != nil {
		head, err := historicalStateDB.GetLatestBlock()
		if err != nil {
			return fmt.Errorf("read the historical state DB's head: %w", err)
		}
		if !standsAtHeight(head) {
			return fmt.Errorf("the historical state DB stands at block %d", head)
		}
	}
	if m.receiptDB != nil {
		head := uint64(m.receiptDB.LatestVersion()) //nolint:gosec // heights are never negative
		if !standsAtHeight(head) {
			return fmt.Errorf("the receipt store stands at block %d", head)
		}
	}
	_, vaultHead, vaultPresent, err := m.appHashBuilder.Bounds()
	if err != nil {
		return fmt.Errorf("read the app hash vault's range: %w", err)
	}
	if vaultPresent && vaultHead != height {
		return fmt.Errorf("the app hash vault stands at block %d", vaultHead)
	}
	if !vaultPresent && height >= m.cfg.AppHashBuilderConfig.GigaActivationHeight {
		return fmt.Errorf("the app hash vault holds no app hash")
	}
	logger.Info("Every store stands at the same block", "height", height)
	return nil
}

// removeRunningMarker removes the running marker, recording a clean shutdown.
func (m *GigaStorageManager) removeRunningMarker() error {
	if m.cfg.RunningMarkerPath == "" {
		return nil
	}
	if err := os.Remove(m.cfg.RunningMarkerPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the running marker %s: %w", m.cfg.RunningMarkerPath, err)
	}
	logger.Info("Recorded a clean shutdown", "running_marker", m.cfg.RunningMarkerPath)
	return nil
}

// logPreviousShutdown logs whether the previous node left the running marker behind, i.e. shut down uncleanly.
func logPreviousShutdown(markerPath string) error {
	if markerPath == "" {
		return nil
	}
	_, err := os.Stat(markerPath)
	switch {
	case err == nil:
		logger.Error("The previous shutdown was not clean; recovery may roll back stores and replay blocks",
			"running_marker", markerPath)
	case errors.Is(err, fs.ErrNotExist):
		logger.Info("No running marker found; the previous shutdown, if any, was clean",
			"running_marker", markerPath)
	default:
		return fmt.Errorf("check for the running marker %s: %w", markerPath, err)
	}
	return nil
}

// createRunningMarker creates the running marker.
func createRunningMarker(markerPath string) error {
	if markerPath == "" {
		return nil
	}
	if err := os.WriteFile(markerPath, nil, 0o600); err != nil {
		return fmt.Errorf("create the running marker %s: %w", markerPath, err)
	}
	return nil
}
