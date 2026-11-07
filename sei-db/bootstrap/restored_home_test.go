package bootstrap

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

// restoredHeight is the height of the FlatKV snapshot the restored homes below are installed from.
const restoredHeight = 4

// producedSnapshot is what a producer node leaves for a restore: a FlatKV snapshot at restoredHeight, and the app
// hashes it computed for that block and the one after it.
type producedSnapshot struct {

	// A directory holding the producer's FlatKV snapshot at restoredHeight.
	snapshotDir string

	// The producer's app hash of restoredHeight.
	appHash [32]byte

	// The producer's app hash of restoredHeight+1.
	nextAppHash [32]byte
}

// produceSnapshot commits blocks 1 through restoredHeight+1 on a fresh home, linking its FlatKV snapshot at
// restoredHeight out before the next block is committed.
func produceSnapshot(t *testing.T) producedSnapshot {
	t.Helper()
	producer, _ := openManager(t, disableSS)
	commitBlocks(t, producer, restoredHeight-1)
	reportTestBlockHash(t, producer, restoredHeight)
	snapshotSCAt(t, producer, restoredHeight)
	snapshotDir := filepath.Join(t.TempDir(), "snapshot")
	require.NoError(t, flatkv.HardLinkSnapshot(producer.cfg.FlatKVConfig.DataDir, restoredHeight, snapshotDir))
	require.NoError(t, flatkv.VerifySnapshotDir(snapshotDir, restoredHeight))
	commitNextBlock(t, producer)
	appHashes := publishedAppHashes(t, producer, restoredHeight, restoredHeight+1)
	return producedSnapshot{
		snapshotDir: snapshotDir,
		appHash:     appHashes[0].AppHash(),
		nextAppHash: appHashes[1].AppHash(),
	}
}

// commitNextBlock commits block restoredHeight+1 as every home below does, so each computes the same app hash.
func commitNextBlock(t *testing.T, manager *GigaStorageManager) {
	t.Helper()
	const next = restoredHeight + 1
	reportTestBlockHash(t, manager, next)
	commitBlock(t, manager, next, evmBlock(next, next))
}

// restoredHome returns the config of a fresh home holding only the FlatKV snapshot in snapshotDir, installed as
// `seid state-sync-snapshot restore` installs it, with the app hash builder's setup left to the caller.
func restoredHome(t *testing.T, snapshotDir string) *config.GigaStorageConfig {
	t.Helper()
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)
	disableSS(cfg)
	cfg.DeferAppHashBuilderSetup = true
	lock, err := flatkv.LockRoot(cfg.FlatKVConfig.DataDir)
	require.NoError(t, err)
	require.NoError(t, lock.InstallSnapshot(snapshotDir, restoredHeight))
	require.NoError(t, lock.Unlock())
	return cfg
}

// openRestoredHome opens a manager over cfg that is closed when the test ends.
func openRestoredHome(t *testing.T, cfg *config.GigaStorageConfig) *GigaStorageManager {
	t.Helper()
	manager, err := NewGigaStorageManager(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	return manager
}

// requireRestoredChain fails unless manager publishes the restore anchor of restoredHeight with produced's app hash,
// and computes produced's app hash of the next block when that block is committed.
func requireRestoredChain(t *testing.T, manager *GigaStorageManager, produced producedSnapshot) {
	t.Helper()
	anchor := publishedAppHashes(t, manager, restoredHeight, restoredHeight)[0]
	require.True(t, anchor.IsRestoreAnchor())
	require.Equal(t, produced.appHash, anchor.AppHash())

	commitNextBlock(t, manager)
	next := publishedAppHashes(t, manager, restoredHeight+1, restoredHeight+1)[0]
	require.Equal(t, produced.appHash, next.PreviousAppHash())
	require.Equal(t, produced.nextAppHash, next.AppHash())
}

// A restored home opens with the builder's setup left open, and completes it from the app hash it is given.
func TestRestoredHomeCompletesSetupFromTheAnchor(t *testing.T) {
	produced := produceSnapshot(t)
	manager := openRestoredHome(t, restoredHome(t, produced.snapshotDir))

	version, err := manager.SC().GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(restoredHeight), version)
	_, _, _, err = manager.AppHashBuilder().Bounds()
	require.Error(t, err, "the builder's setup is not complete before the caller completes it")

	require.NoError(t, manager.AppHashBuilder().CompleteRestoredSetup(t.Context(), restoredHeight, produced.appHash))
	requireRestoredChain(t, manager, produced)
}

// A restart after the anchor is stored, before any later block, completes setup from the stored anchor with a plain
// SetupComplete(), or with the same CompleteRestoredSetup() again.
func TestRestoredHomeRestartsFromTheStoredAnchor(t *testing.T) {
	completeSetups := map[string]func(*GigaStorageManager, [32]byte) error{
		"SetupComplete": func(manager *GigaStorageManager, _ [32]byte) error {
			return manager.AppHashBuilder().SetupComplete(t.Context(), restoredHeight)
		},
		"CompleteRestoredSetup": func(manager *GigaStorageManager, appHash [32]byte) error {
			return manager.AppHashBuilder().CompleteRestoredSetup(t.Context(), restoredHeight, appHash)
		},
	}
	for name, completeSetup := range completeSetups {
		t.Run(name, func(t *testing.T) {
			produced := produceSnapshot(t)
			cfg := restoredHome(t, produced.snapshotDir)
			seeded, err := NewGigaStorageManager(t.Context(), cfg)
			require.NoError(t, err)
			require.NoError(t, seeded.AppHashBuilder().CompleteRestoredSetup(
				t.Context(), restoredHeight, produced.appHash))
			require.NoError(t, seeded.Close())

			restarted := openRestoredHome(t, cfg)
			require.NoError(t, completeSetup(restarted, produced.appHash))
			requireRestoredChain(t, restarted, produced)
		})
	}
}
