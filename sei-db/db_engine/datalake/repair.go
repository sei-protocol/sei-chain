package datalake

import (
	"context"
	"fmt"
	"math"
	"slices"
)

// recoveredState is a store's state as recovered from the store's part of the bucket, after the top of its key
// range is repaired.
type recoveredState struct {
	// False if the store has no metadata object, meaning it has never been written.
	hasMetadata bool

	// The store's keys and its repaired metadata, or a new store's metadata. The durable watermark is one above the
	// highest key, and never below the scheduled deletion watermark.
	topology keyTopology

	// In a store that allows gaps, the keys found below the scheduled deletion watermark, in ascending order. Nil
	// otherwise.
	danglingKeys []uint64
}

// recoverState reads a store's metadata, finishes an interrupted rollback, and repairs the top of its key range. In
// a store that does not allow gaps, it deletes every object above the first gap past the durable watermark. It
// returns an error if the store holds data objects but no metadata object, or if allowGaps differs from the setting
// the store was created with.
func recoverState(
	ctx context.Context,
	reader *bucketReader,
	writer *bucketWriter,
	allowGaps bool,
) (*recoveredState, error) {
	metadata, found, err := loadStoreMetadata(ctx, reader)
	if err != nil {
		return nil, fmt.Errorf("load metadata: %w", err)
	}

	if !found {
		return recoverNewState(ctx, reader, allowGaps)
	}
	if metadata.AllowGaps() != allowGaps {
		return nil, fmt.Errorf("store was created with allowGaps=%t, cannot open it with allowGaps=%t",
			metadata.AllowGaps(), allowGaps)
	}
	if err := finishRollback(ctx, reader, writer, metadata); err != nil {
		return nil, fmt.Errorf("finish interrupted rollback: %w", err)
	}
	if allowGaps {
		return recoverGappedState(ctx, reader, metadata)
	}
	return recoverContiguousState(ctx, reader, writer, metadata)
}

// recoverNewState returns the state of a store without a metadata object. It returns an error if the store
// holds any data object.
func recoverNewState(
	ctx context.Context,
	reader *bucketReader,
	allowGaps bool,
) (*recoveredState, error) {
	metadata := newStoreMetadata(allowGaps)
	topology := metadata.KeyTopology()
	iterator, err := reader.Iterator(true, topology, math.MaxUint64, true, 0)
	if err != nil {
		return nil, fmt.Errorf("create key iterator: %w", err)
	}
	defer func() { _ = iterator.Close() }()
	found, err := iterator.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	if found {
		return nil, fmt.Errorf("store is corrupt: it holds key %d but has no metadata object", iterator.Key())
	}
	return &recoveredState{topology: topology}, nil
}

// recoverContiguousState returns the recovered state of a store that does not allow gaps. Of the objects at or
// above both the durable and the scheduled deletion watermarks, it keeps those that continue the store's keys
// without a gap and deletes the rest.
func recoverContiguousState(
	ctx context.Context,
	reader *bucketReader,
	writer *bucketWriter,
	metadata *storeMetadata,
) (*recoveredState, error) {
	// Every write to a key below the durable watermark is crash durable, and every key below the scheduled deletion
	// watermark is no longer logically part of the store, even while its object awaits deletion. A key at or above
	// the higher of the two may or may not have been written before a crash.
	firstUncertainKey := max(metadata.DurableWatermark(), metadata.ScheduledDeletionWatermark())

	// The keys found at or above firstUncertainKey, where writes may or may not have been lost to a crash, in
	// descending order.
	var uncertainKeys []uint64
	iterator, err := reader.Iterator(true, metadata.KeyTopology(), math.MaxUint64, true, 0)
	if err != nil {
		return nil, fmt.Errorf("create key iterator: %w", err)
	}
	defer func() { _ = iterator.Close() }()
	for {
		ok, err := iterator.Next(ctx)
		if err != nil {
			return nil, fmt.Errorf("list keys from %d: %w", firstUncertainKey, err)
		}
		if !ok || iterator.Key() < firstUncertainKey {
			break
		}
		uncertainKeys = append(uncertainKeys, iterator.Key())
	}

	// Keep the uncertain keys that can be retained without leaving gaps.
	firstMissingKey := firstUncertainKey
	remaining := len(uncertainKeys)
	for remaining > 0 && uncertainKeys[remaining-1] == firstMissingKey {
		firstMissingKey++
		remaining--
	}

	// Delete all keys whose presence causes the store to have gaps.
	for _, key := range uncertainKeys[:remaining] {
		writer.Delete(key)
	}
	if err := writer.Flush(); err != nil {
		return nil, fmt.Errorf("delete keys above the gap at %d: %w", firstMissingKey, err)
	}

	err = metadata.SetWatermarks(
		metadata.ScheduledDeletionWatermark(), metadata.DeletionWatermark(), firstMissingKey)
	if err != nil {
		return nil, fmt.Errorf("record repaired durable watermark: %w", err)
	}
	return &recoveredState{hasMetadata: true, topology: metadata.KeyTopology()}, nil
}

// recoverGappedState returns the recovered state of a store that allows gaps. Every object in the store's part of
// the bucket at or above the scheduled deletion watermark is one of the store's keys.
func recoverGappedState(
	ctx context.Context,
	reader *bucketReader,
	metadata *storeMetadata,
) (*recoveredState, error) {
	// Every key with an object in the store's part of the bucket.
	var allKeys []uint64
	iterator, err := reader.Iterator(true, metadata.KeyTopology(), math.MaxUint64, true, 0)
	if err != nil {
		return nil, fmt.Errorf("create key iterator: %w", err)
	}
	defer func() { _ = iterator.Close() }()
	for {
		ok, err := iterator.Next(ctx)
		if err != nil {
			return nil, fmt.Errorf("list keys: %w", err)
		}
		if !ok {
			break
		}
		allKeys = append(allKeys, iterator.Key())
	}
	// The iterator visits keys from highest to lowest, and recoveredState's key lists are ascending.
	slices.Reverse(allKeys)

	// Keys below the scheduled deletion watermark have been scheduled for pruning.
	split, _ := slices.BinarySearch(allKeys, metadata.ScheduledDeletionWatermark())
	keysScheduledForPruning := allKeys[:split]
	readableKeys := allKeys[split:]

	// One above the highest readable key, or the scheduled deletion watermark if no key is readable.
	durableWatermark := metadata.ScheduledDeletionWatermark()
	if len(readableKeys) > 0 {
		durableWatermark = max(durableWatermark, readableKeys[len(readableKeys)-1]+1)
	}
	err = metadata.SetWatermarks(
		metadata.ScheduledDeletionWatermark(), metadata.DeletionWatermark(), durableWatermark)
	if err != nil {
		return nil, fmt.Errorf("record repaired durable watermark: %w", err)
	}
	return &recoveredState{
		hasMetadata:  true,
		topology:     metadata.KeyTopology().WithKeys(readableKeys),
		danglingKeys: keysScheduledForPruning,
	}, nil
}
