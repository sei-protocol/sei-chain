package keeper

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
	deleted := 0
	skippedCheckpoint := len(checkpoint) == 0
	keysToDelete := make([][]byte, 0, limit)
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
		lastKey = key

		val := iterator.Value()
		if isZeroStorageValue(val) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	if processed == 0 {
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

	for _, key := range keysToDelete {
		store.Delete(key)
		deleted++
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
