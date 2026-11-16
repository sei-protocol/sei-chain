package bootstrap

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// withRunningMarker places the running marker in its own temporary directory.
func withRunningMarker(t *testing.T) func(*config.GigaStorageConfig) {
	markerPath := filepath.Join(t.TempDir(), "node_running")
	return func(cfg *config.GigaStorageConfig) {
		cfg.RunningMarkerPath = markerPath
	}
}

// commitNodeBlocks feeds blocks 1 through through to every store, as execution does.
func commitNodeBlocks(t *testing.T, manager *GigaStorageManager, through byte) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey("evm")
	ctx := testutil.DefaultContext(storeKey, storetypes.NewTransientStoreKey("evm_transient"))
	for block := byte(1); block <= through; block++ {
		reportTestBlockHash(t, manager, uint64(block))
		txHash := common.BigToHash(new(big.Int).SetUint64(uint64(block)))
		records := []receipt.ReceiptRecord{{
			TxHash:  txHash,
			Receipt: &evmtypes.Receipt{TxHashHex: txHash.Hex(), BlockNumber: uint64(block), GasUsed: 21000},
		}}
		require.NoError(t, manager.ReceiptDB().SetReceipts(ctx.WithBlockHeight(int64(block)), records))
		commitBlock(t, manager, int64(block), evmBlock(block, block))
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
	commitNodeBlocks(t, manager, 5)

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
	commitNodeBlocks(t, manager, 2)
	require.NoError(t, manager.Close())
	requireRunningMarker(t, cfg, false)

	// A crash leaves the marker behind.
	require.NoError(t, os.WriteFile(cfg.RunningMarkerPath, nil, 0o600))
	_ = startManager(t, cfg)
	requireRunningMarker(t, cfg, true)
}
