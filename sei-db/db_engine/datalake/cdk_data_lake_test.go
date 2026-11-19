package datalake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
)

// openTestDataLake opens a store with config, closed when the test ends.
func openTestDataLake(t *testing.T, storeName string, allowGaps bool, config DataLakeConfig) *cdkDataLake {
	lake, err := newCDKDataLake(context.Background(), storeName, allowGaps, config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lake.Close()) })
	return lake
}

// openTestStoreBucket opens a second handle on the bucket config names, for reading and writing a store's objects
// without going through the store. The handle is closed when the test ends.
func openTestStoreBucket(t *testing.T, config DataLakeConfig, storeName string) *storeBucket {
	bucket, err := blob.OpenBucket(context.Background(), config.BucketURL)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, bucket.Close()) })
	return newStoreBucket(bucket, storeName)
}

// expectedDeletionWatermark returns the deletion watermark a test store's metadata records: watermark in a store
// without gaps, and 0 in a store with gaps, whose deletion watermark stays where a new store starts it.
func expectedDeletionWatermark(allowGaps bool, watermark uint64) uint64 {
	if allowGaps {
		return 0
	}
	return watermark
}

// testStoreMetadata returns metadata with the given gap setting and watermarks.
func testStoreMetadata(
	t *testing.T,
	allowGaps bool,
	scheduledDeletion uint64,
	deletion uint64,
	durable uint64,
) *storeMetadata {
	metadata := newStoreMetadata(allowGaps)
	require.NoError(t, metadata.SetWatermarks(scheduledDeletion, deletion, durable))
	return metadata
}

// testValue returns the value tests write at key.
func testValue(key uint64) []byte {
	return []byte(fmt.Sprintf("value-%d", key))
}

// putTestKeys writes testValue() at each key.
func putTestKeys(t *testing.T, lake DataLake, keys ...uint64) {
	for _, key := range keys {
		require.NoError(t, lake.Put(context.Background(), key, testValue(key)))
	}
}

// keyRange returns the keys from first to last, inclusive.
func keyRange(first uint64, last uint64) []uint64 {
	var keys []uint64
	for key := first; key <= last; key++ {
		keys = append(keys, key)
	}
	return keys
}

// requireObjectExists checks whether the object holding key exists in the bucket, bypassing the store.
func requireObjectExists(t *testing.T, storeBucket *storeBucket, key uint64, expected bool) {
	exists, err := storeBucket.Has(context.Background(), key)
	require.NoError(t, err)
	require.Equal(t, expected, exists, "object for key %d", key)
}

// loadTestMetadata reads and deserializes a store's metadata object, bypassing the store.
func loadTestMetadata(
	ctx context.Context,
	storeBucket *storeBucket,
) (
	metadata *storeMetadata,
	// False if the store has no metadata object.
	found bool,
	err error,
) {
	data, found, err := storeBucket.ReadMetadata(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("read metadata: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	metadata, err = deserializeStoreMetadata(data)
	if err != nil {
		return nil, false, fmt.Errorf("deserialize metadata: %w", err)
	}
	return metadata, true, nil
}

// readMetadata reads a store's metadata object, failing the test if it is missing.
func readMetadata(t *testing.T, storeBucket *storeBucket) *storeMetadata {
	metadata, ok, err := loadTestMetadata(context.Background(), storeBucket)
	require.NoError(t, err)
	require.True(t, ok)
	return metadata
}

func TestCDKDataLakePutGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", false, DefaultTestDataLakeConfig(t.TempDir()))

	putTestKeys(t, lake, keyRange(10, 19)...)
	require.NoError(t, lake.Flush(ctx))

	for key := uint64(10); key <= 19; key++ {
		value, ok, err := lake.Get(ctx, key)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, testValue(key), value)

		stream, ok, err := lake.GetStream(ctx, key)
		require.NoError(t, err)
		require.True(t, ok)
		streamed, err := io.ReadAll(stream)
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		require.Equal(t, testValue(key), streamed)

		has, err := lake.Has(ctx, key)
		require.NoError(t, err)
		require.True(t, has)
	}

	for _, key := range []uint64{0, 9, 20} {
		_, ok, err := lake.Get(ctx, key)
		require.NoError(t, err)
		require.False(t, ok)
		_, ok, err = lake.GetStream(ctx, key)
		require.NoError(t, err)
		require.False(t, ok)
		has, err := lake.Has(ctx, key)
		require.NoError(t, err)
		require.False(t, has)
	}

	require.Equal(t, uint64(20), lake.GetDurableWatermark())
	first, ok := lake.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(10), first)
}

func TestCDKDataLakeEmptyStoreReportsNoBounds(t *testing.T) {
	lake := openTestDataLake(t, "store", false, DefaultTestDataLakeConfig(t.TempDir()))
	require.Equal(t, uint64(0), lake.GetDurableWatermark())
	_, ok := lake.GetFirstKey()
	require.False(t, ok)
	require.NoError(t, lake.Flush(context.Background()))
}

func TestCDKDataLakePutStreamRoundTrip(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", true, DefaultTestDataLakeConfig(t.TempDir()))

	large := bytes.Repeat([]byte("snapshot"), 1<<16)
	stream := newTestStream(large)
	require.NoError(t, lake.PutStream(ctx, 100, stream))
	require.NoError(t, lake.Flush(ctx))

	value, ok, err := lake.Get(ctx, 100)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, large, value)
	require.Equal(t, 0, stream.resets)
	require.True(t, stream.closed)
}

func TestCDKDataLakePutStreamRestartsAfterTransientFailures(t *testing.T) {
	ctx := context.Background()
	faulty, config := newFaultyBucket(t)
	failures := 0
	faulty.SetHook(func(_ context.Context, op string, key string) error {
		if op == "commit" && !strings.HasSuffix(key, metadataObjectName) && failures < 2 {
			failures++
			return &injectedError{code: gcerrors.Unknown}
		}
		return nil
	})
	lake := openTestDataLake(t, "store", true, config)

	large := bytes.Repeat([]byte("snapshot"), 1<<16)
	stream := newTestStream(large)
	require.NoError(t, lake.PutStream(ctx, 100, stream))
	require.NoError(t, lake.Flush(ctx))

	value, ok, err := lake.Get(ctx, 100)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, large, value)
	require.Equal(t, 2, stream.resets)
	require.True(t, stream.closed)
}

func TestCDKDataLakePutStreamReadFailureFailsTheStore(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", true, DefaultTestDataLakeConfig(t.TempDir()))

	stream := newTestStream([]byte("value"))
	stream.readErr = errors.New("disk on fire")
	require.NoError(t, lake.PutStream(ctx, 100, stream))
	err := lake.Flush(ctx)
	require.ErrorContains(t, err, "disk on fire")
	require.ErrorContains(t, lake.Put(ctx, 200, nil), "disk on fire")
	require.Equal(t, 0, stream.resets, "a failure of the stream itself is not retried")
	require.True(t, stream.closed)
}

func TestCDKDataLakeRejectsGapsWhenGapsAreNotAllowed(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", false, DefaultTestDataLakeConfig(t.TempDir()))

	require.NoError(t, lake.Put(ctx, 5, nil))
	require.Error(t, lake.Put(ctx, 7, nil))
	require.Error(t, lake.Put(ctx, 5, nil))
	require.Error(t, lake.Put(ctx, 4, nil))
	require.NoError(t, lake.Put(ctx, 6, nil))
}

func TestCDKDataLakeRequiresIncreasingKeysWhenGapsAreAllowed(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", true, DefaultTestDataLakeConfig(t.TempDir()))

	require.NoError(t, lake.Put(ctx, 5, nil))
	require.NoError(t, lake.Put(ctx, 10, nil))
	require.Error(t, lake.Put(ctx, 7, nil))
	require.Error(t, lake.Put(ctx, 10, nil))
	require.NoError(t, lake.Put(ctx, 11, nil))
}

func TestCDKDataLakeRejectsMaxUint64Key(t *testing.T) {
	lake := openTestDataLake(t, "store", true, DefaultTestDataLakeConfig(t.TempDir()))
	require.Error(t, lake.Put(context.Background(), math.MaxUint64, nil))
}

func TestCDKDataLakeRejectsInvalidStoreNames(t *testing.T) {
	config := DefaultTestDataLakeConfig(t.TempDir())
	for _, name := range []string{"", "a/b", "..", "a.b", "a b"} {
		_, err := NewCDKDataLake(context.Background(), name, false, config)
		require.Error(t, err, name)
	}
}

func TestCDKDataLakeDeleteWithoutGaps(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", false, DefaultTestDataLakeConfig(t.TempDir()))
	putTestKeys(t, lake, keyRange(1, 5)...)

	require.Error(t, lake.Delete(ctx, 5), "the highest key")
	require.Error(t, lake.Delete(ctx, 3), "not the lowest key")
	require.NoError(t, lake.Delete(ctx, 9), "absent")

	require.NoError(t, lake.Delete(ctx, 1))
	first, ok := lake.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(2), first)
	has, err := lake.Has(ctx, 1)
	require.NoError(t, err)
	require.False(t, has)
	require.NoError(t, lake.Delete(ctx, 1), "already deleted")
}

func TestCDKDataLakeDeleteWithGaps(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	lake, err := NewCDKDataLake(ctx, "store", true, config)
	require.NoError(t, err)
	putTestKeys(t, lake, 10, 20, 30, 40)
	require.NoError(t, lake.Flush(ctx))

	require.Error(t, lake.Delete(ctx, 40), "the highest key")
	require.NoError(t, lake.Delete(ctx, 25), "absent")
	require.NoError(t, lake.Delete(ctx, 20))
	require.NoError(t, lake.Delete(ctx, 10))

	for _, key := range []uint64{10, 20} {
		has, err := lake.Has(ctx, key)
		require.NoError(t, err)
		require.False(t, has, "key %d", key)
	}
	first, ok := lake.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(30), first)

	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())
	storeBucket := openTestStoreBucket(t, config, "store")
	requireObjectExists(t, storeBucket, 10, false)
	requireObjectExists(t, storeBucket, 20, false)
	requireObjectExists(t, storeBucket, 30, true)
}

func TestCDKDataLakePruneBelowDeletesObjects(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	lake, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	putTestKeys(t, lake, keyRange(1, 10)...)

	require.Error(t, lake.PruneBelow(ctx, 11), "would remove the highest key")
	require.NoError(t, lake.PruneBelow(ctx, 6))
	require.NoError(t, lake.PruneBelow(ctx, 3), "already below the first key")

	first, ok := lake.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(6), first)
	_, ok, err = lake.Get(ctx, 5)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())
	storeBucket := openTestStoreBucket(t, config, "store")
	for key := uint64(1); key <= 10; key++ {
		requireObjectExists(t, storeBucket, key, key >= 6)
	}
	require.Equal(t, testStoreMetadata(t, false, 6, 6, 11), readMetadata(t, storeBucket))
}

func TestCDKDataLakeDeletionWatermarkFollowsPruning(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", false, DefaultTestDataLakeConfig(t.TempDir()))
	putTestKeys(t, lake, keyRange(1, 10)...)
	require.NoError(t, lake.Flush(ctx))
	require.Equal(t, uint64(1), lake.GetDeletionWatermark())

	require.NoError(t, lake.PruneBelow(ctx, 6))
	require.LessOrEqual(t, lake.GetDeletionWatermark(), uint64(6))
	require.NoError(t, lake.Flush(ctx))
	require.Equal(t, uint64(6), lake.GetDeletionWatermark())
}

func TestCDKDataLakeDeletionWatermarkWithGaps(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", true, DefaultTestDataLakeConfig(t.TempDir()))
	putTestKeys(t, lake, 10, 20, 30, 40)
	require.NoError(t, lake.Flush(ctx))
	require.Equal(t, uint64(10), lake.GetDeletionWatermark())

	require.NoError(t, lake.Delete(ctx, 20))
	require.NoError(t, lake.Flush(ctx))
	require.Equal(t, uint64(10), lake.GetDeletionWatermark(), "key 10 still exists")

	require.NoError(t, lake.PruneBelow(ctx, 40))
	require.NoError(t, lake.Flush(ctx))
	require.Equal(t, uint64(40), lake.GetDeletionWatermark())
}

func TestCDKDataLakeNewStoreWritesNothingUntilItsFirstPut(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	lake, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	require.NoError(t, lake.Close())

	storeBucket := openTestStoreBucket(t, config, "store")
	_, ok, err := storeBucket.ReadMetadata(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	lake, err = NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	putTestKeys(t, lake, 42)
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())
	require.Equal(t, testStoreMetadata(t, false, 42, 42, 43), readMetadata(t, storeBucket))
}

func TestCDKDataLakeReopenKeepsDurableKeys(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())

	lake, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	putTestKeys(t, lake, keyRange(3, 8)...)
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())

	reopened := openTestDataLake(t, "store", false, config)
	require.Equal(t, uint64(9), reopened.GetDurableWatermark())
	first, ok := reopened.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(3), first)
	value, ok, err := reopened.Get(ctx, 5)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, testValue(5), value)

	require.Error(t, reopened.Put(ctx, 10, nil))
	require.NoError(t, reopened.Put(ctx, 9, nil))
}

func TestCDKDataLakeReopenDeletesKeysAboveAGap(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())

	lake, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	putTestKeys(t, lake, keyRange(1, 5)...)
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())

	// Writes that landed out of order before a crash: 8 never did.
	storeBucket := openTestStoreBucket(t, config, "store")
	for _, key := range []uint64{6, 7, 9, 10} {
		require.NoError(t, storeBucket.Put(ctx, key, testValue(key)))
	}

	reopened, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	require.Equal(t, uint64(8), reopened.GetDurableWatermark())
	require.NoError(t, reopened.Close())
	requireObjectExists(t, storeBucket, 7, true)
	requireObjectExists(t, storeBucket, 9, false)
	requireObjectExists(t, storeBucket, 10, false)
	require.Equal(t, testStoreMetadata(t, false, 1, 1, 8), readMetadata(t, storeBucket))

	reopened = openTestDataLake(t, "store", false, config)
	require.NoError(t, reopened.Put(ctx, 8, nil))
}

func TestCDKDataLakeReopenKeepsKeysAboveTheDurableWatermarkWhenGapsAreAllowed(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())

	lake, err := NewCDKDataLake(ctx, "store", true, config)
	require.NoError(t, err)
	putTestKeys(t, lake, 10)
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())

	storeBucket := openTestStoreBucket(t, config, "store")
	for _, key := range []uint64{30, 50} {
		require.NoError(t, storeBucket.Put(ctx, key, testValue(key)))
	}

	reopened := openTestDataLake(t, "store", true, config)
	require.Equal(t, uint64(51), reopened.GetDurableWatermark())
	first, ok := reopened.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(10), first)
	has, err := reopened.Has(ctx, 30)
	require.NoError(t, err)
	require.True(t, has)
	require.Error(t, reopened.Put(ctx, 40, nil))
	require.NoError(t, reopened.Put(ctx, 51, nil))
}

func TestCDKDataLakeOpenFailsWithCorruptMetadata(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	require.NoError(t, openTestStoreBucket(t, config, "store").WriteMetadata(ctx, []byte("garbage")))

	_, err := NewCDKDataLake(ctx, "store", false, config)
	require.ErrorContains(t, err, "deserialize metadata")
}

func TestCDKDataLakeOpenFailsWithDataButNoMetadata(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	require.NoError(t, openTestStoreBucket(t, config, "store").Put(ctx, 1, nil))

	_, err := NewCDKDataLake(ctx, "store", false, config)
	require.ErrorContains(t, err, "corrupt")
}

func TestCDKDataLakeReopenDropsKeysAfterALostFirstKey(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	storeBucket := openTestStoreBucket(t, config, "store")

	// The first put recorded its key in the metadata, then crashed with only later keys written.
	require.NoError(t, storeBucket.WriteMetadata(ctx, testStoreMetadata(t, false, 100, 100, 100).Serialize()))
	require.NoError(t, storeBucket.Put(ctx, 101, nil))
	require.NoError(t, storeBucket.Put(ctx, 102, nil))

	lake, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	_, ok := lake.GetFirstKey()
	require.False(t, ok)
	require.Error(t, lake.Put(ctx, 99, nil), "below keys scheduled for deletion")
	require.NoError(t, lake.Put(ctx, 100, nil))
	require.NoError(t, lake.Close())

	requireObjectExists(t, storeBucket, 101, false)
	requireObjectExists(t, storeBucket, 102, false)
}

func TestCDKDataLakeReopenFinishesInterruptedPruning(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	storeBucket := openTestStoreBucket(t, config, "store")

	// A prune below 6 was recorded, but none of its deletes happened before the crash.
	require.NoError(t, storeBucket.WriteMetadata(ctx, testStoreMetadata(t, false, 6, 1, 11).Serialize()))
	for key := uint64(1); key <= 10; key++ {
		require.NoError(t, storeBucket.Put(ctx, key, testValue(key)))
	}

	lake, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	first, ok := lake.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(6), first)
	_, ok, err = lake.Get(ctx, 3)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())

	for key := uint64(1); key <= 10; key++ {
		requireObjectExists(t, storeBucket, key, key >= 6)
	}
	require.Equal(t, testStoreMetadata(t, false, 6, 6, 11), readMetadata(t, storeBucket))
}

func TestCDKDataLakeReopenFinishesInterruptedPruningWhenGapsAreAllowed(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	storeBucket := openTestStoreBucket(t, config, "store")

	metadata := testStoreMetadata(t, true, 25, 0, 41)
	require.NoError(t, storeBucket.WriteMetadata(ctx, metadata.Serialize()))
	for _, key := range []uint64{10, 20, 30, 40} {
		require.NoError(t, storeBucket.Put(ctx, key, testValue(key)))
	}

	lake, err := NewCDKDataLake(ctx, "store", true, config)
	require.NoError(t, err)
	first, ok := lake.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(30), first)
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())

	requireObjectExists(t, storeBucket, 10, false)
	requireObjectExists(t, storeBucket, 20, false)
	requireObjectExists(t, storeBucket, 30, true)
}

func TestCDKDataLakeOpenFailsWhenGapAllowanceChanges(t *testing.T) {
	for _, allowGaps := range []bool{false, true} {
		ctx := context.Background()
		config := DefaultTestDataLakeConfig(t.TempDir())

		lake, err := NewCDKDataLake(ctx, "store", allowGaps, config)
		require.NoError(t, err)
		putTestKeys(t, lake, keyRange(1, 5)...)
		require.NoError(t, lake.Flush(ctx))
		require.NoError(t, lake.Close())

		// A key above the durable watermark, which repair would delete if it ran.
		storeBucket := openTestStoreBucket(t, config, "store")
		require.NoError(t, storeBucket.Put(ctx, 7, testValue(7)))

		_, err = NewCDKDataLake(ctx, "store", !allowGaps, config)
		require.ErrorContains(t, err, "allowGaps", "created with allowGaps=%t", allowGaps)
		for _, key := range append(keyRange(1, 5), 7) {
			requireObjectExists(t, storeBucket, key, true)
		}
		expected := testStoreMetadata(t, allowGaps, 1, expectedDeletionWatermark(allowGaps, 1), 6)
		require.Equal(t, expected, readMetadata(t, storeBucket))
	}
}

func TestCDKDataLakeRollBackWithoutGaps(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	lake, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	putTestKeys(t, lake, keyRange(1, 10)...)
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())

	require.NoError(t, RollBackCDKDataLake(ctx, "store", false, config, 6))
	storeBucket := openTestStoreBucket(t, config, "store")
	for key := uint64(1); key <= 10; key++ {
		requireObjectExists(t, storeBucket, key, key < 6)
	}
	require.Equal(t, testStoreMetadata(t, false, 1, 1, 6), readMetadata(t, storeBucket))

	reopened := openTestDataLake(t, "store", false, config)
	require.Equal(t, uint64(6), reopened.GetDurableWatermark())
	first, ok := reopened.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Error(t, reopened.Put(ctx, 7, nil))
	putTestKeys(t, reopened, 6, 7)
	require.NoError(t, reopened.Flush(ctx))
	require.Equal(t, keyRange(1, 7), iterateTestKeys(t, reopened, 0, false, false))
}

func TestCDKDataLakeRollBackWithGaps(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	lake, err := NewCDKDataLake(ctx, "store", true, config)
	require.NoError(t, err)
	putTestKeys(t, lake, 10, 20, 30, 40)
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())

	require.Error(t, RollBackCDKDataLake(ctx, "store", false, config, 25), "gap setting differs")
	require.NoError(t, RollBackCDKDataLake(ctx, "store", true, config, 25))
	storeBucket := openTestStoreBucket(t, config, "store")
	requireObjectExists(t, storeBucket, 20, true)
	requireObjectExists(t, storeBucket, 30, false)
	requireObjectExists(t, storeBucket, 40, false)
	expected := testStoreMetadata(t, true, 10, expectedDeletionWatermark(true, 10), 21)
	require.Equal(t, expected, readMetadata(t, storeBucket))

	reopened := openTestDataLake(t, "store", true, config)
	require.Equal(t, uint64(21), reopened.GetDurableWatermark())
	require.Error(t, reopened.Put(ctx, 15, nil))
	putTestKeys(t, reopened, 26)
	require.NoError(t, reopened.Flush(ctx))
	require.Equal(t, []uint64{10, 20, 26}, iterateTestKeys(t, reopened, 0, false, false))
}

func TestCDKDataLakeRollBackEverything(t *testing.T) {
	for _, allowGaps := range []bool{false, true} {
		for _, watermark := range []uint64{5, 0} {
			name := fmt.Sprintf("allowGaps=%t watermark=%d", allowGaps, watermark)
			ctx := context.Background()
			config := DefaultTestDataLakeConfig(t.TempDir())
			lake, err := NewCDKDataLake(ctx, "store", allowGaps, config)
			require.NoError(t, err, name)
			putTestKeys(t, lake, keyRange(5, 10)...)
			require.NoError(t, lake.Flush(ctx), name)
			require.NoError(t, lake.Close(), name)

			require.NoError(t, RollBackCDKDataLake(ctx, "store", allowGaps, config, watermark), name)
			storeBucket := openTestStoreBucket(t, config, "store")
			for key := uint64(5); key <= 10; key++ {
				requireObjectExists(t, storeBucket, key, false)
			}
			expected := testStoreMetadata(
				t, allowGaps, watermark, expectedDeletionWatermark(allowGaps, watermark), watermark)
			require.Equal(t, expected, readMetadata(t, storeBucket), name)

			reopened := openTestDataLake(t, "store", allowGaps, config)
			_, ok := reopened.GetFirstKey()
			require.False(t, ok, name)
			require.Equal(t, watermark, reopened.GetDurableWatermark(), name)
			putTestKeys(t, reopened, 7)
			require.NoError(t, reopened.Flush(ctx), name)
			require.Equal(t, []uint64{7}, iterateTestKeys(t, reopened, 0, false, false), name)
		}
	}
}

// interruptTestRollback writes keys 1 through 10, then records a rollback to 6 that deleted only keys 9 and 10
// before it was interrupted.
func interruptTestRollback(t *testing.T, config DataLakeConfig) *storeBucket {
	ctx := context.Background()
	lake, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	putTestKeys(t, lake, keyRange(1, 10)...)
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())

	storeBucket := openTestStoreBucket(t, config, "store")
	metadata := testStoreMetadata(t, false, 1, 1, 11)
	require.NoError(t, metadata.StartRollback(6))
	require.NoError(t, storeBucket.WriteMetadata(ctx, metadata.Serialize()))
	require.NoError(t, storeBucket.Delete(ctx, 10))
	require.NoError(t, storeBucket.Delete(ctx, 9))
	return storeBucket
}

func TestCDKDataLakeRollBackFinishesAnInterruptedRollback(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	storeBucket := interruptTestRollback(t, config)

	// The interrupted rollback to 6 is finished before the rollback to 3.
	require.NoError(t, RollBackCDKDataLake(ctx, "store", false, config, 3))
	for key := uint64(1); key <= 10; key++ {
		requireObjectExists(t, storeBucket, key, key < 3)
	}
	require.Equal(t, testStoreMetadata(t, false, 1, 1, 3), readMetadata(t, storeBucket))
}

func TestCDKDataLakeOpenFinishesAnInterruptedRollback(t *testing.T) {
	config := DefaultTestDataLakeConfig(t.TempDir())
	storeBucket := interruptTestRollback(t, config)

	lake, err := NewCDKDataLake(context.Background(), "store", false, config)
	require.NoError(t, err)
	require.Equal(t, uint64(6), lake.GetDurableWatermark())
	require.Equal(t, keyRange(1, 5), iterateTestKeys(t, lake, 0, false, false))
	require.NoError(t, lake.Close())

	for key := uint64(1); key <= 10; key++ {
		requireObjectExists(t, storeBucket, key, key < 6)
	}
	require.Equal(t, testStoreMetadata(t, false, 1, 1, 6), readMetadata(t, storeBucket))
}

func TestCDKDataLakeStoresShareABucket(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())
	blocks := openTestDataLake(t, "blocks", false, config)
	receipts := openTestDataLake(t, "receipts", false, config)

	putTestKeys(t, blocks, keyRange(1, 3)...)
	putTestKeys(t, receipts, keyRange(7, 9)...)
	require.NoError(t, blocks.Flush(ctx))
	require.NoError(t, receipts.Flush(ctx))

	first, ok := blocks.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	first, ok = receipts.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(7), first)
	_, ok, err := blocks.Get(ctx, 8)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCDKDataLakeIteratorWithoutGaps(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", false, DefaultTestDataLakeConfig(t.TempDir()))
	putTestKeys(t, lake, keyRange(1, 10)...)
	require.NoError(t, lake.Flush(ctx))

	cases := []struct {
		startKey uint64
		reverse  bool
		expected []uint64
	}{
		{startKey: 0, reverse: false, expected: keyRange(1, 10)},
		{startKey: 5, reverse: false, expected: keyRange(5, 10)},
		{startKey: 11, reverse: false, expected: nil},
		{startKey: 7, reverse: true, expected: []uint64{7, 6, 5, 4, 3, 2, 1}},
		{startKey: 100, reverse: true, expected: []uint64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}},
		{startKey: 0, reverse: true, expected: nil},
	}
	for _, prefetch := range []bool{false, true} {
		for _, c := range cases {
			name := fmt.Sprintf("start=%d reverse=%t prefetch=%t", c.startKey, c.reverse, prefetch)
			require.Equal(t, c.expected, iterateTestKeys(t, lake, c.startKey, prefetch, c.reverse), name)
		}
	}
}

func TestCDKDataLakeIteratorWithGaps(t *testing.T) {
	ctx := context.Background()
	lake := openTestDataLake(t, "store", true, DefaultTestDataLakeConfig(t.TempDir()))
	putTestKeys(t, lake, 10, 20, 30)
	require.NoError(t, lake.Flush(ctx))

	for _, prefetch := range []bool{false, true} {
		require.Equal(t, []uint64{10, 20, 30}, iterateTestKeys(t, lake, 0, prefetch, false))
		require.Equal(t, []uint64{20, 30}, iterateTestKeys(t, lake, 15, prefetch, false))
		require.Equal(t, []uint64{20, 10}, iterateTestKeys(t, lake, 25, prefetch, true))
		require.Equal(t, []uint64{30, 20, 10}, iterateTestKeys(t, lake, 30, prefetch, true))
		require.Empty(t, iterateTestKeys(t, lake, 5, prefetch, true))
	}
}

func TestCDKDataLakeIteratorOfAnEmptyStore(t *testing.T) {
	lake := openTestDataLake(t, "store", false, DefaultTestDataLakeConfig(t.TempDir()))
	require.Empty(t, iterateTestKeys(t, lake, 0, true, false))
}

// iterateTestKeys returns the keys an iterator visits, checking that each value is testValue().
func iterateTestKeys(t *testing.T, lake DataLake, startKey uint64, prefetch bool, reverse bool) []uint64 {
	ctx := context.Background()
	iterator, err := lake.Iterator(ctx, startKey, prefetch, reverse)
	require.NoError(t, err)
	defer func() { require.NoError(t, iterator.Close()) }()

	var keys []uint64
	for {
		ok, err := iterator.Next(ctx)
		require.NoError(t, err)
		if !ok {
			return keys
		}
		value, err := iterator.Value(ctx)
		require.NoError(t, err)
		require.Equal(t, testValue(iterator.Key()), value)
		keys = append(keys, iterator.Key())
	}
}

func TestCDKDataLakeRetriesTransientFailures(t *testing.T) {
	ctx := context.Background()
	faulty, config := newFaultyBucket(t)
	failures := 0
	faulty.SetHook(func(_ context.Context, op string, key string) error {
		if op == "write" && !strings.HasSuffix(key, metadataObjectName) && failures < 5 {
			failures++
			return &injectedError{code: gcerrors.Unknown}
		}
		return nil
	})
	config.WriterCount = 1
	lake := openTestDataLake(t, "store", false, config)

	putTestKeys(t, lake, keyRange(1, 3)...)
	require.NoError(t, lake.Flush(ctx))
	require.Equal(t, 5, failures)
	for key := uint64(1); key <= 3; key++ {
		value, ok, err := lake.Get(ctx, key)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, testValue(key), value)
	}
}

func TestCDKDataLakePermanentFailureFailsTheStore(t *testing.T) {
	ctx := context.Background()
	faulty, config := newFaultyBucket(t)
	lake := openTestDataLake(t, "store", false, config)
	faulty.SetHook(func(_ context.Context, op string, key string) error {
		if op == "write" && !strings.HasSuffix(key, metadataObjectName) {
			return &injectedError{code: gcerrors.PermissionDenied}
		}
		return nil
	})

	putTestKeys(t, lake, 1)
	require.ErrorContains(t, lake.Flush(ctx), "PermissionDenied")
	require.ErrorContains(t, lake.Put(ctx, 2, nil), "PermissionDenied")
	_, _, err := lake.Get(ctx, 1)
	require.ErrorContains(t, err, "PermissionDenied")
}

func TestCDKDataLakeFailsAfterMaxRetries(t *testing.T) {
	ctx := context.Background()
	faulty, config := newFaultyBucket(t)
	attempts := 0
	faulty.SetHook(func(_ context.Context, op string, key string) error {
		if op == "write" && !strings.HasSuffix(key, metadataObjectName) {
			attempts++
			return &injectedError{code: gcerrors.Unknown}
		}
		return nil
	})
	config.InfiniteRetries = false
	config.MaxRetries = 2
	lake := openTestDataLake(t, "store", false, config)

	putTestKeys(t, lake, 1)
	require.ErrorContains(t, lake.Flush(ctx), "after 2 retries")
	require.Equal(t, 3, attempts)
}

func TestCDKDataLakeWithoutRetriesFailsOnTheFirstTransientError(t *testing.T) {
	ctx := context.Background()
	faulty, config := newFaultyBucket(t)
	attempts := 0
	faulty.SetHook(func(_ context.Context, op string, key string) error {
		if op == "write" && !strings.HasSuffix(key, metadataObjectName) {
			attempts++
			return &injectedError{code: gcerrors.Unknown}
		}
		return nil
	})
	config.InfiniteRetries = false
	config.MaxRetries = 0
	lake := openTestDataLake(t, "store", false, config)

	putTestKeys(t, lake, 1)
	require.ErrorContains(t, lake.Flush(ctx), "after 0 retries")
	require.Equal(t, 1, attempts)
}

func TestCDKDataLakeAppliesBackPressure(t *testing.T) {
	ctx := context.Background()
	faulty, config := newFaultyBucket(t)
	release := make(chan struct{})
	faulty.SetHook(func(hookCtx context.Context, op string, key string) error {
		if op == "write" && !strings.HasSuffix(key, metadataObjectName) {
			select {
			case <-release:
			case <-hookCtx.Done():
				return hookCtx.Err()
			}
		}
		return nil
	})
	config.WriterCount = 1
	config.WorkBufferSize = 2
	config.RequestBufferSize = 1
	lake := openTestDataLake(t, "store", false, config)

	// The writer holds put 1, its queue holds puts 2 and 3, and the control loop waits to queue put 4.
	putTestKeys(t, lake, 1, 2, 3, 4)

	// Put 5 fills the control loop's request buffer.
	put5 := make(chan error, 1)
	go func() { put5 <- lake.Put(ctx, 5, nil) }()
	require.Eventually(t, func() bool { return len(lake.requests) == 1 }, time.Second, time.Millisecond)

	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, lake.Put(timeoutCtx, 6, nil), context.DeadlineExceeded)

	close(release)
	require.NoError(t, <-put5)
	putTestKeys(t, lake, 6)
	require.NoError(t, lake.Flush(ctx))
	require.Equal(t, uint64(7), lake.GetDurableWatermark())
}

func TestCDKDataLakeCallsFailAfterClose(t *testing.T) {
	ctx := context.Background()
	lake, err := NewCDKDataLake(ctx, "store", false, DefaultTestDataLakeConfig(t.TempDir()))
	require.NoError(t, err)
	putTestKeys(t, lake, 1)
	require.NoError(t, lake.Close())

	require.Error(t, lake.Put(ctx, 2, nil))
	require.Error(t, lake.Flush(ctx))
	_, _, err = lake.Get(ctx, 1)
	require.Error(t, err)
	require.Equal(t, uint64(0), lake.GetDurableWatermark())
}

func TestCDKDataLakeReopenKeepsPrunedRange(t *testing.T) {
	ctx := context.Background()
	config := DefaultTestDataLakeConfig(t.TempDir())

	lake, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	putTestKeys(t, lake, keyRange(1, 20)...)
	require.NoError(t, lake.PruneBelow(ctx, 5))
	require.NoError(t, lake.Flush(ctx))
	require.NoError(t, lake.Close())

	reopened, err := NewCDKDataLake(ctx, "store", false, config)
	require.NoError(t, err)
	first, ok := reopened.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(5), first)
	require.Equal(t, uint64(21), reopened.GetDurableWatermark())
	require.Equal(t, keyRange(5, 20), iterateTestKeys(t, reopened, 0, true, false))
	require.NoError(t, reopened.Close())

	storeBucket := openTestStoreBucket(t, config, "store")
	for key := uint64(1); key <= 20; key++ {
		requireObjectExists(t, storeBucket, key, key >= 5)
	}
}

// testStream is a DataLakeStream over a byte slice that records how it was used.
type testStream struct {
	// The stream's content.
	data []byte

	// Reads the content from the current position.
	reader *bytes.Reader

	// If set, every read fails with it.
	readErr error

	// The number of calls to Reset().
	resets int

	// True once Close() has been called.
	closed bool
}

// newTestStream returns a stream over data.
func newTestStream(data []byte) *testStream {
	return &testStream{data: data, reader: bytes.NewReader(data)}
}

func (s *testStream) Read(p []byte) (int, error) {
	if s.readErr != nil {
		return 0, s.readErr
	}
	// io.Reader callers compare against io.EOF directly, so the reader's error must pass through unwrapped.
	return s.reader.Read(p)
}

func (s *testStream) Reset() error {
	s.resets++
	s.reader = bytes.NewReader(s.data)
	return nil
}

func (s *testStream) Close() error {
	s.closed = true
	return nil
}
