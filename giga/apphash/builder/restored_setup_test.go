package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
)

// The app hash of the block before a restored node's snapshot block, which the restored node never computes.
var testRestoredPreviousAppHash = [32]byte{0x5e, 0x1d}

// expectedChainFrom returns the app hash data of blocks first through last, chained from previous at block first-1,
// built from the values inputValue() gives with salt 0.
func expectedChainFrom(previous [32]byte, first uint64, last uint64) []*apphash.AppHashData {
	chain := make([]*apphash.AppHashData, 0, last-first+1)
	for height := first; height <= last; height++ {
		record := apphash.NewAppHashData(
			testChainID,
			height,
			inputValue(blockHashInput, height, 0),
			inputValue(stateHashInput, height, 0),
			inputValue(budHashInput, height, 0),
			inputValue(receiptHashInput, height, 0),
			previous,
		)
		previous = record.AppHash()
		chain = append(chain, record)
	}
	return chain
}

// storeRestoredRecord stores the record of blockHeight, chained from testRestoredPreviousAppHash, as the only record
// of a fresh vault in dir, as a restore from a state sync archive leaves it. It returns the stored record.
func storeRestoredRecord(t *testing.T, dir string, blockHeight uint64) *apphash.AppHashData {
	t.Helper()
	config := testConfig(dir, 1)
	hashVault, err := vault.NewStandardHashVault(config.HashVaultConfig, testChainID, 1)
	require.NoError(t, err)
	record := expectedChainFrom(testRestoredPreviousAppHash, blockHeight, blockHeight)[0]
	require.NoError(t, hashVault.Append([]*apphash.AppHashData{record}))
	require.NoError(t, hashVault.Close())
	return record
}

// The restored record is the head, so a plain SetupComplete() at its height publishes it, and the next block chains
// onto it.
func TestSetupCompleteAtTheRestoredRecord(t *testing.T) {
	dir := t.TempDir()
	record := storeRestoredRecord(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 10))
	requireSameAppHashes(t, []*apphash.AppHashData{record}, awaitAppHashes(t, b, 10, 10))
	reportBlocks(t, b, 11, 11)
	requireSameAppHashes(t, expectedChainFrom(record.AppHash(), 11, 11), awaitAppHashes(t, b, 11, 11))
}

// A report for the restored block before setup that matches the restored record passes the check against it.
func TestMatchingReportOfTheRestoredBlockBeforeSetup(t *testing.T) {
	dir := t.TempDir()
	record := storeRestoredRecord(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	reportBlocks(t, b, 10, 10)
	require.NoError(t, b.SetupComplete(context.Background(), 10))
	requireSameAppHashes(t, []*apphash.AppHashData{record}, awaitAppHashes(t, b, 10, 10))
	reportBlocks(t, b, 11, 11)
	requireSameAppHashes(t, expectedChainFrom(record.AppHash(), 11, 11), awaitAppHashes(t, b, 11, 11))
}

// A state hash for the restored block before setup that differs from the restored record is a changed hash, as when
// the restored state does not match the record.
func TestChangedStateHashOfTheRestoredBlockBeforeSetupPanics(t *testing.T) {
	requireScenarioPanics(t, "restored", func(t *testing.T, dir string) {
		storeRestoredRecord(t, dir, 10)
		b := openTestBuilder(t, dir, testChainID, 1)
		reportInput(t, b, stateHashInput, 10, 1)
		// Waits behind the changed report, which crashes the process first.
		_ = b.SetupComplete(context.Background(), 10)
	})
}
