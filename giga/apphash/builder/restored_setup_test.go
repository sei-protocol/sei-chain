package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// The app hash a restored node is given for its snapshot height.
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

// storeRestoreAnchor completes a restored setup at blockHeight on a fresh vault in dir, then closes the builder.
func storeRestoreAnchor(t *testing.T, dir string, blockHeight uint64) {
	t.Helper()
	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.CompleteRestoredSetup(context.Background(), blockHeight, testRestoredAppHash))
	require.NoError(t, closeBuilder(b))
}

func TestRestoredSetupPublishesTheAnchor(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.CompleteRestoredSetup(context.Background(), 10, testRestoredAppHash))

	requirePublishedAnchor(t, b, 10, testRestoredAppHash)
	requireBounds(t, b, 10, 10)
	requireNotPublished(t, b, 11)
	head, err := b.vault.GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(10), head)
}

func TestRestoredSetupChainsTheNextBlockOntoTheAnchor(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.CompleteRestoredSetup(context.Background(), 10, testRestoredAppHash))

	reportBlocks(t, b, 11, 13)
	expected := expectedChainFrom(testRestoredAppHash, 11, 13)
	require.Equal(t, testRestoredAppHash, expected[0].PreviousAppHash())
	requireSameAppHashes(t, expected, awaitAppHashes(t, b, 11, 13))
}

func TestRestoredSetupAtGigaActivationHeight(t *testing.T) {
	const gigaActivationHeight = 5
	b := openTestBuilder(t, t.TempDir(), testChainID, gigaActivationHeight)
	require.NoError(t, b.CompleteRestoredSetup(context.Background(), gigaActivationHeight, testRestoredAppHash))

	requirePublishedAnchor(t, b, gigaActivationHeight, testRestoredAppHash)
	reportBlocks(t, b, gigaActivationHeight+1, gigaActivationHeight+1)
	requireSameAppHashes(t, expectedChainFrom(testRestoredAppHash, gigaActivationHeight+1, gigaActivationHeight+1),
		awaitAppHashes(t, b, gigaActivationHeight+1, gigaActivationHeight+1))
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

func TestRestoredSetupAcceptsTheSameStoredAnchor(t *testing.T) {
	dir := t.TempDir()
	storeRestoreAnchor(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.CompleteRestoredSetup(context.Background(), 10, testRestoredAppHash))
	requirePublishedAnchor(t, b, 10, testRestoredAppHash)
	reportBlocks(t, b, 11, 11)
	requireSameAppHashes(t, expectedChainFrom(testRestoredAppHash, 11, 11), awaitAppHashes(t, b, 11, 11))
}

func TestRestoredSetupRefusesAnAnchorAtAnotherHeight(t *testing.T) {
	for _, height := range []uint64{9, 11} {
		dir := t.TempDir()
		storeRestoreAnchor(t, dir, 10)

		b := openTestBuilder(t, dir, testChainID, 1)
		require.Error(t, b.CompleteRestoredSetup(context.Background(), height, testRestoredAppHash),
			"restored setup at block %d", height)
		_, err := b.GetAppHash(context.Background(), 10)
		require.Error(t, err, "the builder stops on the refusal")
	}
}

func TestRestoredSetupRefusesAnAnchorWithAnotherAppHash(t *testing.T) {
	dir := t.TempDir()
	storeRestoreAnchor(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	require.Error(t, b.CompleteRestoredSetup(context.Background(), 10, [32]byte{0xff}))
	_, err := b.GetAppHash(context.Background(), 10)
	require.Error(t, err, "the builder stops on the refusal")
}

func TestRestoredSetupRefusesAVaultWithComputedAppHashes(t *testing.T) {
	stored := expectedChain(1, 5, 5)[0]
	for _, height := range []uint64{5, 6} {
		dir := t.TempDir()
		buildChain(t, dir, 5)

		b := openTestBuilder(t, dir, testChainID, 1)
		require.Error(t, b.CompleteRestoredSetup(context.Background(), height, stored.AppHash()),
			"restored setup at block %d", height)
	}
}

func TestRestoredSetupRefusesAnAnchorWithAppHashesAboveIt(t *testing.T) {
	dir := t.TempDir()
	storeRestoreAnchor(t, dir, 10)
	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 10))
	reportBlocks(t, b, 11, 12)
	awaitAppHashes(t, b, 11, 12)
	require.NoError(t, closeBuilder(b))

	reopened := openTestBuilder(t, dir, testChainID, 1)
	require.Error(t, reopened.CompleteRestoredSetup(context.Background(), 10, testRestoredAppHash))
}

func TestRestoredSetupAfterSetupFails(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	require.Error(t, b.CompleteRestoredSetup(context.Background(), 10, testRestoredAppHash))
}

func TestRestoredSetupBelowGigaActivationFails(t *testing.T) {
	const gigaActivationHeight = 11
	dir := t.TempDir()
	b := openTestBuilder(t, dir, testChainID, gigaActivationHeight)
	require.Error(t, b.CompleteRestoredSetup(context.Background(), gigaActivationHeight-1, testRestoredAppHash))
	_ = b.Close() // may report the refusal that stopped the builder
	require.NoError(t, b.vault.Close())

	reopened := openTestBuilder(t, dir, testChainID, gigaActivationHeight)
	require.NoError(t, reopened.SetupComplete(context.Background(), gigaActivationHeight-1),
		"the refused anchor was not stored")
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
