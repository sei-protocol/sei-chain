package datalake

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob"
	"gocloud.dev/blob/memblob"
)

// listTestKeys lists every key in a store's part of a bucket, page by page.
func listTestKeys(t *testing.T, storeBucket *storeBucket) []uint64 {
	var keys []uint64
	pageToken := blob.FirstPageToken
	for len(pageToken) > 0 {
		names, nextPageToken, err := storeBucket.ListPage(context.Background(), pageToken)
		require.NoError(t, err)
		for _, name := range names {
			key, err := parseInverseKey(name)
			require.NoError(t, err)
			keys = append(keys, key)
		}
		pageToken = nextPageToken
	}
	return keys
}

func TestStoreBucketListsKeysFromHighestToLowest(t *testing.T) {
	ctx := context.Background()
	bucket := memblob.OpenBucket(nil)
	defer func() { require.NoError(t, bucket.Close()) }()
	storeBucket := newStoreBucket(bucket, "store")

	keys := []uint64{9, 0, 10, 12345, math.MaxUint64 - 1, 1}
	for _, key := range keys {
		require.NoError(t, storeBucket.Put(ctx, key, []byte{byte(key)}))
	}
	require.NoError(t, storeBucket.WriteMetadata(ctx, newStoreMetadata(false).Serialize()))

	require.Equal(t, []uint64{math.MaxUint64 - 1, 12345, 10, 9, 1, 0}, listTestKeys(t, storeBucket))
}

func TestStoreBucketAreIsolatedByStoreName(t *testing.T) {
	ctx := context.Background()
	bucket := memblob.OpenBucket(nil)
	defer func() { require.NoError(t, bucket.Close()) }()
	first := newStoreBucket(bucket, "a")
	second := newStoreBucket(bucket, "ab")
	require.NoError(t, first.Put(ctx, 1, []byte("a")))
	require.NoError(t, second.Put(ctx, 2, []byte("ab")))

	require.Equal(t, []uint64{1}, listTestKeys(t, first))
}

func TestParseInverseKeyRejectsMalformedNames(t *testing.T) {
	malformed := []string{"", "123", "1844674407370955161x", "184467440737095516150", "99999999999999999999"}
	for _, name := range malformed {
		_, err := parseInverseKey(name)
		require.Error(t, err, name)
	}
	key, err := parseInverseKey("18446744073709551614")
	require.NoError(t, err)
	require.Equal(t, uint64(1), key)
}
