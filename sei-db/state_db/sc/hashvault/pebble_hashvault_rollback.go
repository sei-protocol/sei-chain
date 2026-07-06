package hashvault

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/cockroachdb/pebble/v2"
)

// HardRollbackPebbleHashVault deletes every recorded hash strictly above blockHeight from the
// on-disk vault rooted at config.DataDir and clears the prune boundary. This is a break-glass
// operator tool: after it returns, commits at any height are allowed until Prune is run again.
func HardRollbackPebbleHashVault(_ context.Context, config HashVaultConfig, blockHeight uint64) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid hashvault config: %w", err)
	}

	// Refuse if the data dir doesn't already exist. pebble.Open would otherwise silently create a
	// fresh empty DB at a typo'd path and report a "successful" no-op rollback, which is exactly
	// the kind of operator-error-eaten-by-tooling we want to avoid in a CLI tool.
	if _, err := os.Stat(config.DataDir); err != nil {
		return fmt.Errorf("hashvault data dir %q is not accessible: %w", config.DataDir, err)
	}

	db, err := pebble.Open(config.DataDir, &pebble.Options{})
	if err != nil {
		return fmt.Errorf("failed to open hashvault pebble db at %q: %w", config.DataDir, err)
	}
	defer func() { _ = db.Close() }()

	boundary, err := readPersistedBoundary(db)
	if err != nil {
		return err
	}

	if blockHeight < boundary {
		return wipeEntireStore(db, config.DataDir, blockHeight, boundary)
	}

	// Partial rollback uses DeleteRange(hashKey(blockHeight+1), ...). At math.MaxUint64 the +1
	// wraps to 0, so hashKey(0) becomes the range start and every hash entry is deleted.
	if blockHeight == math.MaxUint64 {
		return fmt.Errorf("cannot hard rollback above block %d: %w", blockHeight, ErrRollbackHeightOverflow)
	}

	return hardRollbackAbove(db, config.DataDir, blockHeight)
}

// hardRollbackAbove deletes hashes strictly above blockHeight and clears the prune boundary in one
// atomic batch.
func hardRollbackAbove(db *pebble.DB, dataDir string, blockHeight uint64) error {
	batch := db.NewBatch()
	defer func() { _ = batch.Close() }()
	if err := batch.DeleteRange(hashKey(blockHeight+1), hashKeyUpperBound(), nil); err != nil {
		return fmt.Errorf("failed to stage hard rollback above block %d: %w", blockHeight, err)
	}
	if err := batch.Delete(pruneBoundaryKey, nil); err != nil {
		return fmt.Errorf("failed to stage prune boundary clear during hard rollback: %w", err)
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to hard rollback above block %d: %w", blockHeight, err)
	}
	logger.Info("hashvault hard rollback completed",
		"dataDir", dataDir, "blockHeight", blockHeight)
	return nil
}

// readPersistedBoundary returns the on-disk prune boundary, or zero if none has ever been written.
// A malformed boundary record is logged and surfaced as ErrCorruption so the operator must
// investigate before proceeding.
func readPersistedBoundary(db *pebble.DB) (uint64, error) {
	raw, closer, err := db.Get(pruneBoundaryKey)
	if errors.Is(err, pebble.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read prune boundary: %w", err)
	}
	defer func() { _ = closer.Close() }()

	boundary, err := decodeBoundaryValue(raw)
	if err != nil {
		logger.Error("hashvault prune boundary is malformed; refusing rollback",
			"rawHex", hex.EncodeToString(raw), "err", err)
		return 0, err
	}
	return boundary, nil
}

// wipeEntireStore drops every hash entry and the prune boundary record in a single atomic Pebble
// batch, leaving the store indistinguishable from a freshly-initialized vault.
func wipeEntireStore(db *pebble.DB, dataDir string, target, boundary uint64) error {
	batch := db.NewBatch()
	defer func() { _ = batch.Close() }()
	if err := batch.DeleteRange(hashKey(0), hashKeyUpperBound(), nil); err != nil {
		return fmt.Errorf("failed to stage wipe of hash range: %w", err)
	}
	if err := batch.Delete(pruneBoundaryKey, nil); err != nil {
		return fmt.Errorf("failed to stage wipe of prune boundary: %w", err)
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("failed to wipe hashvault store: %w", err)
	}
	logger.Warn("hashvault rollback target is below prune boundary; wiped entire store",
		"dataDir", dataDir, "rollbackTarget", target, "pruneBoundary", boundary)
	return nil
}
