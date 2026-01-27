package receipt_test

import (
	"os"
	"testing"
	"time"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func setupReceiptStore(t *testing.T) (receipt.ReceiptStore, sdk.Context, storetypes.StoreKey) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(0)
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = t.TempDir()
	cfg.KeepRecent = 0
	store, err := receipt.NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store, ctx, storeKey
}

func makeReceipt(txHash common.Hash, addr common.Address, topics []common.Hash, logIndex uint32) *types.Receipt {
	topicHex := make([]string, 0, len(topics))
	for _, topic := range topics {
		topicHex = append(topicHex, topic.Hex())
	}
	return &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      1,
		TransactionIndex: logIndex,
		Logs: []*types.Log{
			{
				Address: addr.Hex(),
				Topics:  topicHex,
				Data:    []byte{0x1},
				Index:   logIndex,
			},
		},
	}
}

func withBloom(r *types.Receipt) *types.Receipt {
	logs := receipt.GetLogsForTx(r, 0)
	bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: logs})
	r.LogsBloom = bloom.Bytes()
	return r
}

func TestNewReceiptStoreConfigErrors(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey("evm")
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = ""
	store, err := receipt.NewReceiptStore(nil, cfg, storeKey)
	require.Error(t, err)
	require.Nil(t, store)

	cfg.DBDirectory = t.TempDir()
	cfg.Backend = "rocksdb"
	store, err = receipt.NewReceiptStore(nil, cfg, storeKey)
	require.Error(t, err)
	require.Nil(t, store)

	cfg.Backend = "pebble"
	store, err = receipt.NewReceiptStore(nil, cfg, storeKey)
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NoError(t, store.Close())

	cfg.Backend = "parquet"
	store, err = receipt.NewReceiptStore(nil, cfg, storeKey)
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NoError(t, store.Close())
}

func TestReceiptStoreNilReceiver(t *testing.T) {
	store := receipt.NilReceiptStore()
	require.Equal(t, int64(0), store.LatestVersion())
	require.Error(t, store.SetLatestVersion(1))
	require.Error(t, store.SetEarliestVersion(1))
	_, err := store.GetReceipt(sdk.Context{}, common.Hash{})
	require.Error(t, err)
	_, err = store.GetReceiptFromStore(sdk.Context{}, common.Hash{})
	require.Error(t, err)
	require.Error(t, store.SetReceipts(sdk.Context{}, nil))
	_, err = store.FilterLogs(sdk.Context{}, 1, common.Hash{}, nil, filters.FilterCriteria{}, false)
	require.Error(t, err)
	require.NoError(t, store.Close())
}

func TestSetReceiptsAndGet(t *testing.T) {
	store, ctx, _ := setupReceiptStore(t)
	txHash := common.HexToHash("0x1")
	addr := common.HexToAddress("0x1")
	topic := common.HexToHash("0x2")
	r := makeReceipt(txHash, addr, []common.Hash{topic}, 0)

	err := store.SetReceipts(ctx.WithBlockHeight(0), []receipt.ReceiptRecord{
		{TxHash: txHash, Receipt: r},
		{TxHash: txHash},
	})
	require.NoError(t, err)

	got, err := store.GetReceipt(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, r.TxHashHex, got.TxHashHex)

	got, err = store.GetReceiptFromStore(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, r.TxHashHex, got.TxHashHex)

	_, err = store.GetReceiptFromStore(ctx, common.HexToHash("0x3"))
	require.Error(t, err)

	require.GreaterOrEqual(t, store.LatestVersion(), int64(1))
	require.NoError(t, store.SetLatestVersion(10))
	require.Equal(t, int64(10), store.LatestVersion())
	require.NoError(t, store.SetEarliestVersion(1))
}

func TestReceiptStoreLegacyFallback(t *testing.T) {
	store, ctx, storeKey := setupReceiptStore(t)
	txHash := common.HexToHash("0x4")
	r := makeReceipt(txHash, common.HexToAddress("0x2"), []common.Hash{common.HexToHash("0x5")}, 0)
	bz, err := r.Marshal()
	require.NoError(t, err)

	ctx.KVStore(storeKey).Set(types.ReceiptKey(txHash), bz)

	got, err := store.GetReceipt(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, r.TxHashHex, got.TxHashHex)

	_, err = store.GetReceiptFromStore(ctx, txHash)
	require.Error(t, err)
}

func TestSetReceiptsAsync(t *testing.T) {
	store, ctx, _ := setupReceiptStore(t)
	txHash := common.HexToHash("0x6")
	r := makeReceipt(txHash, common.HexToAddress("0x3"), []common.Hash{common.HexToHash("0x7")}, 0)
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(2), []receipt.ReceiptRecord{{TxHash: txHash, Receipt: r}}))

	require.Eventually(t, func() bool {
		_, err := store.GetReceipt(ctx, txHash)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
}

func TestReceiptStorePebbleBackendBasic(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(0)
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = t.TempDir()
	cfg.KeepRecent = 0
	cfg.Backend = "pebble"

	store, err := receipt.NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	txHash := common.HexToHash("0x55")
	addr := common.HexToAddress("0x500")
	topic := common.HexToHash("0x501")
	r := makeReceipt(txHash, addr, []common.Hash{topic}, 0)

	require.NoError(t, store.SetReceipts(ctx, []receipt.ReceiptRecord{{TxHash: txHash, Receipt: r}}))

	got, err := store.GetReceipt(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, r.TxHashHex, got.TxHashHex)

	blockHash := common.HexToHash("0xf00")
	logs, err := store.FilterLogs(ctx, 1, blockHash, []common.Hash{txHash}, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	}, true)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, blockHash, logs[0].BlockHash)
}

func TestFilterLogs(t *testing.T) {
	store, ctx, _ := setupReceiptStore(t)
	blockHeight := int64(8)
	blockHash := common.HexToHash("0xab")

	txHash1 := common.HexToHash("0x10")
	txHash2 := common.HexToHash("0x11")
	txHash3 := common.HexToHash("0x12")

	addr1 := common.HexToAddress("0x100")
	addr2 := common.HexToAddress("0x200")
	addr3 := common.HexToAddress("0x300")

	topic1 := common.HexToHash("0xaa")
	topic2 := common.HexToHash("0xbb")
	topic3 := common.HexToHash("0xcc")

	r1 := withBloom(makeReceipt(txHash1, addr1, []common.Hash{topic1}, 0))
	r2 := makeReceipt(txHash2, addr2, []common.Hash{topic2}, 1)
	r3 := withBloom(makeReceipt(txHash3, addr3, []common.Hash{topic3}, 2))

	err := store.SetReceipts(ctx.WithBlockHeight(0), []receipt.ReceiptRecord{
		{TxHash: txHash1, Receipt: r1},
		{TxHash: txHash2, Receipt: r2},
		{TxHash: txHash3, Receipt: r3},
	})
	require.NoError(t, err)

	logs, err := store.FilterLogs(ctx, blockHeight, blockHash, nil, filters.FilterCriteria{}, false)
	require.NoError(t, err)
	require.Len(t, logs, 0)

	crit := filters.FilterCriteria{
		Addresses: []common.Address{addr1},
		Topics:    [][]common.Hash{{topic1}},
	}
	txHashes := []common.Hash{txHash1, txHash2, txHash3, common.HexToHash("0xdead")}

	logs, err = store.FilterLogs(ctx, blockHeight, blockHash, txHashes, crit, true)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, addr1, logs[0].Address)
	require.Equal(t, uint64(blockHeight), logs[0].BlockNumber)
	require.Equal(t, blockHash, logs[0].BlockHash)
	require.Equal(t, uint(0), logs[0].TxIndex)

	logs, err = store.FilterLogs(ctx, blockHeight, blockHash, txHashes, crit, false)
	require.NoError(t, err)
	require.Len(t, logs, 2)
	require.Equal(t, addr1, logs[0].Address)
	require.Equal(t, addr2, logs[1].Address)

	logs, err = store.FilterLogs(ctx, blockHeight, blockHash, txHashes[:3], filters.FilterCriteria{}, false)
	require.NoError(t, err)
	require.Len(t, logs, 3)
}

func TestMatchTopics(t *testing.T) {
	topic1 := common.HexToHash("0x1")
	topic2 := common.HexToHash("0x2")
	require.True(t, receipt.MatchTopics([][]common.Hash{{topic1}, {}}, []common.Hash{topic1}))
	require.False(t, receipt.MatchTopics([][]common.Hash{{topic1}, {topic2}}, []common.Hash{topic1}))
}

func TestRecoverReceiptStoreReplaysChangelog(t *testing.T) {
	dir := t.TempDir()
	changelogPath := dbutils.GetChangelogPath(dir)
	require.NoError(t, os.MkdirAll(changelogPath, 0o750))

	stream, err := wal.NewChangelogWAL(dbLogger.NewNopLogger(), changelogPath, wal.Config{})
	require.NoError(t, err)

	txHash1 := common.HexToHash("0x20")
	txHash2 := common.HexToHash("0x21")
	receipt1 := makeReceipt(txHash1, common.HexToAddress("0x400"), []common.Hash{common.HexToHash("0x22")}, 0)
	receipt2 := makeReceipt(txHash2, common.HexToAddress("0x401"), []common.Hash{common.HexToHash("0x23")}, 0)

	entry1, err := makeChangeSetEntry(1, txHash1, receipt1)
	require.NoError(t, err)
	entry2, err := makeChangeSetEntry(2, txHash2, receipt2)
	require.NoError(t, err)

	require.NoError(t, stream.Write(entry1))
	require.NoError(t, stream.Write(entry2))
	require.NoError(t, stream.Close())

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = dir
	cfg.KeepRecent = 0
	ssConfig := dbconfig.DefaultStateStoreConfig()
	ssConfig.DBDirectory = cfg.DBDirectory
	ssConfig.KeepRecent = cfg.KeepRecent
	if cfg.PruneIntervalSeconds > 0 {
		ssConfig.PruneIntervalSeconds = cfg.PruneIntervalSeconds
	}
	ssConfig.KeepLastVersion = false
	ssConfig.Backend = "pebbledb"
	db, err := mvcc.OpenDB(dir, ssConfig)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.SetLatestVersion(1))
	require.NoError(t, receipt.RecoverReceiptStore(dbLogger.NewNopLogger(), changelogPath, db))
	require.Equal(t, int64(2), db.GetLatestVersion())

	bz, err := db.Get(types.ReceiptStoreKey, db.GetLatestVersion(), types.ReceiptKey(txHash2))
	require.NoError(t, err)
	require.NotNil(t, bz)
}

func makeChangeSetEntry(version int64, txHash common.Hash, receipt *types.Receipt) (proto.ChangelogEntry, error) {
	marshalledReceipt, err := receipt.Marshal()
	if err != nil {
		return proto.ChangelogEntry{}, err
	}
	kvPair := &iavl.KVPair{
		Key:   types.ReceiptKey(txHash),
		Value: marshalledReceipt,
	}
	ncs := &proto.NamedChangeSet{
		Name:      types.ReceiptStoreKey,
		Changeset: iavl.ChangeSet{Pairs: []*iavl.KVPair{kvPair}},
	}
	return proto.ChangelogEntry{
		Version:    version,
		Changesets: []*proto.NamedChangeSet{ncs},
	}, nil
}
