package walrussim

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// testSimConfig returns a short run: small pods so several are built, retention tight enough that collection
// runs, and snapshots often enough that the floor advances more than once.
func testSimConfig(t *testing.T) *Config {
	t.Helper()

	config := DefaultConfig()
	config.DataDir = t.TempDir()
	config.DeleteDataDirOnStartup = false
	config.CannedRandomSize = 1 << 20
	config.KeyClasses = []KeyClass{
		{KeyCount: 20, Period: 1},
		{KeyCount: 400, Period: 20},
		{KeyCount: 2000, Period: 500},
	}
	config.NeverWrittenKeyCount = 500
	config.DeleteRate = 50
	config.Walrus.TargetPodSize = 64 * 1024
	config.Walrus.RetentionBlocks = 400
	config.SnapshotIntervalSeconds = 0.2
	config.BlockCount = 1500
	config.MaxBlocksPerSecond = 800
	config.ReadConcurrency = 4
	config.ReadsPerSecond = 800
	config.NeverWrittenReadFraction = 0.3
	config.MetricsAddr = ""
	config.ConsoleUpdateIntervalSeconds = 3600
	return config
}

func TestWalrusSimRunsWithoutMismatches(t *testing.T) {
	config := testSimConfig(t)

	simulator, err := NewWalrusSim(context.Background(), config)
	require.NoError(t, err)
	defer func() { require.NoError(t, simulator.Close()) }()

	require.NoError(t, simulator.Run())

	require.Equal(t, config.BlockCount, simulator.blocksWritten.Load())
	require.Positive(t, simulator.reads.Load(), "the readers never ran")
	require.Zero(t, simulator.mismatches.Load(), "the engine disagreed with the workload")

	ok, first, last, err := simulator.engine.QueryableBounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Greater(t, first, config.FirstBlock, "retention never collected anything")
	require.Greater(t, last, first)
}

func TestWalrusSimWriteOnly(t *testing.T) {
	config := testSimConfig(t)
	config.EnableSnapshots = false
	config.ReadConcurrency = 0
	config.NeverWrittenReadFraction = 0
	config.BlockCount = 500

	simulator, err := NewWalrusSim(context.Background(), config)
	require.NoError(t, err)
	defer func() { require.NoError(t, simulator.Close()) }()

	require.NoError(t, simulator.Run())
	require.Equal(t, config.BlockCount, simulator.blocksWritten.Load())
	require.Zero(t, simulator.reads.Load())

	// With no snapshot to terminate a walk, nothing may be deleted however far the retention window slides.
	ok, first, _, err := simulator.engine.QueryableBounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, config.FirstBlock, first)
}

func TestWalrusSimStopsOnContextCancel(t *testing.T) {
	config := testSimConfig(t)
	config.BlockCount = 0
	config.MaxBlocksPerSecond = 200

	runContext, cancel := context.WithCancel(context.Background())
	simulator, err := NewWalrusSim(runContext, config)
	require.NoError(t, err)
	defer func() { require.NoError(t, simulator.Close()) }()

	go func() {
		for simulator.blocksWritten.Load() < 50 {
			continue
		}
		cancel()
	}()

	require.NoError(t, simulator.Run())
	require.Positive(t, simulator.blocksWritten.Load())
}
