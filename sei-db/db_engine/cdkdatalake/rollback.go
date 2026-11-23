package cdkdatalake

import (
	"context"
	"errors"
	"fmt"
	"math"

	"gocloud.dev/blob"
)

// RollBackCDKDataLake deletes every key at or above rollbackWatermark from the store named storeName in the bucket
// named by config. The store must not be open while it runs. Once recorded, a rollback takes effect even if it is
// interrupted: the next call or the next open of the store finishes it.
func RollBackCDKDataLake(
	ctx context.Context,
	// Names the store within the bucket. Must match ^[a-zA-Z0-9_-]+$.
	storeName string,
	// Must match the setting the store was created with.
	allowGaps bool,
	config DataLakeConfig,
	// Every key strictly less than this watermark is kept.
	rollbackWatermark uint64,
) (err error) {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	if !storeNameRegex.MatchString(storeName) {
		return fmt.Errorf("invalid store name %q, must match %s", storeName, storeNameRegex)
	}

	bucket, err := blob.OpenBucket(ctx, config.BucketURL)
	if err != nil {
		return fmt.Errorf("open bucket %q: %w", config.BucketURL, err)
	}
	defer func() {
		if closeErr := bucket.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close bucket: %w", closeErr))
		}
	}()
	storeBucket := newStoreBucket(bucket, storeName)
	reader := newBucketReader(ctx, storeBucket, config)
	defer reader.Close()
	writer := newBucketWriter(ctx, storeBucket, config)
	defer writer.Close()

	metadata, found, err := loadStoreMetadata(ctx, reader)
	if err != nil {
		return fmt.Errorf("load metadata of store %q: %w", storeName, err)
	}
	if !found {
		// A store that has never been written holds nothing to roll back.
		return nil
	}
	if metadata.AllowGaps() != allowGaps {
		return fmt.Errorf("store %q was created with allowGaps=%t, cannot roll it back with allowGaps=%t",
			storeName, metadata.AllowGaps(), allowGaps)
	}

	if err := finishRollback(ctx, reader, writer, metadata); err != nil {
		return fmt.Errorf("finish interrupted rollback of store %q: %w", storeName, err)
	}
	if err := metadata.StartRollback(rollbackWatermark); err != nil {
		return fmt.Errorf("record rollback of store %q: %w", storeName, err)
	}
	if err := writer.WriteMetadata(metadata.Serialize()).Wait(); err != nil {
		return fmt.Errorf("write rollback of store %q: %w", storeName, err)
	}
	if err := finishRollback(ctx, reader, writer, metadata); err != nil {
		return fmt.Errorf("roll back store %q: %w", storeName, err)
	}
	return nil
}

// finishRollback completes the rollback recorded in metadata, if one is in progress: it deletes every key at or
// above the rollback watermark and records the rollback as finished.
func finishRollback(
	ctx context.Context,
	reader *bucketReader,
	writer *bucketWriter,
	metadata *storeMetadata,
) error {
	if !metadata.RollbackInProgress() {
		return nil
	}
	remainingEnd, err := deleteKeysFrom(ctx, reader, writer, metadata)
	if err != nil {
		return fmt.Errorf("delete keys: %w", err)
	}
	if err := metadata.FinishRollback(remainingEnd); err != nil {
		return fmt.Errorf("record finished rollback: %w", err)
	}
	if err := writer.WriteMetadata(metadata.Serialize()).Wait(); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}

// deleteKeysFrom deletes every key at or above the rollback watermark of metadata.
func deleteKeysFrom(
	ctx context.Context,
	reader *bucketReader,
	writer *bucketWriter,
	metadata *storeMetadata,
) (
	// One above the highest key left, or 0 if none is left.
	remainingEnd uint64,
	err error,
) {
	watermark := metadata.RollbackWatermark()
	iterator, err := reader.Iterator(true, metadata.KeyTopology(), math.MaxUint64, true, 0)
	if err != nil {
		return 0, fmt.Errorf("create key iterator: %w", err)
	}
	defer func() { _ = iterator.Close() }()
	for {
		ok, err := iterator.Next(ctx)
		if err != nil {
			return 0, fmt.Errorf("list keys at or above %d: %w", watermark, err)
		}
		if !ok {
			break
		}
		if iterator.Key() < watermark {
			remainingEnd = iterator.Key() + 1
			break
		}
		writer.Delete(iterator.Key())
	}
	if err := writer.Flush(); err != nil {
		return 0, fmt.Errorf("delete keys: %w", err)
	}
	return remainingEnd, nil
}
