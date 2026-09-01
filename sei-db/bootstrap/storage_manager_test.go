package bootstrap

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// openManager opens a manager over a fresh home directory, applying tweak to the default config
// first. It registers the Close, so a leaked file lock fails the test that took it.
func openManager(t *testing.T, tweak func(*config.GigaStorageConfig)) (*GigaStorageManager, config.GigaStorageConfig) {
	t.Helper()
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)
	if tweak != nil {
		tweak(&cfg)
	}
	manager, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	return manager, cfg
}

// TestOpenFreshHome covers the shape a node boots into on a new home directory: every store
// opens, every store is at genesis, and recovery has nothing to reconcile.
func TestOpenFreshHome(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.NotNil(t, manager.BlockStore())
	require.NotNil(t, manager.ReceiptDB())
	require.NotNil(t, manager.StateWAL())
	require.NotNil(t, manager.SC())
	require.NotNil(t, manager.StateDB())
	require.NotNil(t, manager.SS())

	scVersion, err := manager.SC().GetLatestVersion()
	require.NoError(t, err)
	require.Zero(t, scVersion)
	require.Zero(t, manager.SS().GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())

	// Recovery already ran inside the constructor, and running it again must find the same nothing to
	// do: it is the only step here a restart repeats.
	require.NoError(t, manager.CrashRecover())
}

// TestOpenLoadsTheCommitStore pins that SC is opened rather than merely constructed.
//
// NewCommitStore returns a store that reports no version until it is loaded, so a manager that
// skipped the load would hand recovery a state height of 0 and roll every other store back to
// genesis. A loaded store also holds the file lock, which is what TestReopenAfterClose relies on.
func TestOpenLoadsTheCommitStore(t *testing.T) {
	manager, _ := openManager(t, nil)

	version, err := manager.SC().GetLatestVersion()
	require.NoError(t, err)
	require.Zero(t, version, "a fresh home is at genesis, but the version must be readable")
	require.NotNil(t, manager.SC().Name(), "a loaded store answers as a prunable store")
}

// TestCheckpointScheduleCoversBothHalvesOfState pins that the schedule is started and that SS can act
// on a height it picks. SC and SS restore from their own snapshots, so a rollback can only target a
// height both of them hold — which is what one shared schedule delivers and two intervals do not.
func TestCheckpointScheduleCoversBothHalvesOfState(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.NotNil(t, manager.checkpointer)
	require.NotNil(t, manager.SS().Snapshots(),
		"SS takes no snapshot at all without a snapshot manager, so a height the schedule picks would be dropped")
}

// TestReceiptsDisabled pins that Enable is what decides whether the receipt store is opened at
// all, and that a manager without one still opens, recovers and closes.
func TestReceiptsDisabled(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.ReceiptDBConfig.Enable = false
	})

	require.Nil(t, manager.ReceiptDB())
	require.NotNil(t, manager.SC())
	require.NotNil(t, manager.StateDB())
	require.NotNil(t, manager.SS())

	require.NoError(t, manager.CrashRecover())
}

// TestStateStoreDisabled pins that SS has an Enable of its own, and that every step after the open
// tolerates its absence: the checkpoint schedule, the prune cycle, recovery, and Close.
func TestStateStoreDisabled(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.SSConfig.Enable = false
	})

	require.Nil(t, manager.SS())
	require.NotNil(t, manager.SC())
	require.NotNil(t, manager.StateDB())
	require.NotNil(t, manager.BlockStore())
	require.NotNil(t, manager.ReceiptDB())

	require.NotNil(t, manager.checkpointer, "SC still needs the schedule that replaces its own interval")
	require.True(t, manager.SC().ExternalPruning())

	names := make([]string, 0, 4)
	for _, store := range manager.prunableStores() {
		names = append(names, store.Name())
	}
	require.Equal(t, []string{"FlatKV", "StateWAL", "ReceiptDB", "BlockDB"}, names,
		"a store this node never opened must not be offered to the collector")

	require.NoError(t, manager.CrashRecover())
}

// TestStateStoreDisabledNeedsNoEVMDirectory pins that the path SS would have opened under is not
// required by a node that opens no SS. Validate rejects an empty one only when SS is enabled.
func TestStateStoreDisabledNeedsNoEVMDirectory(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.SSConfig.Enable = false
		cfg.SSConfig.EVMDBDirectory = ""
	})

	require.Nil(t, manager.SS())
}

// TestReopenAfterClose proves Close releases what the next open needs. SC holds a file lock and
// the manager owns the state WAL, so a Close that missed either of them, or closed the WAL twice,
// shows up here rather than as a node that will not restart.
func TestReopenAfterClose(t *testing.T) {
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)

	first, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, first.CrashRecover())
	require.NoError(t, first.Close())

	second, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, second.CrashRecover())
	require.NoError(t, second.Close())
}

// TestCloseOnAPartialOpen covers the constructor's own cleanup: a config that fails partway must
// leave nothing holding the home directory, which the successful reopen afterwards is the proof
// of.
//
// The receipt store is opened second, so a broken one fails with the block ledger already open and
// nothing else. That is the case worth pinning: a store opened before the failure is one Close has to
// reach, where a failure on the first store leaves nothing to clean up.
func TestCloseOnAPartialOpen(t *testing.T) {
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)

	broken := cfg
	broken.ReceiptDBConfig.DBDirectory = "" // NewReceiptStore refuses an unset directory
	_, err = NewGigaStorageManager(context.Background(), broken)
	require.Error(t, err)
	require.ErrorContains(t, err, "open receipt store")

	manager, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, manager.Close())
}

// TestCloseOnAFailureBeforeAnyStoreOpens covers the other end of the same cleanup: the block ledger is
// opened first, so a broken one fails with nothing open at all.
func TestCloseOnAFailureBeforeAnyStoreOpens(t *testing.T) {
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)

	broken := cfg
	broken.BlockDBConfig = brokenBlockDBConfig(cfg)
	_, err = NewGigaStorageManager(context.Background(), broken)
	require.Error(t, err)
	require.ErrorContains(t, err, "open block db")

	manager, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, manager.Close())
}

// TestAnInvalidConfigIsRefusedBeforeAnyStoreOpens pins that config validation runs first, using a
// config GigaStorageConfig.Validate rejects. An untouched home directory afterwards is the
// proof: a config no store will accept costs no files.
func TestAnInvalidConfigIsRefusedBeforeAnyStoreOpens(t *testing.T) {
	home := t.TempDir()
	cfg, err := config.DefaultGigaStorageConfig(home)
	require.NoError(t, err)
	cfg.PruningConfig = nil

	_, err = NewGigaStorageManager(context.Background(), cfg)
	require.ErrorContains(t, err, "cannot open storage")

	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	require.Empty(t, entries, "validation must reject the config before a store creates its directory")
}

// TestStateDBCommitsToWALAndLiveSC pins that the manager hands out a StateDB whose write path
// reaches both layers: the WAL the manager owns, and the live SC opened with no WAL of its own.
func TestStateDBCommitsToWALAndLiveSC(t *testing.T) {
	manager, _ := openManager(t, nil)

	cs := []*proto.NamedChangeSet{{
		Name: "bank",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{{Key: []byte("k"), Value: []byte("v")}},
		},
	}}
	require.NoError(t, manager.StateDB().CommitStateChanges(1, cs))

	require.Equal(t, int64(1), manager.SC().Version())
	ok, first, last, err := manager.StateWAL().GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(1), last)
}

// TestEveryStoreJoinsThePruneCycle pins which stores the shared cut line covers.
//
// The cut line is a minimum over the floors the collector is handed, so a store left out cannot hold
// that line down to the height it can still restore to: a rollback inside the configured window can
// find that store already pruned past the target. Every store a Giga node runs is in, which is why
// prunableStores warns about any that is not.
func TestEveryStoreJoinsThePruneCycle(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.NotNil(t, manager.gc)
	require.True(t, manager.SC().ExternalPruning())
	require.True(t, manager.SS().ExternalPruning())

	names := make([]string, 0, 5)
	for _, store := range manager.prunableStores() {
		names = append(names, store.Name())
	}
	require.Equal(t, []string{"FlatKV", "StateWAL", "ReceiptDB", "EVM SS", "BlockDB"}, names)
}

// TestPrunableStoresOmitsDisabledReceipts pins that a store that was never opened is not offered
// to the collector.
func TestPrunableStoresOmitsDisabledReceipts(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.ReceiptDBConfig.Enable = false
	})

	var stores []controller.PrunableStore
	stores = append(stores, manager.prunableStores()...)
	for _, store := range stores {
		require.NotEqual(t, "ReceiptDB", store.Name())
	}
}

// brokenBlockDBConfig returns a copy of cfg's block ledger config that NewBlockDB rejects,
// leaving the original untouched so the reopen afterwards uses a valid one.
func brokenBlockDBConfig(cfg config.GigaStorageConfig) *littblock.BlockDBConfig {
	broken := *cfg.BlockDBConfig
	litt := *broken.Litt
	litt.Paths = nil // NewBlockDB refuses an empty path list
	broken.Litt = &litt
	return &broken
}
