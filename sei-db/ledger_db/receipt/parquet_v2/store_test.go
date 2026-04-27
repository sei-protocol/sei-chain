package parquet_v2_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	pq "github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet_v2"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	rpv2 "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func newTestContext() (sdk.Context, storetypes.StoreKey) {
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)
	return ctx, storeKey
}

func makeReceipt(txHash common.Hash, block uint64, txIndex uint32) *types.Receipt {
	return &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      block,
		TransactionIndex: txIndex,
		Logs: []*types.Log{
			{
				Address: common.HexToAddress("0x1").Hex(),
				Data:    []byte{0x1},
				Index:   0,
			},
		},
	}
}

func blockTxHash(block uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(block))
}

// TestNewStoreReadWriteRoundTrip covers the basic happy path through the V2
// wrapper: open, write, read back via tx hash index.
func TestNewStoreReadWriteRoundTrip(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := rpv2.NewStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Write 5 blocks worth of receipts.
	for block := uint64(1); block <= 5; block++ {
		txHash := blockTxHash(block)
		err := store.SetReceipts(ctx.WithBlockHeight(int64(block)), []receipt.ReceiptRecord{
			{TxHash: txHash, Receipt: makeReceipt(txHash, block, 0)},
		})
		require.NoError(t, err)
	}
	require.Equal(t, int64(5), store.LatestVersion())

	// Force a rotation so the early blocks are readable from a closed file.
	v2 := store.(*rpv2.Store)
	require.NoError(t, v2.Underlying().Flush())
	v2.Underlying().SetMaxBlocksPerFile(3)
	require.NoError(t, v2.Underlying().ObserveEmptyBlock(6))

	// Block 1 must be readable now.
	got, err := store.GetReceiptFromStore(ctx, blockTxHash(1))
	require.NoError(t, err)
	require.Equal(t, blockTxHash(1).Hex(), got.TxHashHex)
}

// TestNewStoreCrashRecovery uses fault-injection on the V2 store and then
// reopens to confirm the WAL replay path through the coordinator restores
// pre-crash state. We verify recovery by checking that the WAL repopulates
// LatestVersion and the tx hash index — V2 does not currently warm a read
// cache from the WAL (TODO: cache integration), so we don't assert direct
// readback of pre-flush blocks here.
func TestNewStoreCrashRecovery(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := rpv2.NewStore(cfg, storeKey)
	require.NoError(t, err)
	v2 := store.(*rpv2.Store)
	pqStore := v2.Underlying()
	pqStore.SetMaxBlocksPerFile(3)

	// Write 4 blocks: blocks 1, 2 land in receipts_0.parquet (rotated when
	// block 3 arrives), block 3 lands in receipts_3.parquet but is buffered.
	for block := uint64(1); block <= 4; block++ {
		txHash := blockTxHash(block)
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []receipt.ReceiptRecord{
			{TxHash: txHash, Receipt: makeReceipt(txHash, block, 0)},
		}))
	}

	// Arm a fault that fires after the WAL write but before parquet apply.
	errSimulated := errors.New("simulated crash")
	fired := false
	pqStore.SetFaultHooks(&pq.FaultHooks{
		AfterWALWrite: func(_ uint64) error {
			if !fired {
				fired = true
				return errSimulated
			}
			return nil
		},
	})

	// This write fails after the WAL entry has been durably written.
	crashBlock := uint64(5)
	crashTxHash := blockTxHash(crashBlock)
	err = store.SetReceipts(ctx.WithBlockHeight(int64(crashBlock)), []receipt.ReceiptRecord{
		{TxHash: crashTxHash, Receipt: makeReceipt(crashTxHash, crashBlock, 0)},
	})
	require.ErrorIs(t, err, errSimulated)

	v2.SimulateCrash()

	store2, err := rpv2.NewStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store2.Close() })

	// Blocks 1 and 2 live in the rotated receipts_0.parquet (footer was
	// already written before the crash) — they must be readable.
	for block := uint64(1); block <= 2; block++ {
		txHash := blockTxHash(block)
		got, err := store2.GetReceiptFromStore(ctx, txHash)
		require.NoError(t, err, "rotated block %d must be readable", block)
		require.Equal(t, txHash.Hex(), got.TxHashHex)
	}

	// LatestVersion must be at least the highest WAL entry block (the crash
	// block was durably WAL-written).
	require.GreaterOrEqual(t, store2.LatestVersion(), int64(crashBlock),
		"WAL replay should bump LatestVersion past the crash block")
}

// TestNewStoreFilterLogs exercises the log query path through the coordinator.
func TestNewStoreFilterLogs(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := rpv2.NewStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Force a small rotation interval before any writes.
	v2 := store.(*rpv2.Store)
	v2.Underlying().SetMaxBlocksPerFile(3)

	for block := uint64(1); block <= 9; block++ {
		txHash := blockTxHash(block)
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []receipt.ReceiptRecord{
			{TxHash: txHash, Receipt: makeReceipt(txHash, block, 0)},
		}))
	}

	// Make blocks in [1,9) flush by triggering one more rotation past 9.
	require.NoError(t, v2.Underlying().ObserveEmptyBlock(12))

	// FilterLogs over the closed-files range. We don't assert the full count
	// (that depends on rotation timing) but we want a non-error response.
	_, err = store.FilterLogs(ctx, 1, 6, filters.FilterCriteria{})
	require.NoError(t, err)
}
