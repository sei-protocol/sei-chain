package builder

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

const testChainID uint64 = 713715

// How long a test waits for the builder to publish an app hash.
const publishTimeout = 10 * time.Second

// Every input, in a fixed order.
var allInputs = []hashInput{blockHashInput, stateHashInput, budInput, receiptHashInput}

// inputValue returns the value tests report for input at blockHeight. A different salt gives a different value.
func inputValue(input hashInput, blockHeight uint64, salt byte) [32]byte {
	var preimage [10]byte
	preimage[0] = byte(input)
	preimage[1] = salt
	binary.BigEndian.PutUint64(preimage[2:], blockHeight)
	return sha256.Sum256(preimage[:])
}

// expectedChain returns the app hash data of blocks first through last, chained from a zero previous app hash at
// gigaActivationHeight, built from the values inputValue() gives with salt 0.
func expectedChain(gigaActivationHeight uint64, first uint64, last uint64) []*apphash.AppHashData {
	var previous [32]byte
	var chain []*apphash.AppHashData
	for height := gigaActivationHeight; height <= last; height++ {
		record := apphash.NewAppHashData(
			testChainID,
			height,
			inputValue(blockHashInput, height, 0),
			inputValue(stateHashInput, height, 0),
			inputValue(budInput, height, 0),
			inputValue(receiptHashInput, height, 0),
			previous,
		)
		previous = record.AppHash()
		if height >= first {
			chain = append(chain, record)
		}
	}
	return chain
}

// openTestBuilder opens a builder over the vault in dir that is closed when the test ends.
func openTestBuilder(
	t *testing.T,
	dir string,
	chainID uint64,
	gigaActivationHeight uint64,
) *StandardAppHashBuilder {
	t.Helper()
	b, err := openBuilder(dir, chainID, gigaActivationHeight)
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// openBuilder opens a builder over the vault in dir.
func openBuilder(dir string, chainID uint64, gigaActivationHeight uint64) (*StandardAppHashBuilder, error) {
	config := DefaultAppHashBuilderConfig()
	config.HashVaultConfig.Path = dir
	return NewStandardAppHashBuilder(config, chainID, gigaActivationHeight)
}

// reportInput reports input's value for blockHeight.
func reportInput(t *testing.T, b AppHashBuilder, input hashInput, blockHeight uint64, salt byte) {
	t.Helper()
	ctx := context.Background()
	value := inputValue(input, blockHeight, salt)
	var err error
	switch input {
	case blockHashInput:
		err = b.ReportBlockHash(ctx, blockHeight, value)
	case stateHashInput:
		err = b.ReportStateHash(ctx, blockHeight, value)
	case budInput:
		err = b.ReportBUD(ctx, blockHeight, value)
	case receiptHashInput:
		err = b.ReportReceiptHash(ctx, blockHeight, value)
	}
	require.NoError(t, err)
}

// reportBlocks reports every input for blocks first through last, interleaving the inputs differently from block
// to block.
func reportBlocks(t *testing.T, b AppHashBuilder, first uint64, last uint64) {
	t.Helper()
	for height := first; height <= last; height++ {
		for i := range allInputs {
			reportInput(t, b, allInputs[(int(height)+i)%len(allInputs)], height, 0)
		}
	}
}

// reportInputBlocks reports input for blocks first through last.
func reportInputBlocks(t *testing.T, b AppHashBuilder, input hashInput, first uint64, last uint64) {
	t.Helper()
	for height := first; height <= last; height++ {
		reportInput(t, b, input, height, 0)
	}
}

// registerListener registers a listener and returns the app hash RegisterListener() returned along with a channel
// receiving every app hash passed to the listener.
func registerListener(t *testing.T, b AppHashBuilder) (*apphash.AppHashData, <-chan *apphash.AppHashData) {
	t.Helper()
	published := make(chan *apphash.AppHashData, 1024)
	anchor, err := b.RegisterListener(context.Background(), func(appHash *apphash.AppHashData) {
		published <- appHash
	})
	require.NoError(t, err)
	return anchor, published
}

// receive returns the next count app hashes from published.
func receive(t *testing.T, published <-chan *apphash.AppHashData, count int) []*apphash.AppHashData {
	t.Helper()
	received := make([]*apphash.AppHashData, 0, count)
	for len(received) < count {
		select {
		case appHash := <-published:
			received = append(received, appHash)
		case <-time.After(publishTimeout):
			require.FailNow(t, "timed out waiting for an app hash",
				"received %d of %d", len(received), count)
		}
	}
	return received
}

// requireNothingPublished fails if published receives an app hash within a short wait.
func requireNothingPublished(t *testing.T, published <-chan *apphash.AppHashData) {
	t.Helper()
	select {
	case appHash := <-published:
		require.FailNow(t, "unexpected app hash", "block %d", appHash.BlockHeight())
	case <-time.After(100 * time.Millisecond):
	}
}

// iterateAll returns every app hash the builder iterates from start.
func iterateAll(t *testing.T, b AppHashBuilder, start uint64) []*apphash.AppHashData {
	t.Helper()
	it, err := b.Iterator(context.Background(), start)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()
	var appHashes []*apphash.AppHashData
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			return appHashes
		}
		appHashes = append(appHashes, it.Entry())
	}
}

// requireSameAppHashes fails unless expected and actual hold the same app hash data in the same order.
func requireSameAppHashes(t *testing.T, expected []*apphash.AppHashData, actual []*apphash.AppHashData) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].Serialize(), actual[i].Serialize(), "app hash %d", i)
	}
}

// buildChain builds blocks 1 through last on a fresh vault in dir with giga activation height 1, then closes the
// builder.
func buildChain(t *testing.T, dir string, last uint64) {
	t.Helper()
	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	_, published := registerListener(t, b)
	reportBlocks(t, b, 1, last)
	receive(t, published, int(last))
	require.NoError(t, b.Close())
}

func TestBuildsChainFromEmptyVault(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	anchor, published := registerListener(t, b)
	require.Nil(t, anchor)

	reportBlocks(t, b, 1, 10)
	expected := expectedChain(1, 1, 10)
	requireSameAppHashes(t, expected, receive(t, published, 10))
	requireSameAppHashes(t, expected, iterateAll(t, b, 1))
	requireSameAppHashes(t, expected[4:], iterateAll(t, b, 5))
}

func TestWaitsForEveryInput(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	_, published := registerListener(t, b)

	// Each input reports its blocks in a burst, so the inputs run far apart.
	reportInputBlocks(t, b, stateHashInput, 1, 5)
	reportInputBlocks(t, b, blockHashInput, 1, 5)
	reportInputBlocks(t, b, budInput, 1, 5)
	requireNothingPublished(t, published)

	reportInputBlocks(t, b, receiptHashInput, 1, 3)
	requireSameAppHashes(t, expectedChain(1, 1, 3), receive(t, published, 3))
	requireNothingPublished(t, published)

	reportInputBlocks(t, b, receiptHashInput, 4, 5)
	requireSameAppHashes(t, expectedChain(1, 4, 5), receive(t, published, 2))
}

func TestPublishedAppHashesSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 5)

	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 5))
	anchor, published := registerListener(t, b)
	requireSameAppHashes(t, expectedChain(1, 5, 5), []*apphash.AppHashData{anchor})
	requireSameAppHashes(t, expectedChain(1, 1, 5), iterateAll(t, b, 1))

	reportBlocks(t, b, 6, 8)
	requireSameAppHashes(t, expectedChain(1, 6, 8), receive(t, published, 3))
	requireSameAppHashes(t, expectedChain(1, 1, 8), iterateAll(t, b, 1))
}

func TestStartupReplayMatchingStoredAppHashes(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	// Replay that reaches the newest stored block on every input, and replay of a single input.
	reportBlocks(t, b, 4, 10)
	require.NoError(t, b.SetupComplete(context.Background(), 10))
	anchor, published := registerListener(t, b)
	requireSameAppHashes(t, expectedChain(1, 10, 10), []*apphash.AppHashData{anchor})

	reportBlocks(t, b, 11, 12)
	requireSameAppHashes(t, expectedChain(1, 11, 12), receive(t, published, 2))
}

func TestStartupReplayOfOneInput(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	reportInputBlocks(t, b, stateHashInput, 6, 10)
	require.NoError(t, b.SetupComplete(context.Background(), 10))
	_, published := registerListener(t, b)
	reportBlocks(t, b, 11, 11)
	requireSameAppHashes(t, expectedChain(1, 11, 11), receive(t, published, 1))
}

func TestSetupStoresBlocksAboveVault(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 5)

	b := openTestBuilder(t, dir, testChainID, 1)
	// Inputs replay blocks the vault already holds, then blocks it does not.
	reportBlocks(t, b, 3, 8)
	require.NoError(t, b.SetupComplete(context.Background(), 8))
	anchor, published := registerListener(t, b)
	requireSameAppHashes(t, expectedChain(1, 8, 8), []*apphash.AppHashData{anchor})
	requireSameAppHashes(t, expectedChain(1, 1, 8), iterateAll(t, b, 1))

	reportBlocks(t, b, 9, 9)
	requireSameAppHashes(t, expectedChain(1, 9, 9), receive(t, published, 1))
}

func TestSetupDiscardsReportsAboveStorageHeight(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 5)

	b := openTestBuilder(t, dir, testChainID, 1)
	// Inputs report beyond the height the storage layer settles on, then report those blocks again.
	reportBlocks(t, b, 6, 9)
	require.NoError(t, b.SetupComplete(context.Background(), 7))
	anchor, published := registerListener(t, b)
	requireSameAppHashes(t, expectedChain(1, 7, 7), []*apphash.AppHashData{anchor})

	reportBlocks(t, b, 8, 9)
	requireSameAppHashes(t, expectedChain(1, 8, 9), receive(t, published, 2))
}

func TestSetupFailsWhenABlockCannotBeComputed(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 5)

	b := openTestBuilder(t, dir, testChainID, 1)
	reportInputBlocks(t, b, blockHashInput, 6, 8)
	reportInputBlocks(t, b, stateHashInput, 6, 8)
	reportInputBlocks(t, b, budInput, 6, 8)
	require.Error(t, b.SetupComplete(context.Background(), 8))

	_, err := b.RegisterListener(context.Background(), func(*apphash.AppHashData) {})
	require.Error(t, err)
	require.Error(t, b.ReportBlockHash(context.Background(), 9, [32]byte{}))
}

func TestStoredBlocksAboveStorageHeightAreRebuilt(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 6))
	anchor, published := registerListener(t, b)
	requireSameAppHashes(t, expectedChain(1, 6, 6), []*apphash.AppHashData{anchor})
	requireSameAppHashes(t, expectedChain(1, 1, 6), iterateAll(t, b, 1))

	reportBlocks(t, b, 7, 12)
	requireSameAppHashes(t, expectedChain(1, 7, 12), receive(t, published, 6))
	requireSameAppHashes(t, expectedChain(1, 1, 12), iterateAll(t, b, 1))
}

func TestInputsBelowGigaActivationHeightAreDiscarded(t *testing.T) {
	const gigaActivationHeight = 11
	b := openTestBuilder(t, t.TempDir(), testChainID, gigaActivationHeight)
	reportBlocks(t, b, 3, 8)
	require.NoError(t, b.SetupComplete(context.Background(), 5))
	anchor, published := registerListener(t, b)
	require.Nil(t, anchor)

	reportBlocks(t, b, 6, 13)
	expected := expectedChain(gigaActivationHeight, gigaActivationHeight, 13)
	require.Equal(t, [32]byte{}, expected[0].PreviousAppHash())
	requireSameAppHashes(t, expected, receive(t, published, 3))
	requireSameAppHashes(t, expected, iterateAll(t, b, gigaActivationHeight))
}

func TestRestartWithGigaActivationHeight(t *testing.T) {
	const gigaActivationHeight = 11
	dir := t.TempDir()
	b := openTestBuilder(t, dir, testChainID, gigaActivationHeight)
	require.NoError(t, b.SetupComplete(context.Background(), gigaActivationHeight-1))
	_, published := registerListener(t, b)
	reportBlocks(t, b, gigaActivationHeight, gigaActivationHeight+3)
	receive(t, published, 4)
	require.NoError(t, b.Close())

	reopened := openTestBuilder(t, dir, testChainID, gigaActivationHeight)
	require.NoError(t, reopened.SetupComplete(context.Background(), gigaActivationHeight-1))
	anchor, published := registerListener(t, reopened)
	require.Nil(t, anchor)
	reportBlocks(t, reopened, gigaActivationHeight, gigaActivationHeight+4)
	requireSameAppHashes(t, expectedChain(gigaActivationHeight, gigaActivationHeight, gigaActivationHeight+4),
		receive(t, published, 5))
}

func TestOutOfOrderReportStopsBuilder(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	reportInput(t, b, blockHashInput, 4, 0)
	reportInput(t, b, blockHashInput, 6, 0)

	_, err := b.RegisterListener(context.Background(), func(*apphash.AppHashData) {})
	require.Error(t, err)
	require.Error(t, b.ReportStateHash(context.Background(), 1, [32]byte{}))
	require.Error(t, b.SetupComplete(context.Background(), 0))
}

func TestFailedBuilderReleasesHashVault(t *testing.T) {
	dir := t.TempDir()
	b := openTestBuilder(t, dir, testChainID, 1)
	reportInput(t, b, blockHashInput, 4, 0)
	reportInput(t, b, blockHashInput, 6, 0)
	b.wg.Wait()

	openTestBuilder(t, dir, testChainID, 1)
}

func TestFirstReportAfterSetupMustFollowStorageHeight(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	reportInputBlocks(t, b, budInput, 1, 3)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	reportInput(t, b, budInput, 4, 0)

	_, err := b.RegisterListener(context.Background(), func(*apphash.AppHashData) {})
	require.Error(t, err)
}

func TestCallsBeforeSetupFail(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	_, err := b.RegisterListener(context.Background(), func(*apphash.AppHashData) {})
	require.Error(t, err)
	_, err = b.Iterator(context.Background(), 1)
	require.Error(t, err)

	// Neither call stops the builder.
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	_, err = b.RegisterListener(context.Background(), func(*apphash.AppHashData) {})
	require.NoError(t, err)
}

func TestRegisterListenerRejectsNil(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	_, err := b.RegisterListener(context.Background(), nil)
	require.Error(t, err)

	// The builder keeps running.
	_, err = b.RegisterListener(context.Background(), func(*apphash.AppHashData) {})
	require.NoError(t, err)
}

func TestSetupCompleteTwiceFails(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	require.Error(t, b.SetupComplete(context.Background(), 0))
}

func TestIteratorBeyondNewestIsEmpty(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	require.Empty(t, iterateAll(t, b, 1))

	_, published := registerListener(t, b)
	reportBlocks(t, b, 1, 3)
	receive(t, published, 3)
	require.Empty(t, iterateAll(t, b, 4))
	requireSameAppHashes(t, expectedChain(1, 3, 3), iterateAll(t, b, 3))
}

func TestPruneKeepsNewestPublishedAppHash(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.Prune(context.Background(), 100))
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	_, published := registerListener(t, b)
	reportBlocks(t, b, 1, 5)
	receive(t, published, 5)
	require.NoError(t, b.Prune(context.Background(), 100))

	requireSameAppHashes(t, expectedChain(1, 5, 5), iterateAll(t, b, 5))
}

func TestChainIDMismatchFailsStartup(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 3)
	_, err := openBuilder(dir, testChainID+1, 1)
	require.Error(t, err)
}

func TestZeroGigaActivationHeightFailsStartup(t *testing.T) {
	_, err := openBuilder(t.TempDir(), testChainID, 0)
	require.Error(t, err)
}

func TestCloseStopsBuilder(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.Close())
	require.NoError(t, b.Close())
	require.Error(t, b.ReportBlockHash(context.Background(), 1, [32]byte{}))
	require.Error(t, b.SetupComplete(context.Background(), 0))
}

// The environment variable naming the scenario a child test process runs.
const mismatchScenarioEnv = "APPHASH_BUILDER_MISMATCH_SCENARIO"

// The environment variable naming the vault directory a child test process uses.
const mismatchDirEnv = "APPHASH_BUILDER_MISMATCH_DIR"

// requireScenarioPanics runs scenario in a child test process and requires it to crash on a changed hash. A panic
// on the builder goroutine cannot be recovered by the test.
func requireScenarioPanics(t *testing.T, name string, scenario func(t *testing.T, dir string)) {
	if os.Getenv(mismatchScenarioEnv) == name {
		scenario(t, os.Getenv(mismatchDirEnv))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), mismatchScenarioEnv+"="+name, mismatchDirEnv+"="+t.TempDir())
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "the scenario did not crash:\n%s", output)
	require.Contains(t, string(output), "changed: stored")
}

func TestChangedInputDuringStartupReplayPanics(t *testing.T) {
	requireScenarioPanics(t, "replay", func(t *testing.T, dir string) {
		buildChain(t, dir, 5)
		b := openTestBuilder(t, dir, testChainID, 1)
		reportInputBlocks(t, b, stateHashInput, 2, 2)
		reportInput(t, b, stateHashInput, 3, 1)
		// Waits behind the changed report, which crashes the process first.
		_ = b.SetupComplete(context.Background(), 5)
	})
}

func TestChangedBlockAboveStorageHeightPanics(t *testing.T) {
	requireScenarioPanics(t, "stored-ahead", func(t *testing.T, dir string) {
		buildChain(t, dir, 5)
		b := openTestBuilder(t, dir, testChainID, 1)
		require.NoError(t, b.SetupComplete(context.Background(), 3))
		_, published := registerListener(t, b)
		reportInputBlocks(t, b, blockHashInput, 4, 4)
		reportInputBlocks(t, b, stateHashInput, 4, 4)
		reportInputBlocks(t, b, receiptHashInput, 4, 4)
		reportInput(t, b, budInput, 4, 1)
		// The changed block crashes the process before it is published.
		receive(t, published, 1)
	})
}
