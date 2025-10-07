package keeper

import (
	"bytes"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const ZeroStorageCleanupBatchSize = 100

// zeroStorageCleanupCheckpointInMemory tracks the checkpoint without persisting to KV store.
var zeroStorageCleanupCheckpointInMemory []byte

func (k *Keeper) GetZeroStorageCleanupCheckpoint(ctx sdk.Context) []byte {
	if len(zeroStorageCleanupCheckpointInMemory) == 0 {
		return nil
	}
	return append([]byte(nil), zeroStorageCleanupCheckpointInMemory...)
}

func (k *Keeper) PruneZeroStorageSlots(ctx sdk.Context, limit int) (int, int) {
	if limit <= 0 {
		return 0, 0
	}

	start := time.Now()

	checkpoint := append([]byte(nil), zeroStorageCleanupCheckpointInMemory...)
	store := k.PrefixStore(ctx, types.StateKeyPrefix)
	iterator := store.Iterator(checkpoint, nil)
	defer func() { _ = iterator.Close() }()

	processed := 0
	zeroValueCount := 0
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
		lastKey = key

		val := iterator.Value()
		if isZeroStorageValue(val) {
			zeroValueCount++
		}
	}

	if processed == 0 {
		if len(checkpoint) != 0 && !iterator.Valid() {
			zeroStorageCleanupCheckpointInMemory = nil
		}
		duration := time.Since(start)
		fmt.Printf("[DEBUG] Zero storage slot scan took %s (processed: %d, zero-value: %d)\n", duration, processed, zeroValueCount)
		return 0, 0
	}

	if iterator.Valid() {
		zeroStorageCleanupCheckpointInMemory = append([]byte(nil), lastKey...)
	} else {
		zeroStorageCleanupCheckpointInMemory = nil
	}

	duration := time.Since(start)
	fmt.Printf("[DEBUG] Zero storage slot scan took %s (processed: %d, zero-value: %d)\n", duration, processed, zeroValueCount)

	return processed, 0
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
