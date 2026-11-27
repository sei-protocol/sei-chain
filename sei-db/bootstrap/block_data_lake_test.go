package bootstrap

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/cdkdatalake"
)

// dataObjectCount returns the number of objects other than its metadata that the data lake store storeName holds
// in the bucket bucketURL names, or -1 if the bucket cannot be listed.
func dataObjectCount(ctx context.Context, bucketURL string, storeName string) int {
	bucket, err := blob.OpenBucket(ctx, bucketURL)
	if err != nil {
		return -1
	}
	defer func() { _ = bucket.Close() }()
	count := 0
	objects := bucket.List(&blob.ListOptions{Prefix: storeName + "/"})
	for {
		object, err := objects.Next(ctx)
		if errors.Is(err, io.EOF) {
			return count
		}
		if err != nil {
			return -1
		}
		if object.Key != storeName+"/metadata" {
			count++
		}
	}
}

// TestBlockDataLakeCopiesTheBlockStore pins the wiring: a manager given a data lake copies the blocks its block store
// holds into it, and Close leaves every copy durable.
func TestBlockDataLakeCopiesTheBlockStore(t *testing.T) {
	lakeConfig := cdkdatalake.DefaultTestDataLakeConfig(t.TempDir())
	cfg := newHome(t, func(cfg *config.GigaStorageConfig) {
		cfg.DataLakeConfig = &lakeConfig
	})
	writeBlockLedger(t, cfg, 3)

	manager, err := NewGigaStorageManager(t.Context(), cfg)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return dataObjectCount(t.Context(), lakeConfig.BucketURL, "blocks") == 3 },
		time.Minute, time.Millisecond)
	require.NoError(t, manager.Close())

	blocks, err := cdkdatalake.NewCDKDataLake(t.Context(), "blocks", false, lakeConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, blocks.Close()) }()
	first, ok := blocks.GetFirstKey()
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(4), blocks.GetDurableWatermark())

	appQCs, err := cdkdatalake.NewCDKDataLake(t.Context(), "app_qcs", false, lakeConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, appQCs.Close()) }()
	_, ok = appQCs.GetFirstKey()
	require.False(t, ok, "the ledger holds no AppQC to copy")
}

// TestAnInvalidDataLakeConfigIsRefused pins that the data lake config is validated with the rest.
func TestAnInvalidDataLakeConfigIsRefused(t *testing.T) {
	cfg := newHome(t, func(cfg *config.GigaStorageConfig) {
		lakeConfig := cdkdatalake.DefaultTestDataLakeConfig(t.TempDir())
		lakeConfig.BucketURL = ""
		cfg.DataLakeConfig = &lakeConfig
	})
	_, err := NewGigaStorageManager(t.Context(), cfg)
	require.ErrorContains(t, err, "data lake config is invalid")
}
