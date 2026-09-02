package receipt

import (
	"context"
	"encoding/binary"
	"fmt"
	"path/filepath"

	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/offline"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// GetRange reports the lowest and highest block heights present on disk for the receipt store described
// by cfg, without opening the store. ok is false if no receipts are present.
func GetRange(cfg dbconfig.ReceiptStoreConfig) (ok bool, lowestBlock uint64, highestBlock uint64, err error) {
	littConfig, err := litt.DefaultConfig(filepath.Join(cfg.DBDirectory, "littdb"))
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
// store described by cfg, without opening the store. It refuses if highestBlockToKeep falls below the
// store's retention floor, since that data has already been pruned and cannot be faithfully restored —
// checked before any mutation, so a refusal leaves the store untouched. Otherwise it rolls back the
// litt-backed receipt bodies, then deletes the pebble tag-index entries above highestBlockToKeep and moves
// the store's latest-block metadata back to match.
func PruneAfter(cfg dbconfig.ReceiptStoreConfig, highestBlockToKeep uint64) error {
	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = filepath.Join(cfg.DBDirectory, "log-index")
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	if err != nil {
		return fmt.Errorf("failed to open receipt log index: %w", err)
	}
	defer func() { _ = index.Close() }()

	earliest, err := readMetaOffline(index, receiptEarliestVersionKey)
	if err != nil {
		return err
	}
	if earliest > 0 && highestBlockToKeep < earliest {
		return fmt.Errorf("cannot roll back receipt store to block %d: it is below the retention floor "+
			"(earliest readable block %d); that data has already been pruned", highestBlockToKeep, earliest)
	}

	latest, err := readMetaOffline(index, receiptLatestVersionKey)
	if err != nil {
		return err
	}
	if latest <= highestBlockToKeep {
		return nil
	}

	littConfig, err := litt.DefaultConfig(filepath.Join(cfg.DBDirectory, "littdb"))
	if err != nil {
		return fmt.Errorf("failed to build littdb config: %w", err)
	}
	filter := func(tableName string, key []byte, isPrimary bool) (bool, error) {
		if tableName != littReceiptTableName || !isPrimary {
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
// Returns 0 if the key is absent, malformed, or unreadable, matching littReceiptStore.readMeta's behavior.
func readMetaOffline(index dbtypes.KeyValueDB, key []byte) (uint64, error) {
	val, err := index.Get(key)
	if err != nil || len(val) != blockNumLen {
		return 0, nil
	}
	return decodeBlockNumber(val), nil
}
