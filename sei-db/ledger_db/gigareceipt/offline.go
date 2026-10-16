package gigareceipt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"

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

// PruneAfter rolls back the closed receipt store described by cfg so that litt and the index both end at
// the lowest of highestBlockToKeep, the litt head and the index head. A store already ending there is
// left unchanged.
//
// It returns an error without changing anything when litt would be rolled back below the retention floor,
// when that block is below the oldest receipt in litt while the index has a head, or when the index has a
// head but litt holds no receipts.
func PruneAfter(cfg dbconfig.ReceiptStoreConfig, highestBlockToKeep uint64) (err error) {
	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = filepath.Join(cfg.DBDirectory, littIndexDirName)
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		return fmt.Errorf("failed to open receipt log index: %w", err)
	}
	// WriteOptions{} does not fsync, so Close is where this function's writes are committed. A failed
	// close means they may not have landed, which must not read as a successful prune.
	defer func() { err = errors.Join(err, index.Close()) }()

	earliest, earliestFound, err := readMetaOffline(index, receiptEarliestVersionKey)
	if err != nil {
		return err
	}
	latest, latestFound, err := readMetaOffline(index, receiptLatestVersionKey)
	if err != nil {
		return err
	}
	stored, lowest, highest, err := GetRange(cfg)
	if err != nil {
		return fmt.Errorf("failed to read the receipt store's block range: %w", err)
	}
	if !stored {
		if latestFound {
			return fmt.Errorf("cannot roll back receipt store to block %d: the index reports a head at block "+
				"%d but no receipts are present on disk", highestBlockToKeep, latest)
		}
		return nil
	}

	cut := min(highestBlockToKeep, highest)
	if latestFound {
		cut = min(cut, latest)
	}
	littAboveCut := highest > cut
	indexAboveCut := latestFound && latest > cut
	if !littAboveCut && !indexAboveCut {
		return nil
	}
	// Rolling back only the index below the floor is allowed: litt lost those blocks, and replay rewrites them.
	if littAboveCut && earliestFound && cut < earliest {
		return fmt.Errorf("cannot roll back receipt store to block %d: it is below the retention floor "+
			"(earliest readable block %d); that data has already been pruned", cut, earliest)
	}
	// With no index head no receipt is readable, so deleting every receipt in litt loses nothing.
	if latestFound && cut < lowest {
		return fmt.Errorf("cannot roll back receipt store to block %d: the oldest receipt on disk is for "+
			"block %d, so no receipt would survive; delete %s to discard the store entirely",
			cut, lowest, cfg.DBDirectory)
	}

	if littAboveCut {
		if err := rollBackLitt(cfg, cut); err != nil {
			return err
		}
	}
	if indexAboveCut {
		return rollBackIndex(index, cut)
	}
	return nil
}

// rollBackLitt deletes every receipt in litt for a block above highestBlockToKeep. The store must be closed.
func rollBackLitt(cfg dbconfig.ReceiptStoreConfig, highestBlockToKeep uint64) error {
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
		return height <= highestBlockToKeep, nil
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
