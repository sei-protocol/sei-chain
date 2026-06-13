package receipt

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func extractParquetV2Store(t *testing.T, store ReceiptStore) *parquet_v2.Store {
	t.Helper()
	cached, ok := store.(*cachedReceiptStore)
	require.True(t, ok, "expected *cachedReceiptStore")
	pq, ok := cached.backend.(*parquetReceiptStoreV2)
	require.True(t, ok, "expected *parquetReceiptStoreV2 backend")
	return pq.store
}

// TestBuildParquetReceiptInputsResetsLogIndexAcrossBlockZero pins down the
// bug where currentBlock==0 was used as a "no current block yet" sentinel.
// A batch that starts at block 0 and crosses into a non-zero block must
// still reset logStartIndex at the boundary; otherwise the first log of
// the new block inherits the prior block's running index.
func TestBuildParquetReceiptInputsResetsLogIndexAcrossBlockZero(t *testing.T) {
	addr := common.HexToAddress("0x9001")
	topic := common.HexToHash("0x9002")
	txHash0 := common.HexToHash("0x9000")
	txHash5 := common.HexToHash("0x9050")

	inputs, err := buildParquetReceiptInputs([]ReceiptRecord{
		{TxHash: txHash0, Receipt: makeTestReceipt(txHash0, 0, 0, addr, []common.Hash{topic})},
		{TxHash: txHash5, Receipt: makeTestReceipt(txHash5, 5, 0, addr, []common.Hash{topic})},
	})
	require.NoError(t, err)
	require.Len(t, inputs, 2)
	require.Len(t, inputs[0].Logs, 1)
	require.Len(t, inputs[1].Logs, 1)
	require.Equal(t, uint32(0), inputs[0].Logs[0].LogIndex, "block 0's first log must have LogIndex 0")
	require.Equal(t, uint32(0), inputs[1].Logs[0].LogIndex, "first log of the post-block-0 block must restart LogIndex at 0")
}

// TestBuildParquetReceiptInputsSortsNonContiguousBlocks guards against a
// duplicate-LogIndex bug: with input order [block5-A, block3, block5-B] the
// pre-sort code reset logStartIndex twice for block 5, so block5-A's logs
// and block5-B's logs both started at 0. After groupReceiptInputsByBlock
// merged them into one batch, block 5 contained duplicate LogIndex values.
// The fix stable-sorts by block number before computing log indices.
func TestBuildParquetReceiptInputsSortsNonContiguousBlocks(t *testing.T) {
	addr := common.HexToAddress("0xa001").Hex()
	topic := common.HexToHash("0xa002").Hex()
	mkReceipt := func(txHash common.Hash, blockNumber uint64, numLogs int) *types.Receipt {
		logs := make([]*types.Log, numLogs)
		for i := 0; i < numLogs; i++ {
			logs[i] = &types.Log{
				Address: addr,
				Topics:  []string{topic},
				Data:    []byte{0x1},
				Index:   uint32(i), //nolint:gosec // test data
			}
		}
		return &types.Receipt{
			TxHashHex:   txHash.Hex(),
			BlockNumber: blockNumber,
			Logs:        logs,
		}
	}

	txHash5A := common.HexToHash("0x5A")
	txHash3 := common.HexToHash("0x03")
	txHash5B := common.HexToHash("0x5B")

	inputs, err := buildParquetReceiptInputs([]ReceiptRecord{
		{TxHash: txHash5A, Receipt: mkReceipt(txHash5A, 5, 2)},
		{TxHash: txHash3, Receipt: mkReceipt(txHash3, 3, 1)},
		{TxHash: txHash5B, Receipt: mkReceipt(txHash5B, 5, 2)},
	})
	require.NoError(t, err)
	require.Len(t, inputs, 3)

	// After the stable sort, the iteration order is block3, block5-A, block5-B.
	require.Equal(t, uint64(3), inputs[0].Receipt.BlockNumber)
	require.Equal(t, uint64(5), inputs[1].Receipt.BlockNumber)
	require.Equal(t, uint64(5), inputs[2].Receipt.BlockNumber)

	require.Len(t, inputs[0].Logs, 1)
	require.Equal(t, uint32(0), inputs[0].Logs[0].LogIndex)

	require.Len(t, inputs[1].Logs, 2)
	require.Equal(t, uint32(0), inputs[1].Logs[0].LogIndex)
	require.Equal(t, uint32(1), inputs[1].Logs[1].LogIndex)

	require.Len(t, inputs[2].Logs, 2)
	require.Equal(t, uint32(2), inputs[2].Logs[0].LogIndex)
	require.Equal(t, uint32(3), inputs[2].Logs[1].LogIndex)
}

func TestParquetV2ReceiptStoreReopenQueries(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet_v2"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	txHash := common.HexToHash("0x220")
	addr := common.HexToAddress("0x300")
	topic := common.HexToHash("0x5678")
	receipt := makeTestReceipt(txHash, 5, 1, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(5), []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))
	require.NoError(t, store.Close())

	store, err = NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	got, err := store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, got.TxHashHex)

	logs, err := store.FilterLogs(ctx, 3, 5, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(5), logs[0].BlockNumber)
}

func TestParquetV2DuplicateHashLogsSurviveReopen(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet_v2"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	txHash := common.HexToHash("0x333")
	addr := common.HexToAddress("0x3330")
	topic := common.HexToHash("0x3331")
	for _, block := range []uint64{1, 2} {
		receipt := makeTestReceipt(txHash, block, 0, addr, []common.Hash{topic})
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
			{TxHash: txHash, Receipt: receipt},
		}))
	}
	require.NoError(t, store.Close())

	store, err = NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	logs, err := store.FilterLogs(ctx, 1, 2, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	require.Equal(t, []uint64{1, 2}, []uint64{logs[0].BlockNumber, logs[1].BlockNumber})
	require.Equal(t, txHash, logs[0].TxHash)
	require.Equal(t, txHash, logs[1].TxHash)
}

func TestParquetV2MixedBlockBatchUsesReceiptBlockNumbers(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet_v2"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	addr := common.HexToAddress("0x4545")
	topic := common.HexToHash("0x4546")
	txHash5 := common.HexToHash("0x5005")
	txHash7 := common.HexToHash("0x7007")
	receipt5 := makeTestReceipt(txHash5, 5, 0, addr, []common.Hash{topic})
	receipt7 := makeTestReceipt(txHash7, 7, 0, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(7), []ReceiptRecord{
		{TxHash: txHash7, Receipt: receipt7},
		{TxHash: txHash5, Receipt: receipt5},
	}))
	require.NoError(t, store.Close())

	store, err = NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	got5, err := store.GetReceiptFromStore(ctx, txHash5)
	require.NoError(t, err)
	require.Equal(t, receipt5.TxHashHex, got5.TxHashHex)
	got7, err := store.GetReceiptFromStore(ctx, txHash7)
	require.NoError(t, err)
	require.Equal(t, receipt7.TxHashHex, got7.TxHashHex)

	backend := store.(*cachedReceiptStore).backend.(*parquetReceiptStoreV2)
	blockNum, ok, err := backend.txHashIndex.GetBlockNumber(ctx.Context(), txHash5)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), blockNum)
	blockNum, ok, err = backend.txHashIndex.GetBlockNumber(ctx.Context(), txHash7)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(7), blockNum)

	logs, err := store.FilterLogs(ctx, 5, 7, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	require.Equal(t, []uint64{5, 7}, []uint64{logs[0].BlockNumber, logs[1].BlockNumber})
	require.Equal(t, txHash5, logs[0].TxHash)
	require.Equal(t, txHash7, logs[1].TxHash)
}

// TestParquetV2SetReceiptsRejectsNonMonotonicBatchPreservesEarlierBlock
// reproduces the user-reported data-loss scenario at the receipt-store
// layer: with MaxBlocksPerFile=4 we write block 5, then issue a SetReceipts
// whose grouped batches sort to [4, 6]. The block-4 batch must be rejected
// before any rotation runs so the receipts_4.parquet file holding block 5
// is not truncated by a second initWriters os.Create. After close/reopen
// block 5's receipt and log must remain readable.
func TestParquetV2SetReceiptsRejectsNonMonotonicBatchPreservesEarlierBlock(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet_v2"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	pqStore := extractParquetV2Store(t, store)
	require.NoError(t, pqStore.SetMaxBlocksPerFile(4))

	addr := common.HexToAddress("0x880")
	topic := common.HexToHash("0x881")
	txHash5 := common.HexToHash("0x55")
	txHash4 := common.HexToHash("0x44")
	txHash6 := common.HexToHash("0x66")
	receipt5 := makeTestReceipt(txHash5, 5, 0, addr, []common.Hash{topic})
	receipt4 := makeTestReceipt(txHash4, 4, 0, addr, []common.Hash{topic})
	receipt6 := makeTestReceipt(txHash6, 6, 0, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(5), []ReceiptRecord{
		{TxHash: txHash5, Receipt: receipt5},
	}))

	err = store.SetReceipts(ctx.WithBlockHeight(6), []ReceiptRecord{
		{TxHash: txHash6, Receipt: receipt6},
		{TxHash: txHash4, Receipt: receipt4},
	})
	require.Error(t, err, "non-monotonic batch must be rejected at the receipt-store layer")
	require.ErrorContains(t, err, "non-monotonic")
	require.ErrorContains(t, err, "height 4")

	require.NoError(t, store.Close())

	store, err = NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, extractParquetV2Store(t, store).SetMaxBlocksPerFile(4))

	got, err := store.GetReceiptFromStore(ctx, txHash5)
	require.NoError(t, err, "block 5's receipt must survive after a rejected out-of-order batch")
	require.Equal(t, receipt5.TxHashHex, got.TxHashHex)

	_, err = store.GetReceiptFromStore(ctx, txHash4)
	require.ErrorIs(t, err, ErrNotFound, "rejected out-of-order receipt must not be persisted")

	logs, err := store.FilterLogs(ctx, 5, 5, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1, "block 5's log must survive after a rejected out-of-order batch")
	require.Equal(t, uint64(5), logs[0].BlockNumber)
}

func TestParquetV2EmptyBoundaryRotationFeedsClosedFileReads(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet_v2"
	cfg.DBDirectory = t.TempDir()
	cfg.TxIndexBackend = ""

	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)

	pqStore := extractParquetV2Store(t, store)
	require.NoError(t, pqStore.SetMaxBlocksPerFile(4))

	addr := common.HexToAddress("0x440")
	topic := common.HexToHash("0x441")
	for _, block := range []uint64{2, 5} {
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		receipt := makeTestReceipt(txHash, block, 0, addr, []common.Hash{topic})
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
			{TxHash: txHash, Receipt: receipt},
		}))
		if block == 2 {
			require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(4), nil))
		}
	}
	require.NoError(t, store.Close())

	store, err = NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, extractParquetV2Store(t, store).SetMaxBlocksPerFile(4))

	logs, err := store.FilterLogs(ctx, 5, 5, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(5), logs[0].BlockNumber)
}
