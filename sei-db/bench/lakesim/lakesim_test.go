package lakesim

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/cdkdatalake"
)

// testConfig returns a configuration for a short run against a file bucket in dir.
func testConfig(dir string) *LakesimConfig {
	config := DefaultLakesimConfig()
	config.BucketURL = (&url.URL{Scheme: "file", Path: filepath.ToSlash(dir)}).String()
	config.Lanes = []LaneConfig{{ValueSize: 1024}, {ValueSize: 64}, {ValueSize: 0}}
	config.MaxBlocks = 300
	config.RetainedBlocks = 50
	config.MaxDeletionBacklog = 20
	config.ParallelWrites = 4
	config.QueuedWrites = 8
	config.MetadataWriteIntervalMs = 5
	config.RetryBackoffMs = 1
	config.WatermarkPollIntervalMs = 1
	config.CannedRandomSize = 1 << 20
	config.ConsoleUpdateIntervalSeconds = 0
	config.MetricsAddr = ""
	return config
}

// runLakesim runs the benchmark described by config to completion.
func runLakesim(t *testing.T, config *LakesimConfig) {
	require.NoError(t, config.Validate())
	sim, err := NewLakesim(context.Background(), config, NewLakesimMetrics())
	require.NoError(t, err)
	require.NoError(t, sim.Wait())
}

// requireLaneBounds checks that every lane's data lake holds exactly the keys from firstKey up to nextKey, and that
// every key below firstKey has been deleted from the bucket.
func requireLaneBounds(t *testing.T, config *LakesimConfig, firstKey uint64, nextKey uint64) {
	ctx := context.Background()
	for i := range config.Lanes {
		lake, err := cdkdatalake.NewCDKDataLake(ctx, fmt.Sprintf("lane%d", i), false, config.DataLakeConfig())
		require.NoError(t, err)
		require.Equal(t, nextKey, lake.GetDurableWatermark(), "lane %d", i)
		first, ok := lake.GetFirstKey()
		require.True(t, ok)
		require.Equal(t, firstKey, first, "lane %d", i)
		require.Equal(t, firstKey, lake.GetDeletionWatermark(), "lane %d", i)
		require.NoError(t, lake.Close())
	}
}

func TestLakesimWritesEveryLaneAndPrunesOldBlocks(t *testing.T) {
	config := testConfig(t.TempDir())
	runLakesim(t, config)
	requireLaneBounds(t, config, 250, 300)
}

func TestLakesimResumesWhereThePreviousRunStopped(t *testing.T) {
	config := testConfig(t.TempDir())
	runLakesim(t, config)
	config.MaxBlocks = 100
	runLakesim(t, config)
	requireLaneBounds(t, config, 350, 400)
}

func TestLakesimKeepsEveryBlockWithoutRetention(t *testing.T) {
	config := testConfig(t.TempDir())
	config.RetainedBlocks = 0
	runLakesim(t, config)
	requireLaneBounds(t, config, 0, 300)
}

func TestLakesimThrottlesBlocks(t *testing.T) {
	config := testConfig(t.TempDir())
	config.MaxBlocks = 20
	config.BlocksPerSecond = 100
	start := time.Now()
	runLakesim(t, config)
	// The first block starts at once, and each later one at least 10ms after the one before it.
	require.GreaterOrEqual(t, time.Since(start), 190*time.Millisecond)
	requireLaneBounds(t, config, 0, 20)
}
