package mvcc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/exp/slices"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// This file contains the descending-version MVCC implementation used by DBs
// created by this build. It is the fast path: versions of a logical key sort
// newest-first on disk, so Pebble's First() / SeekGE lands directly on the
// latest visible version without iterating older ones.
//
// Callers go through the dispatchers in db.go; nothing here should be invoked
// directly by code outside the package.

func (db *Database) hasDescending(storeKey string, version int64, key []byte) (bool, error) {
	if version < db.GetEarliestVersion() {
		return false, nil
	}

	val, err := db.getDescending(storeKey, version, key)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

func (db *Database) getDescending(storeKey string, targetVersion int64, key []byte) (_ []byte, _err error) {
	startTime := time.Now()
	defer func() {
		otelMetrics.getLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(
				attribute.Bool("success", _err == nil),
				attribute.String("store", storeKey),
			),
		)
	}()
	if targetVersion < db.GetEarliestVersion() {
		return nil, nil
	}

	prefixedVal, err := getMVCCSliceDescending(db.storage, storeKey, key, targetVersion)
	if err != nil {
		if errors.Is(err, errorutils.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to perform PebbleDB read: %w", err)
	}

	return visibleValueAtVersionDescending(prefixedVal, targetVersion)
}

// pruneDescending attempts to prune all versions up to and including the current version
// Get the range of keys, manually iterate over them and delete them
// We add a heuristic to skip over a module's keys during pruning if it hasn't been updated
// since the last time pruning occurred.
// NOTE: There is a rare case when a module's keys are skipped during pruning even though
// it has been updated. This occurs when that module's keys are updated in between pruning runs, the node after is restarted.
// This is not a large issue given the next time that module is updated, it will be properly pruned thereafter.
func (db *Database) pruneDescending(version int64) (_err error) {
	// Defensive check: ensure database is not closed
	if db.storage == nil {
		return errors.New("pebbledb: database is closed")
	}

	startTime := time.Now()
	defer func() {
		otelMetrics.pruneLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(
				attribute.Bool("success", _err == nil),
			),
		)
	}()

	earliestVersion := version + 1 // we increment by 1 to include the provided version

	itr, err := db.storage.NewIter(nil)
	if err != nil {
		return err
	}
	defer func() { _ = itr.Close() }()

	batch := db.storage.NewBatch()
	defer func() { _ = batch.Close() }()

	var (
		counter        int
		prevKey        []byte
		keptBelowPrune bool
		prevStore      string
	)

	for itr.First(); itr.Valid(); {
		currKeyEncoded := slices.Clone(itr.Key())

		// Ignore metadata entries during pruning
		if isMetadataKey(currKeyEncoded) {
			itr.Next()
			continue
		}

		// Store current key and version
		currKey, currVersion, currOK := SplitMVCCKey(currKeyEncoded)
		if !currOK {
			return fmt.Errorf("invalid MVCC key")
		}

		storeKey, err := parseStoreKey(currKey)
		if err != nil {
			// XXX: This should never happen given we skip the metadata keys.
			return err
		}

		// For every new module visited, check to see last time it was updated
		if storeKey != prevStore {
			prevStore = storeKey
			updated, ok := db.storeKeyDirty.Load(storeKey)
			versionUpdated, typeOk := updated.(int64)
			// Skip a store's keys if version it was last updated is less than last prune height
			if !ok || (typeOk && versionUpdated < db.GetEarliestVersion()) {
				itr.SeekGE(storePrefix(storeKey + "0"))
				continue
			}
		}

		currVersionDecoded, err := decodeUint64Descending(currVersion)
		if err != nil {
			return err
		}

		// Reset per-logical-key state when the logical key changes.
		if !bytes.Equal(prevKey, currKey) {
			prevKey = slices.Clone(currKey)
			keptBelowPrune = false

			// Fast path: under descending encoding, versions of a key are stored
			// newest-first. When the newest real version is above the prune
			// height, seek directly to the first version <= prune height for
			// this key instead of iterating through every above-prune version.
			if currVersionDecoded > version {
				itr.SeekGE(MVCCEncodeDescending(currKey, version))
				continue
			}
		}

		// Descending iteration: for a given logical key we see newest→oldest.
		// Versions > prune height are always kept. For versions <= prune
		// height, keep only the newest one when KeepLastVersion is true;
		// delete every other such version.
		if currVersionDecoded <= version {
			if db.config.KeepLastVersion && !keptBelowPrune {
				keptBelowPrune = true
			} else {
				if err := batch.Delete(currKeyEncoded, nil); err != nil {
					return err
				}
				counter++
				if counter >= PruneCommitBatchSize {
					if err := batch.Commit(defaultWriteOpts); err != nil {
						return err
					}
					counter = 0
					batch.Reset()
				}
			}
		}

		itr.Next()
	}

	// Commit any leftover delete ops in batch
	if counter > 0 {
		err = batch.Commit(defaultWriteOpts)
		if err != nil {
			return err
		}
	}

	return db.SetEarliestVersion(earliestVersion, false)
}

func (db *Database) iteratorDescending(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncodeDescending(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncodeDescending(prependStoreKey(storeKey, end), 0)
	} else {
		upperBound = iteratorUpperBoundForStoreDescending(storeKey)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, db.GetEarliestVersion(), false, storeKey), nil
}

func (db *Database) reverseIteratorDescending(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncodeDescending(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncodeDescending(prependStoreKey(storeKey, end), 0)
	} else {
		upperBound = MVCCEncodeDescending(prefixEnd(storePrefix(storeKey)), 0)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newPebbleDBIterator(itr, storePrefix(storeKey), start, end, version, db.GetEarliestVersion(), true, storeKey), nil
}

func getMVCCSliceDescending(db *pebble.DB, storeKey string, key []byte, version int64) (_ []byte, err error) {
	prefixedKey := prependStoreKey(storeKey, key)
	itr, err := db.NewIter(&pebble.IterOptions{
		LowerBound: MVCCEncodeDescending(prefixedKey, version),
		UpperBound: iteratorUpperBoundForLogicalKeyDescending(prefixedKey),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer func() {
		err = errorutils.Join(err, itr.Close())
	}()

	if !itr.First() {
		return nil, errorutils.ErrRecordNotFound
	}
	return decodeMVCCEntryDescending(itr.Key(), itr.Value(), prefixedKey, version)
}

// decodeMVCCEntryDescending validates that the iterator's current entry
// belongs to prefixedKey at a version <= target and returns a safe copy of the
// value. Assumes descending version encoding.
func decodeMVCCEntryDescending(rawIterKey, rawIterValue, prefixedKey []byte, version int64) ([]byte, error) {
	userKey, vBz, ok := SplitMVCCKey(rawIterKey)
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC key: %s", rawIterKey)
	}
	if !bytes.Equal(userKey, prefixedKey) {
		return nil, errorutils.ErrRecordNotFound
	}
	keyVersion, err := decodeUint64Descending(vBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key version: %w", err)
	}
	if keyVersion > version {
		return nil, errorutils.ErrRecordNotFound
	}
	return slices.Clone(rawIterValue), nil
}

func visibleValueAtVersionDescending(prefixedVal []byte, targetVersion int64) ([]byte, error) {
	valBz, tombBz, ok := SplitMVCCKey(prefixedVal)
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC value: %s", prefixedVal)
	}
	if len(tombBz) == 0 {
		return valBz, nil
	}
	tombstone, err := decodeUint64Descending(tombBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode value tombstone: %w", err)
	}
	if targetVersion < tombstone {
		return valBz, nil
	}
	return nil, nil
}

func iteratorUpperBoundForStoreDescending(storeKey string) []byte {
	upperStorePrefix := prefixEnd(storePrefix(storeKey))
	if upperStorePrefix == nil {
		return nil
	}
	return MVCCEncodeDescending(upperStorePrefix, 0)
}

func iteratorUpperBoundForLogicalKeyDescending(key []byte) []byte {
	upperKeyPrefix := prefixEnd(key)
	if upperKeyPrefix == nil {
		return nil
	}
	return MVCCEncodeDescending(upperKeyPrefix, 0)
}
