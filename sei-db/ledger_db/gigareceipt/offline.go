package gigareceipt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"os"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/offline"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// GetLatestBlock returns the lower of the litt head and the index head of the receipt store described by
// cfg, without opening the store. It returns 0 when the index has no head, and the index head when litt
// holds no receipts.
func GetLatestBlock(cfg dbconfig.ReceiptStoreConfig) (uint64, error) {
	indexDir := filepath.Join(cfg.DBDirectory, littIndexDirName)
	// Opening the index would create it. A store that was never written has no head to read, and
	// creating its directories here would leave a half-made store behind on a failed startup.
	if _, statErr := os.Stat(indexDir); os.IsNotExist(statErr) {
		return 0, nil
	}
	indexHead, err := readIndexHead(indexDir)
	if err != nil || indexHead == 0 {
		return 0, err
	}

	littConfig, err := litt.DefaultConfig(filepath.Join(cfg.DBDirectory, littValuesDirName))
	if err != nil {
		return 0, fmt.Errorf("failed to build littdb config: %w", err)
	}
	littHead, ok, err := newestOrOldestBlock(littConfig, true)
	if err != nil {
		return 0, err
	}
	if !ok {
		return indexHead, nil
	}
	return min(indexHead, littHead), nil
}

// readIndexHead returns the head recorded in the index at indexDir, or 0 if none is recorded.
func readIndexHead(indexDir string) (head uint64, err error) {
	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = indexDir
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		return 0, fmt.Errorf("failed to open receipt log index: %w", err)
	}
	defer func() { err = errors.Join(err, index.Close()) }()

	head, _, err = readMetaOffline(index, receiptLatestVersionKey)
	return head, err
}

// GetRange reports the lowest and highest block heights present on disk for the receipt store described
// by cfg, without opening the store. ok is false if no receipts are present.
func GetRange(cfg dbconfig.ReceiptStoreConfig) (ok bool, lowestBlock uint64, highestBlock uint64, err error) {
	littConfig, err := litt.DefaultConfig(filepath.Join(cfg.DBDirectory, littValuesDirName))
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to build littdb config: %w", err)
	}

	highestBlock, ok, err = newestOrOldestBlock(littConfig, true)
	if err != nil || !ok {
		return false, 0, 0, err
	}
	lowestBlock, ok, err = newestOrOldestBlock(littConfig, false)
	if err != nil || !ok {
		return false, 0, 0, err
	}
	return true, lowestBlock, highestBlock, nil
}

// newestOrOldestBlock returns the block of the newest receipt part in litt when newest is true, or of the
// oldest when it is false. ok is false if litt holds no receipts.
func newestOrOldestBlock(littConfig *litt.Config, newest bool) (block uint64, ok bool, err error) {
	it, err := offline.NewIterator(littConfig, littReceiptTableName, newest)
	if err != nil {
		return 0, false, fmt.Errorf("failed to open receipt iterator: %w", err)
	}
	block, ok, err = firstPrimaryKeyHeight(it)
	if closeErr := it.Close(); closeErr != nil && err == nil {
		err = fmt.Errorf("failed to close receipt iterator: %w", closeErr)
	}
	if err != nil {
		return 0, false, err
	}
	return block, ok, nil
}

// PruneAfter rolls back the closed receipt store described by cfg so that it ends at the lower of
// highestBlockToKeep and its own head, lowering its retention floor to that block when the floor is above it.
// A store already ending there, or one that was never written, is left unchanged. One that failed or was
// interrupted completes when run again.
func PruneAfter(cfg dbconfig.ReceiptStoreConfig, highestBlockToKeep uint64) (err error) {
	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = filepath.Join(cfg.DBDirectory, littIndexDirName)
	if _, statErr := os.Stat(indexCfg.DataDir); os.IsNotExist(statErr) {
		// Opening the index would create it, leaving a half-made store behind.
		return nil
	}
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		return fmt.Errorf("failed to open receipt log index: %w", err)
	}
	// WriteOptions{} does not fsync, so Close is where this function's writes are committed. A failed
	// close means they may not have landed, which must not read as a successful prune.
	defer func() { err = errors.Join(err, index.Close()) }()

	retentionFloor, _, err := readMetaOffline(index, receiptEarliestVersionKey)
	if err != nil {
		return err
	}
	latestVersion, latestVersionRecorded, err := readMetaOffline(index, receiptLatestVersionKey)
	if err != nil {
		return err
	}
	hasReceipts, oldestReceiptBlock, newestReceiptBlock, err := GetRange(cfg)
	if err != nil {
		return fmt.Errorf("failed to read the receipt store's block range: %w", err)
	}
	// The store ends at the lowest of the target, its latest version and its newest receipt.
	heights := []uint64{highestBlockToKeep}
	if latestVersionRecorded {
		heights = append(heights, latestVersion)
	}
	if hasReceipts {
		heights = append(heights, newestReceiptBlock)
	}
	newLatestVersion := slices.Min(heights)

	if !hasReceipts && latestVersionRecorded && newLatestVersion > retentionFloor {
		// Judged on the store as rolled back: one interrupted after discarding its receipts still records its
		// old latest version, and a new latest version at or below the retention floor promises no receipt.
		return fmt.Errorf("cannot roll back receipt store to block %d: its latest version is block %d but no "+
			"receipts are present on disk", highestBlockToKeep, latestVersion)
	}

	discardReceipts := newestReceiptBlock > newLatestVersion
	rewindIndex := latestVersion > newLatestVersion
	lowerRetentionFloor := retentionFloor > newLatestVersion
	if !discardReceipts && !rewindIndex && !lowerRetentionFloor {
		return nil
	}
	// With no latest version no receipt is queryable, so discarding every receipt loses nothing. A new latest
	// version below the retention floor, or at 0, keeps no receipt by design.
	if newLatestVersion > 0 && latestVersionRecorded && !lowerRetentionFloor &&
		newLatestVersion < oldestReceiptBlock {
		return fmt.Errorf("cannot roll back receipt store to block %d: the oldest receipt on disk is for "+
			"block %d, so no receipt would survive; delete %s to discard the store entirely",
			newLatestVersion, oldestReceiptBlock, cfg.DBDirectory)
	}

	if discardReceipts {
		if err := rollBackLitt(cfg, retentionFloor, newLatestVersion); err != nil {
			return err
		}
	}
	if rewindIndex {
		if err := rollBackIndex(index, newLatestVersion); err != nil {
			return err
		}
	}
	if lowerRetentionFloor {
		// Last, so the floor never drops while pruned receipts it would expose are still on disk. The block
		// after the new latest version is written next, and a floor above it would hide that block and let it
		// be collected.
		newRetentionFloor := encodeBlockNumber(newLatestVersion)
		if err := index.Set(receiptEarliestVersionKey, newRetentionFloor, dbtypes.WriteOptions{}); err != nil {
			return fmt.Errorf("failed to lower the retention floor to block %d: %w", newLatestVersion, err)
		}
	}
	return nil
}

// rollBackLitt deletes every receipt in litt for a block above highestBlockToKeep, and every receipt when
// none at or above lowestBlockToKeep remains. The store must be closed.
func rollBackLitt(
	cfg dbconfig.ReceiptStoreConfig,
	// The retention floor. Receipts below it are pruned, so the rollback never lands on one.
	lowestBlockToKeep uint64,
	// The newest block whose receipts survive.
	highestBlockToKeep uint64,
) error {
	littConfig, err := litt.DefaultConfig(filepath.Join(cfg.DBDirectory, littValuesDirName))
	if err != nil {
		return fmt.Errorf("failed to build littdb config: %w", err)
	}
	filter := func(tableName string, key []byte, isPrimary bool) (bool, error) {
		if tableName != littReceiptTableName {
			// RollbackLittDB deletes any table its filter never matches, so returning false for a table
			// this filter cannot decode would destroy it rather than leave it alone.
			return false, fmt.Errorf("unexpected table %q under the receipt littdb", tableName)
		}
		if !isPrimary {
			// Secondary keys are tx-hash aliases and carry no height, so they are never rollback points.
			// Every group has a primary, so this never suppresses a whole table.
			return false, nil
		}
		height, err := decodePartKeyHeight(key)
		if err != nil {
			return false, err
		}
		return height >= lowestBlockToKeep && height <= highestBlockToKeep, nil
	}
	if err := offline.RollbackLittDB(littConfig, filter); err != nil {
		return fmt.Errorf("failed to roll back littdb receipts: %w", err)
	}
	return nil
}

// rollBackIndex deletes every tag key and block-stats entry above highestBlockToKeep and sets the index
// head to highestBlockToKeep.
func rollBackIndex(index dbtypes.KeyValueDB, highestBlockToKeep uint64) error {
	rd, ok := index.(rangeDeleter)
	if !ok {
		return fmt.Errorf("receipt index %T does not support range delete", index)
	}
	lower := littTagBlockKey(highestBlockToKeep + 1)
	upper := prefixSuccessor([]byte{littTagKeyPrefix})
	if err := rd.DeleteRange(lower, upper, dbtypes.WriteOptions{}); err != nil {
		return fmt.Errorf("failed to delete tag index entries above block %d: %w", highestBlockToKeep, err)
	}
	statsLower := blockStatsKey(highestBlockToKeep + 1)
	statsUpper := prefixSuccessor([]byte{blockStatsKeyPrefix})
	if err := rd.DeleteRange(statsLower, statsUpper, dbtypes.WriteOptions{}); err != nil {
		return fmt.Errorf("failed to delete block stats entries above block %d: %w", highestBlockToKeep, err)
	}
	newLatest := encodeBlockNumber(highestBlockToKeep)
	if err := index.Set(receiptLatestVersionKey, newLatest, dbtypes.WriteOptions{}); err != nil {
		return fmt.Errorf("failed to update latest block metadata: %w", err)
	}
	return nil
}

// firstPrimaryKeyHeight advances it to the first primary key found and decodes the block height from its
// littPartKey encoding. Secondary keys (tx-hash aliases) carry no height and are skipped. ok is false if
// the iterator has no primary keys at all.
func firstPrimaryKeyHeight(it litt.Iterator) (height uint64, ok bool, err error) {
	for {
		hasNext, err := it.Next()
		if err != nil {
			return 0, false, fmt.Errorf("failed to advance iterator: %w", err)
		}
		if !hasNext {
			return 0, false, nil
		}
		key, isPrimary, err := it.GetKey()
		if err != nil {
			return 0, false, fmt.Errorf("failed to read key: %w", err)
		}
		if !isPrimary {
			continue
		}
		height, err = decodePartKeyHeight(key)
		if err != nil {
			return 0, false, err
		}
		return height, true, nil
	}
}

// decodePartKeyHeight decodes the block height encoded in a receipts-table primary key (see littPartKey).
func decodePartKeyHeight(key []byte) (uint64, error) {
	if len(key) != blockNumLen+littPartCountLen {
		return 0, fmt.Errorf("unexpected primary receipt key length %d (want %d)",
			len(key), blockNumLen+littPartCountLen)
	}
	return binary.BigEndian.Uint64(key[:blockNumLen]), nil
}

// readMetaOffline reads a version-metadata key directly from the receipt index, without a live store.
// found is false only when the key is absent, which is the state of a store that has never recorded that
// version. A failed read, or a value of the wrong width, is an error: this metadata decides which of
// PruneAfter's refusals apply, so metadata that cannot be trusted has to stop the rollback rather than
// read as zero.
func readMetaOffline(index dbtypes.KeyValueDB, key []byte) (version uint64, found bool, err error) {
	val, err := index.Get(key)
	if errorutils.IsNotFound(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to read receipt store metadata key %q: %w", key, err)
	}
	if len(val) != blockNumLen {
		return 0, false, fmt.Errorf("receipt store metadata key %q holds %d bytes, want %d",
			key, len(val), blockNumLen)
	}
	return decodeBlockNumber(val), true, nil
}
