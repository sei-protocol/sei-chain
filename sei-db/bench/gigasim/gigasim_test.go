package gigasim

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/stretchr/testify/require"
)

// blocksToProcess is how far a test run is taken past setup. It is small enough to stay quick and
// large enough to cross the checkpoint and hash-lag boundaries the test config sets.
const blocksToProcess = 20

// testConfig returns a configuration sized for a test: a tiny account population, no metrics server,
// and a checkpoint schedule tight enough that a short run crosses it.
func testConfig(t *testing.T) *GigasimConfig {
	t.Helper()

	config := DefaultGigasimConfig()
	config.DataDir = filepath.Join(t.TempDir(), "data")
	config.LogDir = filepath.Join(t.TempDir(), "logs")

	config.TransactionsPerBlock = 10
	config.BytesPerTransaction = 64
	config.NumberOfHotAccounts = 5
	config.MinimumNumberOfColdAccounts = 20
	config.MinimumNumberOfDormantAccounts = 10
	config.MinimumNumberOfErc20Contracts = 20
	config.HotErc20ContractSetSize = 5
	config.CannedRandomSize = 1 << 20

	config.ThreadsPerCore = 0
	config.ConstantThreadCount = 2
	config.HashLagBlocks = 4
	config.CheckpointBlockInterval = 5
	config.PruneIntervalSeconds = 1
	config.FlushIntervalBlocks = 5

	config.MetricsAddr = ""
	config.BackgroundMetricsScrapeInterval = 0
	config.EnableSuspension = false
	config.ConsoleUpdateIntervalSeconds = 3600

	require.NoError(t, config.Validate())
	return config
}

// runBlocks runs a benchmark until it has taken blocksToProcess blocks through the whole stack, then
// shuts it down. It returns the newest height completed, read after the run loop has stopped so that
// it is the height the run actually finished on rather than one sampled while it was still moving.
func runBlocks(t *testing.T, config *GigasimConfig) int64 {
	t.Helper()

	benchmark, err := NewGigaSim(t.Context(), config, NewGigasimMetrics())
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return benchmark.BlocksProcessed() >= blocksToProcess
	}, time.Minute, 10*time.Millisecond, "the benchmark did not process %d blocks", blocksToProcess)

	require.NoError(t, benchmark.Close())
	return benchmark.HighestBlock()
}

func TestEveryStoreAdvancesTogether(t *testing.T) {
	config := testConfig(t)
	highest := runBlocks(t, config)
	require.Positive(t, highest)

	storageConfig, err := config.storageConfig()
	require.NoError(t, err)

	// Reopening runs recovery, which refuses a set of stores it cannot bring onto one height. That it
	// opens at all is the assertion that the pipeline left them consistent.
	manager, err := bootstrap.NewGigaStorageManager(t.Context(), storageConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, manager.Close()) }()

	// A block is counted only once it has cleared every store, so the ledger holding exactly the
	// heights the run counted is what says no block was left half-written across the stack.
	blockStoreHead, err := manager.BlockStore().GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(highest), blockStoreHead, //nolint:gosec // test heights are small
		"the block store should hold every block the benchmark completed, and no more")

	view := manager.StateDB().OpenView()
	defer view.Close()
	require.Equal(t, highest, view.GetBlockHeight(),
		"the state DB should be committed to the same height as the block store")

	require.NotNil(t, manager.ReceiptDB(), "receipts were enabled, so the receipt store should be open")
	require.NotNil(t, manager.SS(), "the state store was enabled, so it should be open")
	require.True(t, dirHasContents(t, storageConfig.ReceiptDBConfig.DBDirectory),
		"receipts were enabled, so the receipt store should hold data")
}

func TestDisabledStoresAreNeverWritten(t *testing.T) {
	config := testConfig(t)
	config.EnableStateStore = false
	config.EnableReceiptStore = false

	require.Positive(t, runBlocks(t, config))

	storageConfig, err := config.storageConfig()
	require.NoError(t, err)

	// A store that is never opened leaves no directory behind, which is the durable evidence that
	// nothing was written to it.
	require.NoDirExists(t, storageConfig.ReceiptDBConfig.DBDirectory)
	require.NoDirExists(t, storageConfig.SSConfig.EVMDBDirectory)

	manager, err := bootstrap.NewGigaStorageManager(t.Context(), storageConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, manager.Close()) }()

	require.Nil(t, manager.ReceiptDB(), "receipts were disabled, so no receipt store should be open")
	require.Nil(t, manager.SS(), "the state store was disabled, so it should not be open")
}

// TestGenerationRunsAheadOfExecution pins the shape of the pipeline: the generator writes a block to
// the ledger and hands it on, so the ledger leads the state DB by the blocks still in flight.
func TestGenerationRunsAheadOfExecution(t *testing.T) {
	config := testConfig(t)
	config.StagedBlockQueueSize = 8

	benchmark, err := NewGigaSim(t.Context(), config, NewGigasimMetrics())
	require.NoError(t, err)
	defer func() { require.NoError(t, benchmark.Close()) }()

	// Execution is the slower side, so the ledger is normally ahead. Sampling until it is caught in that
	// state avoids depending on the exact moment the two are level.
	require.Eventually(t, func() bool {
		ledger, err := benchmark.storage.BlockStore().GetLatestBlock()
		require.NoError(t, err)
		return int64(ledger) > benchmark.HighestBlock() //nolint:gosec // test heights are small
	}, time.Minute, time.Millisecond, "the ledger never led the state DB, so generation is not running ahead")
}

func TestARunResumesWhereTheLastOneStopped(t *testing.T) {
	config := testConfig(t)
	first := runBlocks(t, config)

	second := runBlocks(t, config)
	require.Greater(t, second, first, "a second run should append to the first rather than restart it")
}

// dirHasContents reports whether a directory exists and holds at least one entry.
func dirHasContents(t *testing.T, dir string) bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false
	}
	require.NoError(t, err)
	return len(entries) > 0
}
