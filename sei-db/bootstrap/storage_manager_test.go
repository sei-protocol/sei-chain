package bootstrap

import (
	"context"
	"os"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/stretchr/testify/require"
)

// newHome returns the default config of a fresh home directory, with tweak applied to it.
func newHome(t *testing.T, tweak func(*config.GigaStorageConfig)) *config.GigaStorageConfig {
	t.Helper()
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)
	if tweak != nil {
		tweak(cfg)
	}
	return cfg
}

// startManager opens a manager over cfg. It registers the Close, so a leaked file lock fails the test that took it.
func startManager(t *testing.T, cfg *config.GigaStorageConfig) *GigaStorageManager {
	t.Helper()
	manager, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	return manager
}

// openManager opens a manager over a fresh home directory, applying tweak to the default config first.
func openManager(t *testing.T, tweak func(*config.GigaStorageConfig)) (*GigaStorageManager, *config.GigaStorageConfig) {
	t.Helper()
	cfg := newHome(t, tweak)
	return startManager(t, cfg), cfg
}

// TestOpenFreshHome covers the shape a node boots into on a new home directory: every store
// opens and every store is at genesis.
func TestOpenFreshHome(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.NotNil(t, manager.ReceiptDB())
	require.NotNil(t, historicalStateDB(manager))

	scVersion, err := manager.StateDB().SC().GetLatestVersion()
	require.NoError(t, err)
	require.Zero(t, scVersion)
	require.Zero(t, historicalStateDB(manager).GetLatestVersion())
	require.Zero(t, manager.ReceiptDB().LatestVersion())
}

// TestOpenLoadsTheCommitStore pins that SC is opened rather than merely constructed.
//
// NewCommitStore returns a store that reports no version until it is loaded, so a manager that
// skipped the load would hand recovery a state height of 0 and roll every other store back to
// genesis. A loaded store also holds the file lock, which is what TestReopenAfterClose relies on.
func TestOpenLoadsTheCommitStore(t *testing.T) {
	manager, _ := openManager(t, nil)

	version, err := manager.StateDB().SC().GetLatestVersion()
	require.NoError(t, err)
	require.Zero(t, version, "a fresh home is at genesis, but the version must be readable")
	require.NotNil(t, manager.StateDB().SC().Name(), "a loaded store answers as a prunable store")
}

// TestReceiptsDisabled pins that Enable is what decides whether the receipt store is opened at
// all, and that a manager without one still opens and closes.
func TestReceiptsDisabled(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.ReceiptDBConfig.Enable = false
	})

	require.Nil(t, manager.ReceiptDB())
	require.NotNil(t, historicalStateDB(manager))
}

// TestReceiptsDisabledRefusesAStoreWithHistory pins that a node cannot run with receipts disabled while
// its receipt store still holds receipts. Re-enabling them later would converge every store on that
// store's stale head, so the transition is refused up front for the operator to clear the directory.
func TestReceiptsDisabledRefusesAStoreWithHistory(t *testing.T) {
	cfg := newHome(t, nil)
	writeReceipts(t, cfg, 2)

	cfg.ReceiptDBConfig.Enable = false
	_, err := NewGigaStorageManager(t.Context(), cfg)
	require.ErrorIs(t, err, ErrReceiptStoreDisabledWithHistory)

	// The refusal releases everything it opened: the same home opens once receipts are enabled again.
	cfg.ReceiptDBConfig.Enable = true
	second, err := NewGigaStorageManager(t.Context(), cfg)
	require.NoError(t, err)
	require.Equal(t, int64(2), second.ReceiptDB().LatestVersion())
	require.NoError(t, second.Close())
}

// TestHistoricalStateDBDisabledRefusesAStoreWithHistory pins that a node cannot run with the historical
// state DB disabled while its directory holds anything. Re-enabling it later would converge every store on
// that store's stale head, so the transition is refused up front for the operator to clear the directory.
func TestHistoricalStateDBDisabledRefusesAStoreWithHistory(t *testing.T) {
	cfg := newHome(t, nil)
	writeState(t, cfg, 2)
	writeAppHashes(t, cfg, 2)

	cfg.SSConfig.Enable = false
	_, err := NewGigaStorageManager(t.Context(), cfg)
	require.ErrorContains(t, err, cfg.SSConfig.EVMDBDirectory)

	// The refusal releases everything it opened: the same home opens once the store is enabled again.
	cfg.SSConfig.Enable = true
	second, err := NewGigaStorageManager(t.Context(), cfg)
	require.NoError(t, err)
	require.Equal(t, int64(2), historicalStateDB(second).GetLatestVersion())
	require.NoError(t, second.Close())
}

// TestReceiptsDisabledTakesAnEmptyStore pins that a receipt store that was opened but never written
// does not block disabling receipts.
func TestReceiptsDisabledTakesAnEmptyStore(t *testing.T) {
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)
	first, err := NewGigaStorageManager(t.Context(), cfg)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	cfg.ReceiptDBConfig.Enable = false
	second, err := NewGigaStorageManager(t.Context(), cfg)
	require.NoError(t, err)
	require.Nil(t, second.ReceiptDB())
	require.NoError(t, second.Close())
}

// TestStateStoreDisabled pins that SS has an Enable of its own, and that every step after the open
// tolerates its absence: the checkpoint schedule, the prune cycle, and Close.
func TestStateStoreDisabled(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.SSConfig.Enable = false
	})

	require.Nil(t, historicalStateDB(manager))
	require.NotNil(t, manager.ReceiptDB())

	require.NotNil(t, manager.StateDB().CheckpointScheduler(),
		"SC still needs the schedule that replaces its own interval")
	require.True(t, manager.StateDB().SC().ExternalPruning())

	names := make([]string, 0, 5)
	for _, store := range managerPrunableStores(manager) {
		names = append(names, store.Name())
	}
	require.Equal(t, []string{"FlatKV", "StateWAL", "ReceiptDB", "BlockDB", "AppHashVault"}, names,
		"a store this node never opened must not be offered to the collector")
}

// TestStateStoreDisabledNeedsNoEVMDirectory pins that the path SS would have opened under is not
// required by a node that opens no SS. Validate rejects an empty one only when SS is enabled.
func TestStateStoreDisabledNeedsNoEVMDirectory(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.SSConfig.Enable = false
		cfg.SSConfig.EVMDBDirectory = ""
	})

	require.Nil(t, historicalStateDB(manager))
}

// TestReopenAfterClose proves Close releases what the next open needs. SC holds a file lock and
// the manager owns the state WAL, so a Close that missed either of them, or closed the WAL twice,
// shows up here rather than as a node that will not restart.
func TestReopenAfterClose(t *testing.T) {
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)

	first, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, second.Close())
}

// TestCloseOnAPartialOpen covers the constructor's own cleanup: a config that fails partway must
// leave nothing holding the home directory, which the successful reopen afterwards is the proof
// of.
//
// The receipt store is opened last, so a broken one fails with every other store already open. That is
// the case worth pinning: each store opened before the failure is one the cleanup has to reach, where a
// failure on the first store leaves nothing to clean up.
func TestCloseOnAPartialOpen(t *testing.T) {
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)

	broken := *cfg
	broken.ReceiptDBConfig.DBDirectory = "" // NewReceiptStore refuses an unset directory
	_, err = NewGigaStorageManager(context.Background(), &broken)
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

	broken := *cfg
	broken.BlockDBConfig = brokenBlockDBConfig(cfg)
	_, err = NewGigaStorageManager(context.Background(), &broken)
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

	changes := &gigatypes.StateChangeSet{
		Misc: []gigatypes.MiscChange{{Module: "bank", Key: []byte("k"), Value: []byte("v")}},
	}
	commitBlock(t, manager, 1, changes)
	historicalStateDB(manager).WaitForPendingWrites()

	require.Equal(t, int64(1), manager.StateDB().SC().Version())
	require.Equal(t, int64(1), historicalStateDB(manager).GetLatestVersion(),
		"a committed block must advance the EVM state store even when it carries no EVM keys")
	ok, first, last, err := manager.StateDB().WAL().GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), first)
	require.Equal(t, uint64(1), last)
}

func TestStateDBCommitsEVMChangesToSS(t *testing.T) {
	manager, _ := openManager(t, nil)

	commitBlock(t, manager, 1, evmBlock(1, 1))
	historicalStateDB(manager).WaitForPendingWrites()

	require.Equal(t, int64(1), historicalStateDB(manager).GetLatestVersion())
	view, ok := manager.StateDB().OpenViewAt(1)
	require.True(t, ok)
	defer view.Close()
	require.Equal(t, uint64(1), view.GetNonce(gigatypes.Address{1}))
}

// TestEveryStoreJoinsThePruneCycle pins which stores the shared cut line covers.
//
// The cut line is a minimum over the floors the collector is handed, so a store left out cannot hold
// that line down to the height it can still restore to: a rollback inside the configured window can
// find that store already pruned past the target. Every store a Giga node runs is in, which is why
// prunableStores warns about any that is not.
func TestEveryStoreJoinsThePruneCycle(t *testing.T) {
	manager, _ := openManager(t, nil)

	require.True(t, manager.StateDB().SC().ExternalPruning())
	require.True(t, historicalStateDB(manager).ExternalPruning())

	names := make([]string, 0, 6)
	for _, store := range managerPrunableStores(manager) {
		names = append(names, store.Name())
	}
	// The three state stores arrive as one group, from the StateDB that owns them. Order carries no
	// meaning to the collector: it fixes both cut lines as a minimum over every store before pruning
	// any of them.
	require.Equal(t, []string{"FlatKV", "StateWAL", "EVM SS undo", "ReceiptDB", "BlockDB", "AppHashVault"}, names)
}

// TestPrunableStoresOmitsDisabledReceipts pins that a store that was never opened is not offered
// to the collector.
func TestPrunableStoresOmitsDisabledReceipts(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.ReceiptDBConfig.Enable = false
	})

	var stores []controllertypes.PrunableStore
	stores = append(stores, managerPrunableStores(manager)...)
	for _, store := range stores {
		require.NotEqual(t, "ReceiptDB", store.Name())
	}
}

// managerPrunableStores returns the stores manager's prune cycle covers.
func managerPrunableStores(manager *GigaStorageManager) []controllertypes.PrunableStore {
	return prunableStores(manager.stateDB, manager.receiptDB, manager.blockStore, manager.appHashVault)
}

// brokenBlockDBConfig returns a copy of cfg's block ledger config that New rejects,
// leaving the original untouched so the reopen afterwards uses a valid one.
func brokenBlockDBConfig(cfg *config.GigaStorageConfig) *blockstore.Config {
	broken := *cfg.BlockDBConfig
	litt := *broken.Litt
	litt.Paths = nil // New refuses an empty path list
	broken.Litt = &litt
	return &broken
}
