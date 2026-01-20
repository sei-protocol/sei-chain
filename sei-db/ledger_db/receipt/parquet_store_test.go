package receipt

import (
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func makeTestReceipt(txHash common.Hash, blockNumber uint64, txIndex uint32, addr common.Address, topics []common.Hash) *types.Receipt {
	topicHex := make([]string, 0, len(topics))
	for _, topic := range topics {
		topicHex = append(topicHex, topic.Hex())
	}

	return &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      blockNumber,
		TransactionIndex: txIndex,
		Logs: []*types.Log{
			{
				Address: addr.Hex(),
				Topics:  topicHex,
				Data:    []byte{0x1},
				Index:   0,
			},
		},
	}
}

func newTestContext() (sdk.Context, storetypes.StoreKey) {
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)
	return ctx, storeKey
}

func TestLedgerCacheReceiptsAndLogs(t *testing.T) {
	cache := newLedgerCache()
	txHash := common.HexToHash("0x1")
	blockNumber := uint64(10)

	cache.AddReceiptsBatch(blockNumber, []receiptCacheEntry{
		{
			TxHash:  txHash,
			Receipt: &types.Receipt{TxHashHex: txHash.Hex(), BlockNumber: blockNumber},
		},
	})

	got, ok := cache.GetReceipt(txHash)
	require.True(t, ok)
	require.Equal(t, txHash.Hex(), got.TxHashHex)

	addr := common.HexToAddress("0x100")
	topic := common.HexToHash("0xabc")
	cache.AddLogsForBlock(blockNumber, []*ethtypes.Log{
		{
			Address:     addr,
			Topics:      []common.Hash{topic},
			BlockNumber: blockNumber,
			TxHash:      txHash,
			TxIndex:     0,
			Index:       0,
		},
	})

	logs := cache.GetLogsWithFilter(blockNumber, blockNumber, []common.Address{addr}, [][]common.Hash{{topic}})
	require.Len(t, logs, 1)
	require.Equal(t, addr, logs[0].Address)
	require.Equal(t, topic, logs[0].Topics[0])
}

func TestLedgerCacheRotatePrunes(t *testing.T) {
	cache := newLedgerCache()
	txHash := common.HexToHash("0x2")
	blockNumber := uint64(1)
	cache.AddReceiptsBatch(blockNumber, []receiptCacheEntry{
		{
			TxHash:  txHash,
			Receipt: &types.Receipt{TxHashHex: txHash.Hex(), BlockNumber: blockNumber},
		},
	})

	_, ok := cache.GetReceipt(txHash)
	require.True(t, ok)

	cache.Rotate()
	cache.Rotate()

	_, ok = cache.GetReceipt(txHash)
	require.False(t, ok)
}

func TestParquetReceiptStoreCacheLogs(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	txHash := common.HexToHash("0x10")
	addr := common.HexToAddress("0x200")
	topic := common.HexToHash("0x1234")
	receipt := makeTestReceipt(txHash, 10, 2, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))

	blockHash := common.HexToHash("0xbeef")
	logs, err := store.FilterLogs(ctx, 10, blockHash, []common.Hash{txHash}, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	}, true)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, blockHash, logs[0].BlockHash)
	require.Equal(t, uint64(10), logs[0].BlockNumber)
	require.Equal(t, uint(2), logs[0].TxIndex)
}

func TestParquetReceiptStoreReopenQueries(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)

	txHash := common.HexToHash("0x20")
	addr := common.HexToAddress("0x300")
	topic := common.HexToHash("0x5678")
	receipt := makeTestReceipt(txHash, 5, 1, addr, []common.Hash{topic})

	require.NoError(t, store.SetReceipts(ctx, []ReceiptRecord{
		{TxHash: txHash, Receipt: receipt},
	}))
	require.NoError(t, store.Close())

	store, err = NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	got, err := store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, got.TxHashHex)

	blockHash := common.HexToHash("0xcafe")
	logs, err := store.FilterLogs(ctx, 5, blockHash, []common.Hash{txHash}, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	}, true)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, blockHash, logs[0].BlockHash)
	require.Equal(t, uint64(5), logs[0].BlockNumber)
}
