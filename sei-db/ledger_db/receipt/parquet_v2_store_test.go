package receipt

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2"
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
