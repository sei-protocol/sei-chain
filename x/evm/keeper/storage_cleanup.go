package keeper

import (
	"bytes"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	seimetrics "github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const ZeroStorageCleanupBatchSize = 100

func (k *Keeper) GetZeroStorageCleanupCheckpoint(ctx sdk.Context) []byte {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.ZeroStorageCleanupCheckpointKey)
	if len(bz) == 0 {
		return nil
	}
	return append([]byte(nil), bz...)
}

func (k *Keeper) setZeroStorageCleanupCheckpoint(ctx sdk.Context, key []byte) {
	store := ctx.KVStore(k.storeKey)
	if len(key) == 0 {
		store.Delete(types.ZeroStorageCleanupCheckpointKey)
		return
	}
	store.Set(types.ZeroStorageCleanupCheckpointKey, key)
}

func (k *Keeper) PruneZeroStorageSlots(ctx sdk.Context, limit int) (int, int) {
	if limit <= 0 {
		return 0, 0
	}

	checkpoint := k.GetZeroStorageCleanupCheckpoint(ctx)
	store := k.PrefixStore(ctx, types.StateKeyPrefix)
	iterator := store.Iterator(checkpoint, nil)
	defer func() { _ = iterator.Close() }()

	processed := 0
	processedMetric := uint64(0)
	deleted := 0
	deletedMetric := uint64(0)
	var bytesPruned uint64
	skippedCheckpoint := len(checkpoint) == 0
	var lastKey []byte

	for ; iterator.Valid() && processed < limit; iterator.Next() {
		key := append([]byte(nil), iterator.Key()...)
		if !skippedCheckpoint {
			if bytes.Equal(key, checkpoint) {
				skippedCheckpoint = true
				continue
			}
			skippedCheckpoint = true
		}

		processed++
		processedMetric++
		lastKey = key

		val := iterator.Value()
		if isZeroStorageValue(val) {
			store.Delete(key)
			deleted++
			deletedMetric++
			bytesPruned += uint64(len(key)) + uint64(len(val))
		}
	}

	if processed == 0 {
		// No keys were iterated, so the saved checkpoint already points to the end
		// of the iterator. Clear it so the next run restarts from the beginning
		// rather than resuming from an exhausted position.
		if len(checkpoint) != 0 && !iterator.Valid() {
			k.setZeroStorageCleanupCheckpoint(ctx, nil)
		}
		return 0, 0
	}

	if iterator.Valid() {
		k.setZeroStorageCleanupCheckpoint(ctx, lastKey)
	} else {
		k.setZeroStorageCleanupCheckpoint(ctx, nil)
	}

	seimetrics.IncrEvmZeroStorageProcessedKeys(processedMetric)

	if deleted > 0 {
		seimetrics.IncrEvmZeroStoragePrunedKeys(deletedMetric)
		seimetrics.IncrEvmZeroStoragePrunedBytes(bytesPruned)
		ctx.Logger().Info("pruned zero storage slots", "processed", processed, "deleted", deleted, "bytes_saved", bytesPruned)
	}
	return processed, deleted
}

func isZeroStorageValue(val []byte) bool {
	if len(val) == 0 {
		return true
	}
	for _, b := range val {
		if b != 0 {
			return false
		}
	}
	return true
}
