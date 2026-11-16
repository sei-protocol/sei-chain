package builder

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
)

const testChainID uint64 = 713715

// How long a test waits for the builder to publish an app hash.
const publishTimeout = 10 * time.Second

// Every input, in a fixed order.
var allInputs = []hashInput{blockHashInput, stateHashInput, budHashInput, receiptHashInput}

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
			inputValue(budHashInput, height, 0),
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
	b, err := openBuilder(t.Context(), dir, chainID, gigaActivationHeight)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeBuilder(b) })
	return b
}

// openBuilder opens a builder over the vault in dir.
func openBuilder(
	ctx context.Context,
	dir string,
	chainID uint64,
	gigaActivationHeight uint64,
) (*StandardAppHashBuilder, error) {
	config := testConfig(dir, gigaActivationHeight)
	config.ChainID = chainID
	return openBuilderWithConfig(ctx, config)
}

// openBuilderWithConfig opens the hash vault config.HashVaultConfig names, and a builder over it. closeBuilder()
// closes both.
func openBuilderWithConfig(ctx context.Context, config AppHashBuilderConfig) (*StandardAppHashBuilder, error) {
	hashVault, err := vault.NewStandardHashVault(
		config.HashVaultConfig, config.ChainID, config.GigaActivationHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to open hash vault: %w", err)
	}
	b, err := NewStandardAppHashBuilder(ctx, config, hashVault)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to open app hash builder: %w", err), hashVault.Close())
	}
	return b, nil
}

// closeBuilder closes b, then the hash vault it was opened over.
func closeBuilder(b *StandardAppHashBuilder) error {
	return errors.Join(b.Close(), b.vault.Close())
}

// testConfig returns the default configuration for the test chain, with its hash vault in dir, its giga activation
// at gigaActivationHeight, and no hash log.
func testConfig(dir string, gigaActivationHeight uint64) AppHashBuilderConfig {
	config := DefaultAppHashBuilderConfig()
	config.ChainID = testChainID
	config.GigaActivationHeight = gigaActivationHeight
	config.HashVaultConfig.Path = dir
	config.HashLogEnabled = false
	return config
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
		err = b.ReportStateHash(ctx, blockHeight, value, nil)
	case budHashInput:
		err = b.ReportBUDHash(ctx, blockHeight, value)
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

// awaitAppHashes waits for the app hashes of blocks first through last to be published, and returns them.
func awaitAppHashes(t *testing.T, b AppHashBuilder, first uint64, last uint64) []*apphash.AppHashData {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	appHashes := make([]*apphash.AppHashData, 0, last-first+1)
	for height := first; height <= last; height++ {
		appHash, err := b.GetAppHash(ctx, height)
		require.NoError(t, err)
		appHashes = append(appHashes, appHash)
	}
	return appHashes
}

// readAppHashes returns the published app hashes of blocks first through last, failing if any is not published.
func readAppHashes(t *testing.T, b AppHashBuilder, first uint64, last uint64) []*apphash.AppHashData {
	t.Helper()
	_, highest, ok, err := b.Bounds()
	require.NoError(t, err)
	require.True(t, ok && highest >= last, "the app hash of block %d is not published", last)
	return awaitAppHashes(t, b, first, last)
}

// requireNotPublished fails if the app hash of blockHeight is published within a short wait.
func requireNotPublished(t *testing.T, b AppHashBuilder, blockHeight uint64) {
	t.Helper()
	time.Sleep(100 * time.Millisecond)
	_, highest, ok, err := b.Bounds()
	require.NoError(t, err)
	require.False(t, ok && highest >= blockHeight, "unexpected app hash of block %d", blockHeight)
}

// requireSameAppHashes fails unless expected and actual hold the same app hash data in the same order.
func requireSameAppHashes(t *testing.T, expected []*apphash.AppHashData, actual []*apphash.AppHashData) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, mustSerialize(t, expected[i]), mustSerialize(t, actual[i]), "app hash %d", i)
	}
}

// mustSerialize returns the serialization of appHash.
func mustSerialize(t *testing.T, appHash *apphash.AppHashData) []byte {
	t.Helper()
	data, err := appHash.Serialize()
	require.NoError(t, err)
	return data
}

// buildChain builds blocks 1 through last on a fresh vault in dir with giga activation height 1, then closes the
// builder.
func buildChain(t *testing.T, dir string, last uint64) {
	t.Helper()
	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	reportBlocks(t, b, 1, last)
	awaitAppHashes(t, b, 1, last)
	require.NoError(t, closeBuilder(b))
}

func TestBuildsChainFromEmptyVault(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	requireNotPublished(t, b, 1)

	reportBlocks(t, b, 1, 10)
	expected := expectedChain(1, 1, 10)
	requireSameAppHashes(t, expected, awaitAppHashes(t, b, 1, 10))
	requireSameAppHashes(t, expected, readAppHashes(t, b, 1, 10))
	requireSameAppHashes(t, expected[4:], readAppHashes(t, b, 5, 10))
}

func TestWaitsForEveryInput(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))

	// Each input reports its blocks in a burst, so the inputs run far apart.
	reportInputBlocks(t, b, stateHashInput, 1, 5)
	reportInputBlocks(t, b, blockHashInput, 1, 5)
	reportInputBlocks(t, b, budHashInput, 1, 5)
	requireNotPublished(t, b, 1)

	reportInputBlocks(t, b, receiptHashInput, 1, 3)
	requireSameAppHashes(t, expectedChain(1, 1, 3), awaitAppHashes(t, b, 1, 3))
	requireNotPublished(t, b, 4)

	reportInputBlocks(t, b, receiptHashInput, 4, 5)
	requireSameAppHashes(t, expectedChain(1, 4, 5), awaitAppHashes(t, b, 4, 5))
}

func TestPublishedAppHashesSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 5)

	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 5))
	requireSameAppHashes(t, expectedChain(1, 1, 5), readAppHashes(t, b, 1, 5))
	requireNotPublished(t, b, 6)

	reportBlocks(t, b, 6, 8)
	requireSameAppHashes(t, expectedChain(1, 6, 8), awaitAppHashes(t, b, 6, 8))
	requireSameAppHashes(t, expectedChain(1, 1, 8), readAppHashes(t, b, 1, 8))
}

func TestStartupReplayMatchingStoredAppHashes(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	// Replay that reaches the newest stored block on every input, and replay of a single input.
	reportBlocks(t, b, 4, 10)
	require.NoError(t, b.SetupComplete(context.Background(), 10))
	requireSameAppHashes(t, expectedChain(1, 10, 10), readAppHashes(t, b, 10, 10))
	requireNotPublished(t, b, 11)

	reportBlocks(t, b, 11, 12)
	requireSameAppHashes(t, expectedChain(1, 11, 12), awaitAppHashes(t, b, 11, 12))
}

func TestStartupReplayOfOneInput(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	reportInputBlocks(t, b, stateHashInput, 6, 10)
	require.NoError(t, b.SetupComplete(context.Background(), 10))
	reportBlocks(t, b, 11, 11)
	requireSameAppHashes(t, expectedChain(1, 11, 11), awaitAppHashes(t, b, 11, 11))
}

func TestSetupStoresBlocksAboveVault(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 5)

	b := openTestBuilder(t, dir, testChainID, 1)
	// Inputs replay blocks the vault already holds, then blocks it does not.
	reportBlocks(t, b, 3, 8)
	require.NoError(t, b.SetupComplete(context.Background(), 8))
	requireSameAppHashes(t, expectedChain(1, 1, 8), readAppHashes(t, b, 1, 8))
	requireNotPublished(t, b, 9)

	reportBlocks(t, b, 9, 9)
	requireSameAppHashes(t, expectedChain(1, 9, 9), awaitAppHashes(t, b, 9, 9))
}

func TestSetupDiscardsReportsAboveStorageHeight(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 5)

	b := openTestBuilder(t, dir, testChainID, 1)
	// Inputs report beyond the height the storage layer settles on, then report those blocks again.
	reportBlocks(t, b, 6, 9)
	require.NoError(t, b.SetupComplete(context.Background(), 7))
	requireSameAppHashes(t, expectedChain(1, 7, 7), readAppHashes(t, b, 7, 7))
	requireNotPublished(t, b, 8)

	reportBlocks(t, b, 8, 9)
	requireSameAppHashes(t, expectedChain(1, 8, 9), awaitAppHashes(t, b, 8, 9))
}

func TestSetupFailsWhenABlockCannotBeComputed(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 5)

	b := openTestBuilder(t, dir, testChainID, 1)
	reportInputBlocks(t, b, blockHashInput, 6, 8)
	reportInputBlocks(t, b, stateHashInput, 6, 8)
	reportInputBlocks(t, b, budHashInput, 6, 8)
	require.Error(t, b.SetupComplete(context.Background(), 8))

	_, err := b.GetAppHash(context.Background(), 1)
	require.Error(t, err)
	require.Error(t, b.ReportBlockHash(context.Background(), 9, [32]byte{}))
}

func TestStoredBlocksAboveStorageHeightAreRebuilt(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 6))
	requireSameAppHashes(t, expectedChain(1, 1, 6), readAppHashes(t, b, 1, 6))
	// Stored, but not published until it is rebuilt.
	requireNotPublished(t, b, 7)

	reportBlocks(t, b, 7, 12)
	requireSameAppHashes(t, expectedChain(1, 7, 12), awaitAppHashes(t, b, 7, 12))
	requireSameAppHashes(t, expectedChain(1, 1, 12), readAppHashes(t, b, 1, 12))
}

func TestInputsBelowGigaActivationHeightAreDiscarded(t *testing.T) {
	const gigaActivationHeight = 11
	b := openTestBuilder(t, t.TempDir(), testChainID, gigaActivationHeight)
	reportBlocks(t, b, 3, 8)
	require.NoError(t, b.SetupComplete(context.Background(), 5))
	requireNotPublished(t, b, gigaActivationHeight)

	reportBlocks(t, b, 6, 13)
	expected := expectedChain(gigaActivationHeight, gigaActivationHeight, 13)
	require.Equal(t, [32]byte{}, expected[0].PreviousAppHash())
	requireSameAppHashes(t, expected, awaitAppHashes(t, b, gigaActivationHeight, 13))
	requireSameAppHashes(t, expected, readAppHashes(t, b, gigaActivationHeight, 13))
}

func TestRestartWithGigaActivationHeight(t *testing.T) {
	const gigaActivationHeight = 11
	dir := t.TempDir()
	b := openTestBuilder(t, dir, testChainID, gigaActivationHeight)
	require.NoError(t, b.SetupComplete(context.Background(), gigaActivationHeight-1))
	reportBlocks(t, b, gigaActivationHeight, gigaActivationHeight+3)
	awaitAppHashes(t, b, gigaActivationHeight, gigaActivationHeight+3)
	require.NoError(t, closeBuilder(b))

	reopened := openTestBuilder(t, dir, testChainID, gigaActivationHeight)
	require.NoError(t, reopened.SetupComplete(context.Background(), gigaActivationHeight-1))
	requireNotPublished(t, reopened, gigaActivationHeight)
	reportBlocks(t, reopened, gigaActivationHeight, gigaActivationHeight+4)
	requireSameAppHashes(t, expectedChain(gigaActivationHeight, gigaActivationHeight, gigaActivationHeight+4),
		awaitAppHashes(t, reopened, gigaActivationHeight, gigaActivationHeight+4))
}

func TestOutOfOrderReportStopsBuilder(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	reportInput(t, b, blockHashInput, 4, 0)
	reportInput(t, b, blockHashInput, 6, 0)

	_, err := b.GetAppHash(context.Background(), 1)
	require.Error(t, err)
	require.Error(t, b.ReportStateHash(context.Background(), 1, [32]byte{}, nil))
	require.Error(t, b.SetupComplete(context.Background(), 0))
}

func TestFirstReportAfterSetupMustFollowStorageHeight(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	reportInputBlocks(t, b, budHashInput, 1, 3)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	reportInput(t, b, budHashInput, 4, 0)

	_, err := b.GetAppHash(context.Background(), 1)
	require.Error(t, err)
}

func TestSetupCompleteTwiceFails(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	require.Error(t, b.SetupComplete(context.Background(), 0))
}

func TestAppHashBeyondNewestIsNil(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	requireNotPublished(t, b, 1)

	reportBlocks(t, b, 1, 3)
	awaitAppHashes(t, b, 1, 3)
	requireNotPublished(t, b, 4)
	requireSameAppHashes(t, expectedChain(1, 3, 3), readAppHashes(t, b, 3, 3))
}

func TestAppHashBelowGigaActivationFails(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 3)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	reportBlocks(t, b, 3, 5)
	awaitAppHashes(t, b, 3, 5)

	_, err := b.GetAppHash(context.Background(), 2)
	require.Error(t, err)
	_, err = b.GetAppHash(context.Background(), 1)
	require.Error(t, err)

	// Neither call stops the builder.
	requireSameAppHashes(t, expectedChain(3, 3, 5), readAppHashes(t, b, 3, 5))
}

func TestAppHashBelowGigaActivationFailsBeforeActivation(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 3)
	require.NoError(t, b.SetupComplete(context.Background(), 0))

	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	_, err := b.GetAppHash(ctx, 2)
	require.Error(t, err)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
}

func TestOfflineHeadOfEmptyVaultPrecedesGigaActivation(t *testing.T) {
	head, err := GetLatestBlock(testConfig(t.TempDir(), 5))
	require.NoError(t, err)
	require.Equal(t, uint64(4), head)
}

func TestOfflineHeadIsNewestStoredAppHash(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 3)
	head, err := GetLatestBlock(testConfig(dir, 1))
	require.NoError(t, err)
	require.Equal(t, uint64(3), head)
}

func TestOfflineHeadFailsWhileBuilderIsOpen(t *testing.T) {
	dir := t.TempDir()
	openTestBuilder(t, dir, testChainID, 1)
	_, err := GetLatestBlock(testConfig(dir, 1))
	require.Error(t, err)
}

func TestOfflineHeadRejectsZeroGigaActivationHeight(t *testing.T) {
	_, err := GetLatestBlock(testConfig(t.TempDir(), 0))
	require.Error(t, err)
}

func TestChainIDMismatchFailsStartup(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 3)
	_, err := openBuilder(t.Context(), dir, testChainID+1, 1)
	require.Error(t, err)
}

func TestZeroGigaActivationHeightFailsStartup(t *testing.T) {
	_, err := openBuilder(t.Context(), t.TempDir(), testChainID, 0)
	require.Error(t, err)
}

func TestCloseStoresQueuedAppHashes(t *testing.T) {
	dir := t.TempDir()
	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	reportBlocks(t, b, 1, 5)
	require.NoError(t, closeBuilder(b))

	head, err := GetLatestBlock(testConfig(dir, 1))
	require.NoError(t, err)
	require.Equal(t, uint64(5), head)
	reopened := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, reopened.SetupComplete(context.Background(), 5))
	requireSameAppHashes(t, expectedChain(1, 1, 5), readAppHashes(t, reopened, 1, 5))
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

// requireScenarioPanics runs scenario in a child test process and requires it to crash on a changed hash, returning
// the directory the scenario ran in. A panic on the builder goroutine cannot be recovered by the test.
func requireScenarioPanics(t *testing.T, name string, scenario func(t *testing.T, dir string)) string {
	if os.Getenv(mismatchScenarioEnv) == name {
		scenario(t, os.Getenv(mismatchDirEnv))
		require.FailNow(t, "the scenario did not crash")
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), mismatchScenarioEnv+"="+name, mismatchDirEnv+"="+dir)
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "the scenario did not crash:\n%s", output)
	require.Contains(t, string(output), "changed: stored")
	return dir
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
		reportInputBlocks(t, b, blockHashInput, 4, 4)
		reportInputBlocks(t, b, stateHashInput, 4, 4)
		reportInputBlocks(t, b, receiptHashInput, 4, 4)
		reportInput(t, b, budHashInput, 4, 1)
		// The changed block crashes the process before it is published.
		awaitAppHashes(t, b, 4, 4)
	})
}

// openCachingTestBuilder opens a builder over the vault in dir, closed when the test ends, that keeps cacheSize recent
// app hashes in memory. If oneRecordPerFile, the vault seals a file after every record, so a prune removes every
// record below its height.
func openCachingTestBuilder(t *testing.T, dir string, cacheSize int, oneRecordPerFile bool) *StandardAppHashBuilder {
	t.Helper()
	config := testConfig(dir, 1)
	config.RecentAppHashCacheSize = cacheSize
	if oneRecordPerFile {
		config.HashVaultConfig.TargetFileSize = 1
	}
	b, err := openBuilderWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeBuilder(b) })
	return b
}

// publishBlocks reports every input for blocks first through last and waits for their app hashes to be published,
// without reading any, so the wait leaves the read cursor as it is.
func publishBlocks(t *testing.T, b *StandardAppHashBuilder, first uint64, last uint64) {
	t.Helper()
	reportBlocks(t, b, first, last)
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	reached, _, err := b.publishedHeight.Await(ctx, last)
	require.NoError(t, err)
	require.True(t, reached, "the builder stopped before block %d was published", last)
}

// The values returned by one GetAppHash() call.
type awaitResult struct {
	appHash *apphash.AppHashData
	err     error
}

// awaitAsync runs GetAppHash() in a goroutine and returns a channel that receives its result.
func awaitAsync(ctx context.Context, b AppHashBuilder, blockHeight uint64) <-chan awaitResult {
	result := make(chan awaitResult, 1)
	go func() {
		appHash, err := b.GetAppHash(ctx, blockHeight)
		result <- awaitResult{appHash: appHash, err: err}
	}()
	return result
}

// requireStillWaiting fails if result receives a value within a short wait.
func requireStillWaiting(t *testing.T, result <-chan awaitResult) {
	t.Helper()
	select {
	case r := <-result:
		require.FailNow(t, "GetAppHash() returned early", "app hash %v, err %v", r.appHash, r.err)
	case <-time.After(100 * time.Millisecond):
	}
}

// receiveAwaitResult returns the value result receives, failing if it does not arrive in time.
func receiveAwaitResult(t *testing.T, result <-chan awaitResult) awaitResult {
	t.Helper()
	select {
	case r := <-result:
		return r
	case <-time.After(publishTimeout):
		require.FailNow(t, "timed out waiting for GetAppHash() to return")
		return awaitResult{}
	}
}

func TestRecentAppHashesAreServedFromMemory(t *testing.T) {
	b := openCachingTestBuilder(t, t.TempDir(), 4, false)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)

	requireSameAppHashes(t, expectedChain(1, 7, 10), readAppHashes(t, b, 7, 10))
	require.Nil(t, b.readCursor)
}

func TestSequentialReadsShareOneCursor(t *testing.T) {
	b := openCachingTestBuilder(t, t.TempDir(), 4, false)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)

	requireSameAppHashes(t, expectedChain(1, 1, 1), readAppHashes(t, b, 1, 1))
	cursor := b.readCursor
	require.NotNil(t, cursor)
	// The block the cursor is on, then each block after it that is too old for the recent app hashes.
	requireSameAppHashes(t, expectedChain(1, 1, 6), readAppHashes(t, b, 1, 6))
	require.Same(t, cursor, b.readCursor)

	// The block after the cursor's, served from the recent app hashes, leaves the cursor open.
	requireSameAppHashes(t, expectedChain(1, 7, 7), readAppHashes(t, b, 7, 7))
	require.Same(t, cursor, b.readCursor)
	// The cursor is still on block 6, so it cannot serve block 8.
	requireSameAppHashes(t, expectedChain(1, 8, 8), readAppHashes(t, b, 8, 8))
	require.Nil(t, b.readCursor)
}

func TestReadOfAnotherBlockClosesCursor(t *testing.T) {
	b := openCachingTestBuilder(t, t.TempDir(), 4, false)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)

	// A read served from the recent app hashes.
	readAppHashes(t, b, 2, 2)
	require.NotNil(t, b.readCursor)
	requireSameAppHashes(t, expectedChain(1, 9, 9), readAppHashes(t, b, 9, 9))
	require.Nil(t, b.readCursor)
}

func TestIdleCursorIsClosedOnVaultWrite(t *testing.T) {
	config := testConfig(t.TempDir(), 1)
	config.RecentAppHashCacheSize = 4
	config.ReadCursorIdleTimeout = 50 * time.Millisecond
	b, err := openBuilderWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeBuilder(b) })
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)

	readAppHashes(t, b, 2, 2)
	require.NotNil(t, b.readCursor)
	time.Sleep(100 * time.Millisecond)
	publishBlocks(t, b, 11, 11)
	require.Nil(t, b.readCursor)
}

func TestActiveCursorSurvivesVaultWrite(t *testing.T) {
	b := openCachingTestBuilder(t, t.TempDir(), 4, false)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)

	readAppHashes(t, b, 2, 2)
	cursor := b.readCursor
	require.NotNil(t, cursor)
	publishBlocks(t, b, 11, 11)
	require.Same(t, cursor, b.readCursor)
}

func TestOutOfSequenceReadReplacesCursor(t *testing.T) {
	b := openCachingTestBuilder(t, t.TempDir(), 4, false)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)

	requireSameAppHashes(t, expectedChain(1, 3, 3), readAppHashes(t, b, 3, 3))
	backward := b.readCursor

	// A read below the cursor.
	requireSameAppHashes(t, expectedChain(1, 2, 2), readAppHashes(t, b, 2, 2))
	require.NotSame(t, backward, b.readCursor)
	skipping := b.readCursor

	// A read that skips a block.
	requireSameAppHashes(t, expectedChain(1, 4, 4), readAppHashes(t, b, 4, 4))
	require.NotSame(t, skipping, b.readCursor)
}

func TestExhaustedCursorIsClosed(t *testing.T) {
	b := openCachingTestBuilder(t, t.TempDir(), 4, false)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)
	// Opens a cursor through block 10, the newest published.
	requireSameAppHashes(t, expectedChain(1, 5, 5), readAppHashes(t, b, 5, 5))

	// Block 10 leaves the recent app hashes, so the cursor reads through it.
	publishBlocks(t, b, 11, 16)
	requireSameAppHashes(t, expectedChain(1, 6, 10), readAppHashes(t, b, 6, 10))
	require.Nil(t, b.readCursor)

	requireSameAppHashes(t, expectedChain(1, 11, 16), readAppHashes(t, b, 11, 16))
}

func TestReadsOfStoredAppHashesAfterRestart(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 10)

	b := openCachingTestBuilder(t, dir, 4, false)
	require.NoError(t, b.SetupComplete(context.Background(), 6))
	requireSameAppHashes(t, expectedChain(1, 1, 6), readAppHashes(t, b, 1, 6))
	requireNotPublished(t, b, 7)
}

func TestReadOfPrunedAppHashFails(t *testing.T) {
	b := openCachingTestBuilder(t, t.TempDir(), 4, true)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)
	require.NoError(t, b.vault.PruneHistory(6))

	_, err := b.GetAppHash(context.Background(), 3)
	require.Error(t, err)

	// The failed read does not stop the builder.
	requireSameAppHashes(t, expectedChain(1, 6, 10), readAppHashes(t, b, 6, 10))
}

func TestGetAppHashAfterCloseFails(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 3)
	require.NoError(t, b.Close())

	_, err := b.GetAppHash(context.Background(), 3)
	require.Error(t, err)
}

func TestAwaitReplyAbandonedWhenCtxCancelled(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := awaitReply(ctx, b, make(chan callResult[struct{}], 1))
	require.ErrorIs(t, err, context.Canceled)
}

func TestGetAppHashWaitsForPublication(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	result := awaitAsync(context.Background(), b, 3)
	requireStillWaiting(t, result)

	reportBlocks(t, b, 1, 2)
	requireStillWaiting(t, result)
	reportBlocks(t, b, 3, 3)
	r := receiveAwaitResult(t, result)
	require.NoError(t, r.err)
	requireSameAppHashes(t, expectedChain(1, 3, 3), []*apphash.AppHashData{r.appHash})
}

func TestGetAppHashWaitsForSetup(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 5)

	b := openTestBuilder(t, dir, testChainID, 1)
	result := awaitAsync(context.Background(), b, 5)
	requireStillWaiting(t, result)

	require.NoError(t, b.SetupComplete(context.Background(), 5))
	r := receiveAwaitResult(t, result)
	require.NoError(t, r.err)
	requireSameAppHashes(t, expectedChain(1, 5, 5), []*apphash.AppHashData{r.appHash})
}

func TestGetAppHashIsAbandonedWithItsContext(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	ctx, cancel := context.WithCancel(context.Background())
	result := awaitAsync(ctx, b, 1)
	requireStillWaiting(t, result)

	cancel()
	require.ErrorIs(t, receiveAwaitResult(t, result).err, context.Canceled)

	// The abandoned call does not stop the builder.
	publishBlocks(t, b, 1, 1)
}

func TestGetAppHashFailsWhenBuilderCloses(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	result := awaitAsync(context.Background(), b, 1)
	requireStillWaiting(t, result)

	require.NoError(t, b.Close())
	require.Error(t, receiveAwaitResult(t, result).err)
}

// requireBounds fails unless b reports the available app hashes as blocks lowest through highest.
func requireBounds(t *testing.T, b AppHashBuilder, lowest uint64, highest uint64) {
	t.Helper()
	gotLowest, gotHighest, ok, err := b.Bounds()
	require.NoError(t, err)
	require.True(t, ok, "no app hash is available")
	require.Equal(t, lowest, gotLowest, "lowest")
	require.Equal(t, highest, gotHighest, "highest")
}

func TestBoundsBeforeSetupFail(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	_, _, _, err := b.Bounds()
	require.Error(t, err)

	// The call does not stop the builder.
	require.NoError(t, b.SetupComplete(context.Background(), 0))
}

func TestBoundsCoverAvailableAppHashes(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	_, _, ok, err := b.Bounds()
	require.NoError(t, err)
	require.False(t, ok)

	publishBlocks(t, b, 1, 10)
	requireBounds(t, b, 1, 10)
}

func TestBoundsAfterRestartEndAtStorageHeight(t *testing.T) {
	dir := t.TempDir()
	buildChain(t, dir, 10)

	b := openTestBuilder(t, dir, testChainID, 1)
	require.NoError(t, b.SetupComplete(context.Background(), 6))
	// Blocks 7 through 10 are stored, but not available until they are rebuilt.
	requireBounds(t, b, 1, 6)
}

func TestBoundsRiseAsAppHashesArePruned(t *testing.T) {
	b := openCachingTestBuilder(t, t.TempDir(), 4, true)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)
	require.NoError(t, b.vault.PruneHistory(6))

	requireBounds(t, b, 6, 10)
}

func TestBoundsIncludeCachedAppHashesAfterPruning(t *testing.T) {
	b := openCachingTestBuilder(t, t.TempDir(), 8, true)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 1, 10)
	require.NoError(t, b.vault.PruneHistory(6))

	requireBounds(t, b, 3, 10)
	requireSameAppHashes(t, expectedChain(1, 3, 10), readAppHashes(t, b, 3, 10))
}

func TestBoundsStartAtGigaActivation(t *testing.T) {
	b := openTestBuilder(t, t.TempDir(), testChainID, 3)
	require.NoError(t, b.SetupComplete(context.Background(), 0))
	publishBlocks(t, b, 3, 5)

	requireBounds(t, b, 3, 5)
}
