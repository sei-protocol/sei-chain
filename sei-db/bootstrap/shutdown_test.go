package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// withRunningMarker places the running marker in its own temporary directory.
func withRunningMarker(t *testing.T) func(*config.GigaStorageConfig) {
	markerPath := filepath.Join(t.TempDir(), "node_running")
	return func(cfg *config.GigaStorageConfig) {
		cfg.RunningMarkerPath = markerPath
	}
}

// requireRunningMarker requires the running marker of cfg to exist, or not.
func requireRunningMarker(t *testing.T, cfg *config.GigaStorageConfig, exists bool) {
	t.Helper()
	_, err := os.Stat(cfg.RunningMarkerPath)
	if exists {
		require.NoError(t, err)
		return
	}
	require.ErrorIs(t, err, os.ErrNotExist)
}

// After Close, every store is at the last block and the live state DB needs nothing from the state WAL.
func TestCloseLeavesEveryStoreAtTheLastBlock(t *testing.T) {
	cfg := newHome(t, nil)
	manager, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	commitBlocks(t, manager, 5)

	require.NoError(t, manager.Close())

	heads, err := readStoreHeads(cfg)
	require.NoError(t, err)
	require.Equal(t, storeHeads{
		stateWAL:          5,
		historicalStateDB: utils.Some[uint64](5),
		receiptStore:      5,
		appHashVault:      5,
	}, heads)
	liveStateDB, err := flatkv.OpenLiveStateDB(t.Context(), cfg.FlatKVConfig)
	require.NoError(t, err)
	defer func() { require.NoError(t, liveStateDB.Close()) }()
	require.Equal(t, int64(5), liveStateDB.Version(), "the live state DB needs no block from the state WAL")
}

// The running marker exists while the stores are open, Close removes it, and one left by a crash survives the next
// open.
func TestCloseRemovesTheRunningMarker(t *testing.T) {
	cfg := newHome(t, withRunningMarker(t))

	manager, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)
	requireRunningMarker(t, cfg, true)
	commitBlocks(t, manager, 2)
	require.NoError(t, manager.Close())
	requireRunningMarker(t, cfg, false)

	// A crash leaves the marker behind.
	require.NoError(t, os.WriteFile(cfg.RunningMarkerPath, nil, 0o600))
	_ = startManager(t, cfg)
	requireRunningMarker(t, cfg, true)
}

// A shutdown that cannot bring every store to the same block fails Close and leaves the running marker in place.
func TestCloseKeepsTheRunningMarkerWhenTheStoresDisagree(t *testing.T) {
	cfg := newHome(t, withRunningMarker(t))
	manager, err := NewGigaStorageManager(context.Background(), cfg)
	require.NoError(t, err)

	// Every store but the receipt store takes block 1.
	reportTestBlockHash(t, manager, 1)
	require.NoError(t, manager.StateDB().CommitStateChanges(1, evmBlock(1, 1)))
	require.NoError(t, manager.ReceiptHasher().Submit(t.Context(), 1, nil))

	require.ErrorContains(t, manager.Close(), "the receipt store stands at block 0")
	requireRunningMarker(t, cfg, true)
}
