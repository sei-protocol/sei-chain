package receipt_test

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func setupLittIdx(t *testing.T, dir string) (receipt.ReceiptStore, sdk.Context) {
	t.Helper()
	return setupLittIdxPar(t, dir, dbconfig.DefaultReceiptLogFilterParallelism)
}

// setupLittIdxPar is setupLittIdx with an explicit getLogs parallelism degree.
func setupLittIdxPar(t *testing.T, dir string, parallelism int) (receipt.ReceiptStore, sdk.Context) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "littidx"
	cfg.DBDirectory = dir
	cfg.KeepRecent = 0
	cfg.LogFilterParallelism = parallelism
	store, err := receipt.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	return store, ctx
}

func litTxHash(block uint64, txIndex uint32) common.Hash {
	var h common.Hash
	copy(h[:], fmt.Sprintf("tx-%d-%d", block, txIndex))
	return h
}

// litReceipt builds a receipt with one log carrying the given address and topics.
func litReceipt(block uint64, txIndex uint32, addr common.Address, topics ...common.Hash) receipt.ReceiptRecord {
	topicHex := make([]string, 0, len(topics))
	for _, topic := range topics {
		topicHex = append(topicHex, topic.Hex())
	}
	txHash := litTxHash(block, txIndex)
	return receipt.ReceiptRecord{
		TxHash: txHash,
		Receipt: &types.Receipt{
			TxHashHex:        txHash.Hex(),
			BlockNumber:      block,
			TransactionIndex: txIndex,
			GasUsed:          21000,
			Logs:             []*types.Log{{Address: addr.Hex(), Topics: topicHex, Data: []byte{0xde, 0xad}, Index: 0}},
		},
	}
}

func writeLitBlock(t *testing.T, store receipt.ReceiptStore, ctx sdk.Context, block uint64, records ...receipt.ReceiptRecord) {
	t.Helper()
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), records)) //nolint:gosec // small test heights
}

func TestLittIdxReadWrite(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	require.Equal(t, "littidx", receipt.BackendTypeName(store))

	addr := common.HexToAddress("0xabcd")
	topic := common.HexToHash("0x1111")
	writeLitBlock(t, store, ctx, 1, litReceipt(1, 0, addr, topic), litReceipt(1, 1, addr, topic))
	writeLitBlock(t, store, ctx, 2, litReceipt(2, 0, addr, topic))

	for _, e := range []struct {
		block   uint64
		txIndex uint32
	}{{1, 0}, {1, 1}, {2, 0}} {
		rcpt, err := store.GetReceiptFromStore(ctx, litTxHash(e.block, e.txIndex))
		require.NoError(t, err)
		require.Equal(t, e.block, rcpt.BlockNumber)
		require.Equal(t, e.txIndex, rcpt.TransactionIndex)
	}

	_, err := store.GetReceiptFromStore(ctx, litTxHash(9, 9))
	require.ErrorIs(t, err, receipt.ErrNotFound)
	require.Equal(t, int64(2), store.LatestVersion())
}

func TestLittIdxFilterLogs(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	addr1 := common.HexToAddress("0x4444444444444444444444444444444444444444")
	addr2 := common.HexToAddress("0x5555555555555555555555555555555555555555")
	transfer := common.HexToHash("0x1111aa")
	approve := common.HexToHash("0x2222bb")
	alice := common.HexToHash("0x3333cc")
	bob := common.HexToHash("0x4444dd")

	writeLitBlock(t, store, ctx, 1, litReceipt(1, 0, addr1, transfer, alice), litReceipt(1, 1, addr1, transfer, bob))
	writeLitBlock(t, store, ctx, 2, litReceipt(2, 0, addr2, approve, alice))
	writeLitBlock(t, store, ctx, 3, litReceipt(3, 0, addr1, approve, bob))

	// Address OR: either address matches.
	logs, err := store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{Addresses: []common.Address{addr1, addr2}})
	require.NoError(t, err)
	require.Len(t, logs, 4)

	// AND across topic positions: transfer at 0 AND alice at 1.
	logs, err = store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{Topics: [][]common.Hash{{transfer}, {alice}}})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(1), logs[0].BlockNumber)
	require.Equal(t, uint(0), logs[0].TxIndex)

	// OR within a topic position: alice or bob at position 1.
	logs, err = store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{Topics: [][]common.Hash{{transfer}, {alice, bob}}})
	require.NoError(t, err)
	require.Len(t, logs, 2)

	// Wildcard position 0 (empty), bob at position 1.
	logs, err = store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{Topics: [][]common.Hash{{}, {bob}}})
	require.NoError(t, err)
	require.Len(t, logs, 2)

	// Range bound: only block 2.
	logs, err = store.FilterLogs(ctx, 2, 2, filters.FilterCriteria{})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(2), logs[0].BlockNumber)

	// No match.
	logs, err = store.FilterLogs(ctx, 1, 3, filters.FilterCriteria{Addresses: []common.Address{common.HexToAddress("0x9")}})
	require.NoError(t, err)
	require.Empty(t, logs)
}

// TestLittIdxMultiLogReceipt: a receipt with several logs is numbered with
// contiguous block-wide log indices.
func TestLittIdxMultiLogReceipt(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	addr := common.HexToAddress("0xbeef")
	topic := common.HexToHash("0xfeed")
	rec := receipt.ReceiptRecord{
		TxHash: litTxHash(7, 0),
		Receipt: &types.Receipt{
			TxHashHex: litTxHash(7, 0).Hex(), BlockNumber: 7, TransactionIndex: 0,
			Logs: []*types.Log{
				{Address: addr.Hex(), Topics: []string{topic.Hex()}, Index: 0},
				{Address: addr.Hex(), Topics: []string{topic.Hex()}, Index: 1},
				{Address: addr.Hex(), Topics: []string{topic.Hex()}, Index: 2},
			},
		},
	}
	writeLitBlock(t, store, ctx, 7, rec)

	logs, err := store.FilterLogs(ctx, 7, 7, filters.FilterCriteria{Addresses: []common.Address{addr}})
	require.NoError(t, err)
	require.Len(t, logs, 3)
	for i, lg := range logs {
		require.Equal(t, uint(i), lg.Index) //nolint:gosec // small test indices
	}
}

// TestLittIdxBlockWideLogIndex verifies firstLogIndex accumulates across
// transactions, so logs are numbered block-wide rather than per-transaction.
func TestLittIdxBlockWideLogIndex(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	addr := common.HexToAddress("0xabc")
	topic := common.HexToHash("0xdef")
	mk := func(txIndex uint32, nLogs int) receipt.ReceiptRecord {
		logs := make([]*types.Log, nLogs)
		for i := range logs {
			logs[i] = &types.Log{Address: addr.Hex(), Topics: []string{topic.Hex()}, Index: uint32(i)} //nolint:gosec // small test indices
		}
		txHash := litTxHash(8, txIndex)
		return receipt.ReceiptRecord{
			TxHash:  txHash,
			Receipt: &types.Receipt{TxHashHex: txHash.Hex(), BlockNumber: 8, TransactionIndex: txIndex, Logs: logs},
		}
	}
	// tx0: 2 logs, tx1: 1 log, tx2: 3 logs -> contiguous block-wide indices 0..5.
	writeLitBlock(t, store, ctx, 8, mk(0, 2), mk(1, 1), mk(2, 3))

	logs, err := store.FilterLogs(ctx, 8, 8, filters.FilterCriteria{Addresses: []common.Address{addr}})
	require.NoError(t, err)
	require.Len(t, logs, 6)
	for i, lg := range logs {
		require.Equal(t, uint(i), lg.Index, "log %d block-wide index", i) //nolint:gosec // small test indices
	}
	// Index ranges map back to the owning transaction in tx order.
	require.Equal(t, []uint{0, 0, 1, 2, 2, 2}, []uint{
		logs[0].TxIndex, logs[1].TxIndex, logs[2].TxIndex, logs[3].TxIndex, logs[4].TxIndex, logs[5].TxIndex,
	})
}

// TestLittIdxMultiPart writes one block across two SetReceipts calls (the
// legacy-migration shape that produces multiple litt parts) and confirms both
// parts' receipts and logs are visible.
func TestLittIdxMultiPart(t *testing.T) {
	store, ctx := setupLittIdx(t, t.TempDir())
	defer func() { _ = store.Close() }()

	addr := common.HexToAddress("0xabcd")
	topic := common.HexToHash("0x1111")
	// Two separate writes for block 4 -> part 0 then part 1.
	writeLitBlock(t, store, ctx, 4, litReceipt(4, 0, addr, topic))
	writeLitBlock(t, store, ctx, 4, litReceipt(4, 1, addr, topic))

	for _, txIndex := range []uint32{0, 1} {
		rcpt, err := store.GetReceiptFromStore(ctx, litTxHash(4, txIndex))
		require.NoError(t, err)
		require.Equal(t, uint64(4), rcpt.BlockNumber)
		require.Equal(t, txIndex, rcpt.TransactionIndex)
	}

	logs, err := store.FilterLogs(ctx, 4, 4, filters.FilterCriteria{Addresses: []common.Address{addr}})
	require.NoError(t, err)
	require.Len(t, logs, 2)
}

// TestLittIdxLegacyFallback covers GetReceipt falling back to the legacy KV
// store for a receipt that predates this backend (absent from litt).
func TestLittIdxLegacyFallback(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "littidx"
	cfg.DBDirectory = t.TempDir()
	store, err := receipt.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	txHash := litTxHash(99, 0)
	legacy := &types.Receipt{TxHashHex: txHash.Hex(), BlockNumber: 99, TransactionIndex: 0}
	bz, err := legacy.Marshal()
	require.NoError(t, err)
	ctx.KVStore(storeKey).Set(types.ReceiptKey(txHash), bz)

	// Not in litt, so the store-only read misses...
	_, err = store.GetReceiptFromStore(ctx, txHash)
	require.ErrorIs(t, err, receipt.ErrNotFound)

	// ...but GetReceipt falls back to the legacy KV store.
	rcpt, err := store.GetReceipt(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, uint64(99), rcpt.BlockNumber)
}

func TestLittIdxReopen(t *testing.T) {
	dir := t.TempDir()
	store, ctx := setupLittIdx(t, dir)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	for block := uint64(1); block <= 5; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}
	require.NoError(t, store.Close())

	store, ctx = setupLittIdx(t, dir)
	defer func() { _ = store.Close() }()
	require.Equal(t, int64(5), store.LatestVersion())

	rcpt, err := store.GetReceiptFromStore(ctx, litTxHash(3, 0))
	require.NoError(t, err)
	require.Equal(t, uint64(3), rcpt.BlockNumber)

	logs, err := store.FilterLogs(ctx, 1, 5, filters.FilterCriteria{Addresses: []common.Address{addr}})
	require.NoError(t, err)
	require.Len(t, logs, 5)
}

func TestLittIdxPrune(t *testing.T) {
	dir := t.TempDir()
	store, ctx := setupLittIdx(t, dir)
	defer func() { _ = store.Close() }()

	addr := common.HexToAddress("0x6666")
	topic := common.HexToHash("0xeeee")
	for block := uint64(1); block <= 10; block++ {
		writeLitBlock(t, store, ctx, block, litReceipt(block, 0, addr, topic))
	}

	require.NoError(t, receipt.PruneLittIdx(store, 6))
	require.Equal(t, int64(6), store.EarliestVersion())

	// Pruned blocks are invisible (tag entries deleted, read floor enforced).
	for block := uint64(1); block <= 5; block++ {
		_, err := store.GetReceiptFromStore(ctx, litTxHash(block, 0))
		require.ErrorIs(t, err, receipt.ErrNotFound, "block %d should be pruned", block)
	}
	logs, err := store.FilterLogs(ctx, 1, 5, filters.FilterCriteria{Addresses: []common.Address{addr}})
	require.NoError(t, err)
	require.Empty(t, logs)

	// Retained blocks unaffected.
	for block := uint64(6); block <= 10; block++ {
		rcpt, err := store.GetReceiptFromStore(ctx, litTxHash(block, 0))
		require.NoError(t, err)
		require.Equal(t, block, rcpt.BlockNumber)
	}
	logs, err = store.FilterLogs(ctx, 1, 10, filters.FilterCriteria{Addresses: []common.Address{addr}})
	require.NoError(t, err)
	require.Len(t, logs, 5)
}

// TestLittIdxFilterLogsParallelOrder verifies the parallel block fan-out returns
// logs in strict (block, txIndex) order and yields identical results whether run
// sequentially (parallelism 1) or in parallel.
func TestLittIdxFilterLogsParallelOrder(t *testing.T) {
	addr := common.HexToAddress("0xfeed")
	topic := common.HexToHash("0x2222")
	const blocks, txPerBlock = 25, 3

	want := make([]string, 0, blocks*txPerBlock)
	for b := uint64(1); b <= blocks; b++ {
		for tx := uint32(0); tx < txPerBlock; tx++ {
			want = append(want, fmt.Sprintf("%d-%d", b, tx))
		}
	}

	for _, parallelism := range []int{1, 8} {
		store, ctx := setupLittIdxPar(t, t.TempDir(), parallelism)
		for b := uint64(1); b <= blocks; b++ {
			recs := make([]receipt.ReceiptRecord, 0, txPerBlock)
			for tx := uint32(0); tx < txPerBlock; tx++ {
				recs = append(recs, litReceipt(b, tx, addr, topic))
			}
			writeLitBlock(t, store, ctx, b, recs...)
		}

		logs, err := store.FilterLogs(ctx, 1, blocks, filters.FilterCriteria{Addresses: []common.Address{addr}})
		require.NoError(t, err)
		got := make([]string, len(logs))
		for i, lg := range logs {
			got[i] = fmt.Sprintf("%d-%d", lg.BlockNumber, lg.TxIndex)
		}
		require.Equal(t, want, got, "parallelism=%d should return logs in (block, txIndex) order", parallelism)
		require.NoError(t, store.Close())
	}
}
