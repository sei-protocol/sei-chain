package builder

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// hashLoggingConfig returns a config whose vault is dir/vault and whose hash log is hashLogDir(dir).
func hashLoggingConfig(dir string) AppHashBuilderConfig {
	config := DefaultAppHashBuilderConfig()
	config.ChainID = testChainID
	config.HashVaultConfig.Path = filepath.Join(dir, "vault")
	config.HashLogEnabled = true
	config.HashLogConfig.Path = hashLogDir(dir)
	config.HashLogConfig.Version = "v1.0.0"
	return config
}

// hashLogDir returns the hash log directory of a builder opened by openHashLoggingBuilder() on dir.
func hashLogDir(dir string) string {
	return filepath.Join(dir, "hash.log")
}

// openHashLoggingBuilder opens a builder with the config hashLoggingConfig() gives for dir, closed when the test
// ends.
func openHashLoggingBuilder(t *testing.T, dir string) *StandardAppHashBuilder {
	t.Helper()
	b, err := NewStandardAppHashBuilder(hashLoggingConfig(dir))
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// testComponentHashes returns the debugging hashes tests report with the state hash of blockHeight.
func testComponentHashes(blockHeight uint64) map[string][]byte {
	return map[string][]byte{hashlog.FlatKVDBHashPrefix + "storage": {byte(blockHeight)}}
}

// reportLoggedBlock reports every input for blockHeight, with testComponentHashes() alongside the state hash. The
// BUD is reported with budSalt.
func reportLoggedBlock(t *testing.T, b AppHashBuilder, blockHeight uint64, budSalt byte) {
	t.Helper()
	ctx := context.Background()
	stateHash := inputValue(stateHashInput, blockHeight, 0)
	require.NoError(t, b.ReportStateHash(ctx, blockHeight, stateHash, testComponentHashes(blockHeight)))
	reportInput(t, b, blockHashInput, blockHeight, 0)
	reportInput(t, b, receiptHashInput, blockHeight, 0)
	reportInput(t, b, budInput, blockHeight, budSalt)
}

// loggedRow returns the hash log row the builder writes for record when componentHashes was reported with it.
func loggedRow(record *apphash.AppHashData, componentHashes map[string][]byte) map[string][]byte {
	row := map[string][]byte{
		appHashColumn:         hashBytes(record.AppHash()),
		previousAppHashColumn: hashBytes(record.PreviousAppHash()),
		blockHashColumn:       hashBytes(record.BlockHash()),
		stateHashColumn:       hashBytes(record.StateHash()),
		budColumn:             hashBytes(record.BUD()),
		receiptHashColumn:     hashBytes(record.ReceiptHash()),
	}
	for name, hash := range componentHashes {
		row[name] = hash
	}
	return row
}

// requireLoggedRow requires the hash log in dir to hold exactly one row for record's block, the one loggedRow()
// gives.
func requireLoggedRow(t *testing.T, dir string, record *apphash.AppHashData) {
	t.Helper()
	logs, err := hashlog.ReadHashForBlock(hashLogDir(dir), record.BlockHeight())
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, loggedRow(record, testComponentHashes(record.BlockHeight())), logs[0].Hashes)
}

func TestHashLogRecordsEachAppHashBuilt(t *testing.T) {
	dir := t.TempDir()
	b := openHashLoggingBuilder(t, dir)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	_, published := registerListener(t, b)
	for height := uint64(1); height <= 3; height++ {
		reportLoggedBlock(t, b, height, 0)
	}
	receive(t, published, 3)
	require.NoError(t, b.Close())

	for _, record := range expectedChain(1, 1, 3) {
		requireLoggedRow(t, dir, record)
	}
}

func TestHashLogThatFailsToOpenDoesNotStopBuilder(t *testing.T) {
	config := hashLoggingConfig(t.TempDir())
	config.HashLogConfig.Version = ""
	b, err := NewStandardAppHashBuilder(config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	require.NoError(t, b.SetupComplete(context.Background(), 0))
	_, published := registerListener(t, b)
	reportLoggedBlock(t, b, 1, 0)
	requireSameAppHashes(t, expectedChain(1, 1, 1), receive(t, published, 1))
}

func TestChangedAppHashIsFlushedToHashLogBeforeCrash(t *testing.T) {
	dir := requireScenarioPanics(t, "stored-ahead-hash-log", func(t *testing.T, dir string) {
		buildChain(t, filepath.Join(dir, "vault"), 5)
		b := openHashLoggingBuilder(t, dir)
		require.NoError(t, b.SetupComplete(context.Background(), 3))
		_, published := registerListener(t, b)
		reportLoggedBlock(t, b, 4, 1)
		// The changed block crashes the process before it is published.
		receive(t, published, 1)
	})

	changed := apphash.NewAppHashData(
		testChainID,
		4,
		inputValue(blockHashInput, 4, 0),
		inputValue(stateHashInput, 4, 0),
		inputValue(budInput, 4, 1),
		inputValue(receiptHashInput, 4, 0),
		expectedChain(1, 3, 3)[0].AppHash(),
	)
	requireLoggedRow(t, dir, changed)
}
