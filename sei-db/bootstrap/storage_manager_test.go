package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
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

	require.NotNil(t, manager.BlockDB())
	require.NotNil(t, manager.ReceiptDB())
	require.NotNil(t, manager.StateWAL())
	require.NotNil(t, manager.SC())
	require.NotNil(t, manager.SS())

	heights, err := manager.readHeights()
	require.NoError(t, err)
	require.Equal(t, StoreHeights{ReceiptEnabled: true}, heights)

	require.NoError(t, manager.RecoverFromCrash())
}

// TestReceiptsDisabled pins that Enable is what decides whether the receipt store is opened at
// all, and that a manager without one still recovers.
func TestReceiptsDisabled(t *testing.T) {
	manager, _ := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.ReceiptDBConfig.Enable = false
	})

	require.Nil(t, manager.ReceiptDB())

	heights, err := manager.readHeights()
	require.NoError(t, err)
	require.False(t, heights.ReceiptEnabled)

	require.NoError(t, manager.RecoverFromCrash())
}

// TestReopenAfterClose proves Close releases what the next open needs. SC holds a file lock and
// adopts the state WAL, so a Close that missed either of them, or closed the WAL twice, shows up
// here rather than as a node that will not restart.
func TestReopenAfterClose(t *testing.T) {
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)

	first, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, first.RecoverFromCrash())
	require.NoError(t, first.Close())

	second, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, second.RecoverFromCrash())
	require.NoError(t, second.Close())
}

// TestCloseOnAPartialOpen covers the constructor's own cleanup: a config that fails partway must
// leave nothing holding the home directory, which the successful reopen afterwards is the proof
// of.
func TestCloseOnAPartialOpen(t *testing.T) {
	cfg, err := config.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)

	// The block ledger is opened last, so an invalid one fails after every other store is open.
	broken := cfg
	broken.BlockDBConfig = brokenBlockDBConfig(cfg)
	_, err = NewGigaStorageManager(context.Background(), broken)
	require.Error(t, err)
	require.ErrorContains(t, err, "open block db")

	manager, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, manager.Close())
}

// TestNoPruningConfigStartsNoCollector pins that pruning is opt-in: without a config the stores
// keep their own retention, and SC in particular is not left with its pruner stood down and
// nothing in its place.
func TestNoPruningConfigStartsNoCollector(t *testing.T) {
	manager, cfg := openManager(t, func(cfg *config.GigaStorageConfig) {
		cfg.PruningConfig = nil
	})

	require.Nil(t, manager.gc)
	require.False(t, cfg.FlatKVConfig.ExternalPruning)
	require.False(t, manager.SC().ExternalPruning())
}

// TestPruningConfigHandsSCRetentionToTheCollector pins the other half: with a collector running,
// SC stops pruning its own snapshots and truncating the WAL, and it is registered with the
// collector that takes both over. The caller's config is left untouched.
func TestPruningConfigHandsSCRetentionToTheCollector(t *testing.T) {
	manager, cfg := openManager(t, nil)

	require.NotNil(t, manager.gc)
	require.False(t, cfg.FlatKVConfig.ExternalPruning, "the caller's config must not be mutated")
	require.True(t, manager.SC().ExternalPruning())

	names := make([]string, 0, 3)
	for _, store := range manager.prunableStores() {
		names = append(names, store.Name())
	}
	require.Equal(t, []string{"FlatKV", "StateWAL"}, names,
		"the EVM state store, the pebbledb receipt store and the block ledger are not PrunableStores yet")
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
