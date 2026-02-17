package receipt_test

import (
	"os"
	"testing"
	"time"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/cosmos/iavl"
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

	// Pebble backend does not support range queries
	_, err = store.FilterLogs(ctx, 1, 1, filters.FilterCriteria{
		Addresses: []common.Address{addr},
		Topics:    [][]common.Hash{{topic}},
	})
	require.ErrorIs(t, err, receipt.ErrRangeQueryNotSupported)
}

func TestFilterLogsRangeQueryNotSupported(t *testing.T) {
	store, ctx, _ := setupReceiptStore(t)
	// Pebble backend does not support range queries, so FilterLogs returns ErrRangeQueryNotSupported.
	_, err := store.FilterLogs(ctx, 1, 10, filters.FilterCriteria{})
	require.ErrorIs(t, err, receipt.ErrRangeQueryNotSupported)
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
