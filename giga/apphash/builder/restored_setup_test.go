package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
)

// The app hash a restored node holds for its snapshot height.
var testRestoredAppHash = [32]byte{0x5e, 0x1d}

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

// requirePublishedAnchor fails unless b publishes the restore anchor of blockHeight with appHash.
func requirePublishedAnchor(t *testing.T, b AppHashBuilder, blockHeight uint64, appHash [32]byte) {
	t.Helper()
	published := awaitAppHashes(t, b, blockHeight, blockHeight)[0]
	require.True(t, published.IsRestoreAnchor())
	require.Equal(t, testChainID, published.ChainID())
	require.Equal(t, blockHeight, published.BlockHeight())
	require.Equal(t, appHash, published.AppHash())
}

// storeRestoreAnchor stores a restore anchor of blockHeight as the first record of a fresh vault in dir, as a
// restore that installs the app hash alongside the state leaves it.
func storeRestoreAnchor(t *testing.T, dir string, blockHeight uint64) {
	t.Helper()
	config := testConfig(dir, 1)
	hashVault, err := vault.NewStandardHashVault(config.HashVaultConfig, testChainID, 1)
	require.NoError(t, err)
	anchor := apphash.NewRestoreAnchorAppHashData(testChainID, blockHeight, testRestoredAppHash)
	require.NoError(t, hashVault.Append([]*apphash.AppHashData{anchor}))
	require.NoError(t, hashVault.Close())
}

// After a restart, the stored anchor is the head, so a plain SetupComplete() at its height publishes it.
func TestSetupCompleteAtTheStoredAnchor(t *testing.T) {
	dir := t.TempDir()
	storeRestoreAnchor(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 10))
	requirePublishedAnchor(t, b, 10, testRestoredAppHash)
	reportBlocks(t, b, 11, 11)
	requireSameAppHashes(t, expectedChainFrom(testRestoredAppHash, 11, 11), awaitAppHashes(t, b, 11, 11))
}

// A report for the anchor's block before setup has no stored inputs to be checked against, so it must not be taken
// for a changed hash.
func TestReportOfTheAnchorBlockBeforeSetupIsNotCompared(t *testing.T) {
	dir := t.TempDir()
	storeRestoreAnchor(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	reportBlocks(t, b, 10, 10)
	require.NoError(t, b.SetupComplete(context.Background(), 10))
	requirePublishedAnchor(t, b, 10, testRestoredAppHash)
	reportBlocks(t, b, 11, 11)
	requireSameAppHashes(t, expectedChainFrom(testRestoredAppHash, 11, 11), awaitAppHashes(t, b, 11, 11))
}
