package receipt

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

// GetLatestBlock reports the head of the receipt store described by cfg — the highest block a lookup
// reaches — without opening the store. A store that has never recorded a head reads as 0.
//
// This is the head the store records rather than the highest block GetRange finds on disk. Bodies are
// written before the index entries that reach them, so a crash can leave bodies above the head that no
// lookup finds, and it is the head that says what the store serves.
//
// Only a littidx-backed store is supported; any other backend is refused rather than reported as empty.
func GetLatestBlock(cfg dbconfig.ReceiptStoreConfig) (block uint64, err error) {
	if err := requireLittIdxStore(cfg); err != nil {
		return 0, err
	}

	indexDir := filepath.Join(cfg.DBDirectory, littIndexDirName)
	// Opening the index would create it. A store that was never written has no head to read, and
	// creating its directories here would leave a half-made store behind on a failed startup.
	if _, statErr := os.Stat(indexDir); os.IsNotExist(statErr) {
		return 0, nil
	}

	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = indexDir
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		return 0, fmt.Errorf("failed to open receipt log index: %w", err)
	}
	defer func() { err = errors.Join(err, index.Close()) }()

	block, _, err = readMetaOffline(index, receiptLatestVersionKey)
	return block, err
}

// GetRange reports the lowest and highest block heights present on disk for the receipt store described
// by cfg, without opening the store. ok is false if no receipts are present.
//
// Only a littidx-backed store is supported; any other backend is refused rather than reported as empty.
func GetRange(cfg dbconfig.ReceiptStoreConfig) (ok bool, lowestBlock uint64, highestBlock uint64, err error) {
	if err := requireLittIdxStore(cfg); err != nil {
		return false, 0, 0, err
	}

	littConfig, err := litt.DefaultConfig(filepath.Join(cfg.DBDirectory, littValuesDirName))
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to build littdb config: %w", err)
	}

	newest, err := offline.NewIterator(littConfig, littReceiptTableName, true)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to open reverse iterator: %w", err)
	}
	highestBlock, ok, err = firstPrimaryKeyHeight(newest)
	closeErr := newest.Close()
	if err != nil {
		return false, 0, 0, err
	}
	if closeErr != nil {
		return false, 0, 0, fmt.Errorf("failed to close reverse iterator: %w", closeErr)
	}
	if !ok {
		return false, 0, 0, nil
	}

	oldest, err := offline.NewIterator(littConfig, littReceiptTableName, false)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to open forward iterator: %w", err)
	}
	defer func() { _ = oldest.Close() }()
	lowestBlock, ok, err = firstPrimaryKeyHeight(oldest)
	if err != nil {
		return false, 0, 0, err
	}
	if !ok {
		return false, 0, 0, nil
	}

	return true, lowestBlock, highestBlock, nil
}

// PruneAfter deletes every receipt for a block height greater than highestBlockToKeep from the receipt
// store described by cfg, without opening the store. It rolls back the litt-backed receipt bodies, then
// deletes the pebble tag-index entries above highestBlockToKeep and moves the store's latest-block metadata
// back to match. A highestBlockToKeep at or above the store's head is a no-op.
//
// It refuses a highestBlockToKeep below the store's retention floor, and one below the oldest receipt still
// on disk: the former names data that has already been pruned, the latter would leave no receipt at all.
// Both are checked before any receipt is touched, so a refusal leaves the store's data unchanged.
//
// Only a littidx-backed store is supported; any other backend is refused rather than silently left alone.
func PruneAfter(cfg dbconfig.ReceiptStoreConfig, highestBlockToKeep uint64) (err error) {
	if err := requireLittIdxStore(cfg); err != nil {
		return err
	}

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
	if earliestFound && highestBlockToKeep < earliest {
		return fmt.Errorf("cannot roll back receipt store to block %d: it is below the retention floor "+
			"(earliest readable block %d); that data has already been pruned", highestBlockToKeep, earliest)
	}

	latest, latestFound, err := readMetaOffline(index, receiptLatestVersionKey)
	if err != nil {
		return err
	}
	if !latestFound {
		// No head has ever been recorded, so there is nothing above highestBlockToKeep to remove.
		return nil
	}
	if latest <= highestBlockToKeep {
		return nil
	}

	if err := requireTargetWithinStoredRange(cfg, highestBlockToKeep, latest); err != nil {
		return err
	}

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

	rd, ok := index.(rangeDeleter)
	if !ok {
		return fmt.Errorf("receipt index %T does not support range delete", index)
	}
	lower := littTagBlockKey(highestBlockToKeep + 1)
	upper := prefixSuccessor([]byte{littTagKeyPrefix})
	if err := rd.DeleteRange(lower, upper, dbtypes.WriteOptions{}); err != nil {
		return fmt.Errorf("failed to delete tag index entries above block %d: %w", highestBlockToKeep, err)
	}
	newLatest := encodeBlockNumber(highestBlockToKeep)
	if err := index.Set(receiptLatestVersionKey, newLatest, dbtypes.WriteOptions{}); err != nil {
		return fmt.Errorf("failed to update latest block metadata: %w", err)
	}
	return nil
}

// requireLittIdxStore refuses an offline operation on anything but a littidx receipt store. The other
// backends keep receipts in a layout these operations cannot read, so they would report a full store as
// empty rather than fail, and would create littidx's subdirectories inside a directory a live store may
// be using.
//
// It checks both the backend the config asks for and the backend the store directory's marker names as its
// owner, which catch different mistakes: the config check refuses a caller that asked for the wrong
// backend, and the marker check refuses a littidx-configured caller pointed at another backend's store. A
// directory carrying no marker predates the marker file, so the configured backend stands.
func requireLittIdxStore(cfg dbconfig.ReceiptStoreConfig) error {
	if backend := normalizeReceiptBackend(cfg.Backend); backend != receiptBackendLittIdx {
		return fmt.Errorf("offline receipt store operations require the %q backend, but this store is "+
			"configured for %q", receiptBackendLittIdx, backend)
	}
	return requireBackendType(cfg.DBDirectory, receiptBackendLittIdx)
}

// requireTargetWithinStoredRange refuses a rollback target below the oldest receipt still on disk. Such a
// target retains no receipts, so the caller almost certainly named a height the store never held; a caller
// that does mean to discard the store entirely can delete its directory. latest is the head the index
// reports, used only to describe an index that claims receipts the litt store does not have.
//
// This must run to completion before the rollback starts: it reads through an offline iterator that holds
// the same litt directory locks the rollback takes.
func requireTargetWithinStoredRange(
	cfg dbconfig.ReceiptStoreConfig,
	highestBlockToKeep uint64,
	latest uint64,
) error {
	ok, lowestBlock, _, err := GetRange(cfg)
	if err != nil {
		return fmt.Errorf("failed to read the receipt store's block range: %w", err)
	}
	if !ok {
		return fmt.Errorf("cannot roll back receipt store to block %d: the index reports a head at block "+
			"%d but no receipts are present on disk", highestBlockToKeep, latest)
	}
	if highestBlockToKeep < lowestBlock {
		return fmt.Errorf("cannot roll back receipt store to block %d: the oldest receipt on disk is for "+
			"block %d, so no receipt would survive; delete %s to discard the store entirely",
			highestBlockToKeep, lowestBlock, cfg.DBDirectory)
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
