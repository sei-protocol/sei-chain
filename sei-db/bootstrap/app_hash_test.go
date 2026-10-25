package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	"github.com/sei-protocol/sei-chain/sei-db/config"
)

const testAppHashChainID uint64 = 713715

// How long a test waits for the app hash builder to publish an app hash.
const appHashPublishTimeout = 10 * time.Second

// awaitAppHashes waits until the manager's builder has stored the app hash of block height.
func awaitAppHashes(t *testing.T, manager *GigaStorageManager, height uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		head, err := manager.AppHashVault().GetLatestBlock()
		require.NoError(t, err)
		return head >= height
	}, appHashPublishTimeout, time.Millisecond, "the app hash of block %d was not stored", height)
}

// listenForAppHashes registers a listener on the manager's builder, returning the newest app hash published
// before it and a channel receiving every one published after.
func listenForAppHashes(t *testing.T, manager *GigaStorageManager) (*apphash.AppHashData, <-chan *apphash.AppHashData) {
	t.Helper()
	published := make(chan *apphash.AppHashData, 64)
	listener := func(appHash *apphash.AppHashData) { published <- appHash }
	newest, err := manager.AppHashBuilder().RegisterListener(context.Background(), listener)
	require.NoError(t, err)
	return newest, published
}

// receiveAppHashes waits for the next count app hashes published.
func receiveAppHashes(t *testing.T, published <-chan *apphash.AppHashData, count int) []*apphash.AppHashData {
	t.Helper()
	received := make([]*apphash.AppHashData, 0, count)
	for range count {
		select {
		case appHash := <-published:
			received = append(received, appHash)
		case <-time.After(appHashPublishTimeout):
			require.FailNow(t, "timed out waiting for an app hash",
				"received %d of %d", len(received), count)
		}
	}
	return received
}

// closeAppHashBuilder closes the app hash builder and then its hash vault, and drops both from the manager, leaving
// the manager as it was before the builder opened.
func closeAppHashBuilder(t *testing.T, manager *GigaStorageManager) {
	t.Helper()
	if manager.appHashBuilder != nil {
		require.NoError(t, manager.appHashBuilder.Close())
		manager.appHashBuilder = nil
	}
	if manager.appHashVault != nil {
		require.NoError(t, manager.appHashVault.Close())
		manager.appHashVault = nil
	}
}

// stopForRestart closes the state DB and then the app hash builder and its vault, in the order Close() does: the state
// DB's hash listeners report into the builder until the state DB stops.
func stopForRestart(t *testing.T, manager *GigaStorageManager) {
	t.Helper()
	closeStateDB(t, manager)
	closeAppHashBuilder(t, manager)
}

// restartAt re-runs what a restart converging on target does: it closes the builder and every store recovery
// touches, recovers them onto target, and opens the receipt store and the builder again.
func restartAt(t *testing.T, manager *GigaStorageManager, target int64) {
	t.Helper()
	stopForRestart(t, manager)
	reconverge(t, manager, target)
	require.NoError(t, manager.openAppHashBuilder(t.Context()))
}

// Storage reports every input of each committed block, with a zero block hash and receipt hash.
func TestAppHashesBuiltFromCommittedBlocks(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.AppHashBuilderConfig.ChainID = testAppHashChainID
	})
	newest, published := listenForAppHashes(t, manager)
	require.Nil(t, newest)

	commitBlocks(t, manager, 3)

	appHashes := receiveAppHashes(t, published, 3)
	for i, appHash := range appHashes {
		height := uint64(i) + 1 //nolint:gosec // small test heights
		require.Equal(t, height, appHash.BlockHeight())
		require.Equal(t, testAppHashChainID, appHash.ChainID())
		require.Equal(t, [32]byte{}, appHash.BlockHash())
		require.NotEqual(t, [32]byte{}, appHash.StateHash())
		require.NotEqual(t, apphash.BUDHash{}, appHash.BUDHash())
	}
	head, err := manager.AppHashVault().GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(3), head)
}

// A crash can leave the state ahead of the app hash vault. Recovery lowers the target to the vault head, so the
// blocks above it are re-executed and their app hashes built as they are.
func TestRestartWithStateAheadOfTheAppHashVault(t *testing.T) {
	manager, _ := openManager(t, disableSS)
	_, published := listenForAppHashes(t, manager)
	commitBlocks(t, manager, 3)
	stored := receiveAppHashes(t, published, 3)
	// The vault as it stands at block 3 is put back after blocks 4 and 5 commit, as a crash before their app
	// hashes are stored leaves it.
	stopForRestart(t, manager)
	vaultDir := manager.cfg.AppHashBuilderConfig.HashVaultConfig.Path
	savedVaultDir := filepath.Join(t.TempDir(), "vault")
	require.NoError(t, os.CopyFS(savedVaultDir, os.DirFS(vaultDir)))
	restartAt(t, manager, 3)
	require.NoError(t, manager.StateDB().CommitStateChanges(4, evmBlock(4, 4)))
	require.NoError(t, manager.StateDB().CommitStateChanges(5, evmBlock(5, 5)))
	awaitAppHashes(t, manager, 5)
	stopForRestart(t, manager)
	require.NoError(t, os.RemoveAll(vaultDir))
	require.NoError(t, os.CopyFS(vaultDir, os.DirFS(savedVaultDir)))

	appHashHead, err := builder.GetLatestBlock(*manager.cfg.AppHashBuilderConfig)
	require.NoError(t, err)
	target, err := recoveryTarget(5, 5, 0, appHashHead)
	require.NoError(t, err)
	require.Equal(t, uint64(3), target)
	restartAt(t, manager, int64(target)) //nolint:gosec // small test heights

	version, err := manager.SC().GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), version)
	newest, published := listenForAppHashes(t, manager)
	require.Equal(t, stored[2].AppHash(), newest.AppHash())

	require.NoError(t, manager.StateDB().CommitStateChanges(4, evmBlock(4, 4)))
	require.NoError(t, manager.StateDB().CommitStateChanges(5, evmBlock(5, 5)))
	rebuilt := receiveAppHashes(t, published, 2)
	require.Equal(t, uint64(4), rebuilt[0].BlockHeight())
	require.Equal(t, stored[2].AppHash(), rebuilt[0].PreviousAppHash())
}

// A crash can also leave the app hash vault ahead of the state, since the state WAL's tail is not flushed. The
// vault keeps its app hashes, and the re-executed blocks are checked against them.
func TestRestartWithTheAppHashVaultAheadOfState(t *testing.T) {
	manager, _ := openManager(t, disableSS)
	_, published := listenForAppHashes(t, manager)
	commitBlocks(t, manager, 5)
	stored := receiveAppHashes(t, published, 5)

	restartAt(t, manager, 3)

	newest, published := listenForAppHashes(t, manager)
	require.Equal(t, stored[2].AppHash(), newest.AppHash())
	head, err := manager.AppHashVault().GetLatestBlock()
	require.NoError(t, err)
	require.Equal(t, uint64(5), head, "the app hashes above the recovered height are kept")

	require.NoError(t, manager.StateDB().CommitStateChanges(4, evmBlock(4, 4)))
	require.NoError(t, manager.StateDB().CommitStateChanges(5, evmBlock(5, 5)))
	rebuilt := receiveAppHashes(t, published, 2)
	require.Equal(t, stored[3].AppHash(), rebuilt[0].AppHash())
	require.Equal(t, stored[4].AppHash(), rebuilt[1].AppHash())
}

// Registering the builder fails when the state hashes would not start right after the state height, rather than
// leaving the builder to stop on its first report.
func TestRegisteringBuilderFailsWhenStateHashesAreOutOfLine(t *testing.T) {
	manager, _ := openManager(t, disableSS)
	commitBlocks(t, manager, 3)
	stopForRestart(t, manager)
	reconverge(t, manager, 3)

	appHashConfig := manager.cfg.AppHashBuilderConfig
	hashVault, err := vault.NewStandardHashVault(
		appHashConfig.HashVaultConfig, appHashConfig.ChainID, appHashConfig.GigaActivationHeight)
	require.NoError(t, err)
	manager.appHashVault = hashVault
	appHashBuilder, err := builder.NewStandardAppHashBuilder(t.Context(), *appHashConfig, hashVault)
	require.NoError(t, err)
	manager.appHashBuilder = appHashBuilder

	require.ErrorContains(t, manager.reportStateHashesToAppHashBuilder(2), "state commit store")
}
