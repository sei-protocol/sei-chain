package bootstrap

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/giga/apphash/receipthash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	"github.com/sei-protocol/sei-chain/sei-db/config"
)

const testAppHashChainID uint64 = 713715

// How long a test waits for the app hash builder to publish an app hash.
const appHashPublishTimeout = 10 * time.Second

// The environment variable naming the test a child test process runs the crashing half of.
const crashingTestEnv = "BOOTSTRAP_CRASHING_TEST"

// awaitAppHashes waits until the manager's builder has published the app hash of block height.
func awaitAppHashes(t *testing.T, manager *GigaStorageManager, height uint64) {
	t.Helper()
	publishedAppHashes(t, manager, height, height)
}

// publishedAppHashes waits for the manager's builder to publish the app hashes of blocks first through last, and
// returns them.
func publishedAppHashes(t *testing.T, manager *GigaStorageManager, first uint64, last uint64) []*apphash.AppHashData {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), appHashPublishTimeout)
	defer cancel()
	appHashes := make([]*apphash.AppHashData, 0, last-first+1)
	for height := first; height <= last; height++ {
		appHash, err := manager.AppHashBuilder().GetAppHash(ctx, height)
		require.NoError(t, err)
		appHashes = append(appHashes, appHash)
	}
	return appHashes
}

// requireNotPublished fails if the manager's builder has published the app hash of block height.
func requireNotPublished(t *testing.T, manager *GigaStorageManager, height uint64, msgAndArgs ...any) {
	t.Helper()
	_, highest, ok, err := manager.AppHashBuilder().Bounds()
	require.NoError(t, err)
	require.False(t, ok && highest >= height, msgAndArgs...)
}

// requireCrashes runs the test calling it again in a child test process, where crash runs, and requires the child
// to crash with output containing want. A panic on the app hash builder's goroutine cannot be recovered by the test.
func requireCrashes(t *testing.T, want string, crash func(t *testing.T)) {
	if os.Getenv(crashingTestEnv) == t.Name() {
		crash(t)
		require.FailNow(t, "the child test did not crash")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), crashingTestEnv+"="+t.Name())
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "the child test did not crash:\n%s", output)
	require.Contains(t, string(output), want)
}

// Storage reports the state hash and BUD hash of each committed block, execution reports its block
// hash, and the receipt hasher reports the receipt hash.
func TestAppHashesBuiltFromCommittedBlocks(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.AppHashBuilderConfig.ChainID = testAppHashChainID
	})
	requireNotPublished(t, manager, 1)

	commitBlocks(t, manager, 3)

	appHashes := publishedAppHashes(t, manager, 1, 3)
	for i, appHash := range appHashes {
		height := uint64(i) + 1 //nolint:gosec // small test heights
		require.Equal(t, height, appHash.BlockHeight())
		require.Equal(t, testAppHashChainID, appHash.ChainID())
		require.Equal(t, testBlockHash(height), appHash.BlockHash())
		require.NotEqual(t, [32]byte{}, appHash.StateHash())
		require.NotEqual(t, apphash.BUDHash{}, appHash.BUDHash())
		emptyReceipts, err := receipthash.ComputeReceiptHash(nil)
		require.NoError(t, err)
		require.Equal(t, emptyReceipts, appHash.ReceiptHash())
	}
	_, highest, ok, err := manager.AppHashBuilder().Bounds()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(3), highest)
}

// A crash can leave the state ahead of the app hash vault. Recovery lowers the target to the vault head, so the
// blocks above it are re-executed and their app hashes built as they are.
func TestRestartWithStateAheadOfTheAppHashVault(t *testing.T) {
	cfg := newHome(t, disableSS)
	writeBlockLedger(t, cfg, 5)
	writeState(t, cfg, 5)
	writeReceipts(t, cfg, 5)
	writeAppHashes(t, cfg, 3)

	manager := startManager(t, cfg)

	version, err := manager.StateDB().SC().GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), version)
	stored := publishedAppHashes(t, manager, 3, 3)[0]

	reportTestBlockHash(t, manager, 4)
	commitBlock(t, manager, 4, evmBlock(4, 4))
	reportTestBlockHash(t, manager, 5)
	commitBlock(t, manager, 5, evmBlock(5, 5))
	rebuilt := publishedAppHashes(t, manager, 4, 5)
	require.Equal(t, uint64(4), rebuilt[0].BlockHeight())
	require.Equal(t, stored.AppHash(), rebuilt[0].PreviousAppHash())
}

// A crash can also leave the app hash vault ahead of the state, since the state WAL's tail is not flushed. The vault
// publishes its app hash of the recovered height and keeps those above it, unpublished until they are rebuilt.
func TestRestartWithTheAppHashVaultAheadOfState(t *testing.T) {
	cfg := newHome(t, disableSS)
	writeBlockLedger(t, cfg, 5)
	writeState(t, cfg, 5)
	truncateWAL(t, cfg, 3)
	writeReceipts(t, cfg, 5)
	writeAppHashes(t, cfg, 5)

	manager, err := NewGigaStorageManager(t.Context(), cfg)
	require.NoError(t, err)

	require.Equal(t, int64(3), manager.StateDB().SC().Version())
	published := publishedAppHashes(t, manager, 3, 3)[0]
	require.Equal(t, canonicalStateHashes(t, 3)[2], published.StateHash())
	requireNotPublished(t, manager, 4, "the stored app hashes above the recovered height are not published")
	require.NoError(t, manager.Close())
	head, err := builder.GetLatestBlock(*cfg.AppHashBuilderConfig)
	require.NoError(t, err)
	require.Equal(t, uint64(5), head, "the app hashes above the recovered height are kept")
}

// A recovered state whose hash differs from the one the vault stores for its height is state the node never
// committed to, and it stops the node before the app hash builder's setup completes.
func TestRecoveredStateThatDisagreesWithTheVaultStopsTheNode(t *testing.T) {
	requireCrashes(t, "state hash of block 3 changed", func(t *testing.T) {
		cfg := newHome(t, disableSS)
		writeBlockLedger(t, cfg, 3)
		writeState(t, cfg, 3)
		writeReceipts(t, cfg, 3)
		stateHashes := canonicalStateHashes(t, 3)
		stateHashes[2] = [32]byte{0xff}
		appendAppHashes(t, cfg, stateHashes)

		// The builder crashes the process while this waits for its setup to complete.
		_, _ = NewGigaStorageManager(t.Context(), cfg)
	})
}

// Registering the builder fails when the state hashes would not start right after the state height, rather than
// leaving the builder to stop on its first report.
func TestRegisteringBuilderFailsWhenStateHashesAreOutOfLine(t *testing.T) {
	cfg := newHome(t, disableSS)
	writeState(t, cfg, 3)
	stateDB := openStandaloneStateDB(t, cfg)
	defer func() { require.NoError(t, stateDB.Close()) }()
	appHashConfig := cfg.AppHashBuilderConfig
	hashVault, err := vault.NewStandardHashVault(
		appHashConfig.HashVaultConfig, appHashConfig.ChainID, appHashConfig.GigaActivationHeight)
	require.NoError(t, err)
	defer func() { require.NoError(t, hashVault.Close()) }()
	appHashBuilder, err := builder.NewStandardAppHashBuilder(t.Context(), *appHashConfig, hashVault)
	require.NoError(t, err)
	defer func() { require.NoError(t, appHashBuilder.Close()) }()

	err = reportStateHashesToAppHashBuilder(t.Context(), stateDB, appHashBuilder, 2)

	require.ErrorContains(t, err, "live state DB delivers state hashes")
}

// The receipt hash of each block is the hash of the receipts submitted to the manager's receipt hasher.
func TestAppHashCommitsToTheSubmittedReceipts(t *testing.T) {
	manager, _ := openManager(t, disableSS)
	receipts := []*ethtypes.Receipt{{
		Status:  ethtypes.ReceiptStatusSuccessful,
		TxHash:  common.Hash{1},
		GasUsed: 21000,
	}}

	reportTestBlockHash(t, manager, 1)
	require.NoError(t, manager.StateDB().CommitStateChanges(1, evmBlock(1, 1)))
	require.NoError(t, manager.ReceiptHasher().Submit(t.Context(), 1, receipts))

	want, err := receipthash.ComputeReceiptHash(receipts)
	require.NoError(t, err)
	require.Equal(t, want, publishedAppHashes(t, manager, 1, 1)[0].ReceiptHash())
}
