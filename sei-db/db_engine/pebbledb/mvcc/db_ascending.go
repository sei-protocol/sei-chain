package mvcc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/exp/slices"

	dbm "github.com/tendermint/tm-db"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
)

// This file contains the ascending-version MVCC implementation used to read
// and write legacy DBs that were created before the descending-version fast
// path was introduced. It is a verbatim port of main's Get/Has/Iterator/
// ReverseIterator/Prune path, adjusted only to use the *Ascending encoding
// helpers and the ascendingIterator type. Archive nodes that cannot migrate
// will continue to hit this path.

func (db *Database) hasAscending(storeKey string, version int64, key []byte) (bool, error) {
	if version < db.GetEarliestVersion() {
		return false, nil
	}

	val, err := db.getAscending(storeKey, version, key)
	if err != nil {
		return false, err
	}

	return val != nil, nil
}

func (db *Database) getAscending(storeKey string, targetVersion int64, key []byte) (_ []byte, _err error) {
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

	db.operationMetrics.AddRead(1)
	prefixedVal, err := getMVCCSliceAscending(db.storage, storeKey, key, targetVersion)
	if err != nil {
		if errors.Is(err, errorutils.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to perform PebbleDB read: %w", err)
	}

	valBz, tombBz, ok := SplitMVCCKey(prefixedVal)
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC value: %s", prefixedVal)
	}

	// A tombstone of zero or a target version that is less than the tombstone
	// version means the key is not deleted at the target version.
	if len(tombBz) == 0 {
		return valBz, nil
	}

	tombstone, err := decodeUint64Ascending(tombBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode value tombstone: %w", err)
	}

	// A tombstone of zero or a target version that is less than the tombstone
	// version means the key is not deleted at the target version.
	if targetVersion < tombstone {
		return valBz, nil
	}

	// the value is considered deleted
	return nil, nil
}

func (db *Database) pruneAscending(version int64) (_err error) {
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
		counter                                 int
		prevKey, prevKeyEncoded, prevValEncoded []byte
		prevVersionDecoded                      int64
		prevStore                               string
		scanReads                               int64
		firstDeletedKey, lastDeletedKey         []byte
	)

	for itr.First(); itr.Valid(); {
		scanReads++
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

		currVersionDecoded, err := decodeUint64Ascending(currVersion)
		if err != nil {
			return err
		}

		// Seek to next key if we are at a version which is higher than prune height
		// Do not seek to next key if KeepLastVersion is false and we need to delete the previous key in pruning
		if currVersionDecoded > version && (db.config.KeepLastVersion || prevVersionDecoded > version) {
			itr.NextPrefix()
			continue
		}

		// Delete a key if another entry for that key exists at a larger version than original but leq to the prune height
		// Also delete a key if it has been tombstoned and its version is leq to the prune height
		// Also delete a key if KeepLastVersion is false and version is leq to the prune height
		if prevVersionDecoded <= version && (bytes.Equal(prevKey, currKey) || valTombstoned(prevValEncoded) || !db.config.KeepLastVersion) {
			err = batch.Delete(prevKeyEncoded, nil)
			if err != nil {
				return err
			}

			// Track the deleted span (keys are visited in comparer order, so the
			// first delete is the smallest and the last is the largest) to compact
			// just that range once pruning completes.
			if firstDeletedKey == nil {
				firstDeletedKey = prevKeyEncoded
			}
			lastDeletedKey = prevKeyEncoded

			counter++
			if counter >= PruneCommitBatchSize {
				writeCount := int64(batch.Count())
				err = batch.Commit(defaultWriteOpts)
				if err != nil {
					return err
				}

				db.operationMetrics.AddWrite(writeCount)
				counter = 0
				batch.Reset()
			}
		}

		// Update prevKey and prevVersion for next iteration
		prevKey = currKey
		prevVersionDecoded = currVersionDecoded
		prevKeyEncoded = currKeyEncoded
		prevValEncoded = slices.Clone(itr.Value())

		itr.Next()
	}

	// Commit any leftover delete ops in batch
	if counter > 0 {
		writeCount := int64(batch.Count())
		err = batch.Commit(defaultWriteOpts)
		if err != nil {
			return err
		}
		db.operationMetrics.AddWrite(writeCount)
	}
	db.operationMetrics.AddRead(scanReads)

	if err := db.SetEarliestVersion(earliestVersion, false); err != nil {
		return err
	}
	return db.compactPrunedRange(firstDeletedKey, lastDeletedKey)
}

func (db *Database) iteratorAscending(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncodeAscending(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncodeAscending(prependStoreKey(storeKey, end), 0)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newAscendingIterator(itr, storePrefix(storeKey), start, end, version, db.GetEarliestVersion(), false, storeKey, db.operationMetrics), nil
}

func (db *Database) reverseIteratorAscending(storeKey string, version int64, start, end []byte) (dbm.Iterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errorutils.ErrKeyEmpty
	}

	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, errorutils.ErrStartAfterEnd
	}

	lowerBound := MVCCEncodeAscending(prependStoreKey(storeKey, start), 0)

	var upperBound []byte
	if end != nil {
		upperBound = MVCCEncodeAscending(prependStoreKey(storeKey, end), 0)
	} else {
		upperBound = MVCCEncodeAscending(prefixEnd(storePrefix(storeKey)), 0)
	}

	itr, err := db.storage.NewIter(&pebble.IterOptions{LowerBound: lowerBound, UpperBound: upperBound})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}

	return newAscendingIterator(itr, storePrefix(storeKey), start, end, version, db.GetEarliestVersion(), true, storeKey, db.operationMetrics), nil
}

func getMVCCSliceAscending(db *pebble.DB, storeKey string, key []byte, version int64) ([]byte, error) {
	// end domain is exclusive, so we need to increment the version by 1
	if version < math.MaxInt64 {
		version++
	}

	itr, err := db.NewIter(&pebble.IterOptions{
		LowerBound: MVCCEncodeAscending(prependStoreKey(storeKey, key), 0),
		UpperBound: MVCCEncodeAscending(prependStoreKey(storeKey, key), version),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	defer func() {
		err = errorutils.Join(err, itr.Close())
	}()

	if !itr.Last() {
		return nil, errorutils.ErrRecordNotFound
	}

	_, vBz, ok := SplitMVCCKey(itr.Key())
	if !ok {
		return nil, fmt.Errorf("invalid PebbleDB MVCC key: %s", itr.Key())
	}

	keyVersion, err := decodeUint64Ascending(vBz)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key version: %w", err)
	}
	if keyVersion > version {
		return nil, fmt.Errorf("key version too large: %d", keyVersion)
	}

	return slices.Clone(itr.Value()), nil
}
