package gigareceipt

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/stretchr/testify/require"
)

var (
	crashAddr  = common.HexToAddress("0xc0de")
	crashTopic = common.HexToHash("0xdead")
)

// crashRecord is the record writeLittIdxReceipts writes for block.
func crashRecord(block uint64) receipt.ReceiptRecord {
	txHash := common.BytesToHash([]byte{byte(block)})
	return receipt.ReceiptRecord{TxHash: txHash, Receipt: makeTestReceipt(txHash, block, 0, crashAddr,
		[]common.Hash{crashTopic})}
}

// withIndex runs edit against the closed store's index at dir.
func withIndex(t *testing.T, dir string, edit func(index dbtypes.KeyValueDB)) {
	t.Helper()
	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = filepath.Join(dir, littIndexDirName)
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	require.NoError(t, err)
	edit(index)
	require.NoError(t, index.Close())
}

// openStore opens the store at dir, closing it when the test ends.
func openStore(t *testing.T, dir string) *littReceiptStore {
	t.Helper()
	_, storeKey := newTestContext()
	store, err := NewReceiptStore(littIdxCfg(dir), storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store.(*littReceiptStore)
}

func requireStoredRange(t *testing.T, dir string, lowest, highest uint64) {
	t.Helper()
	ok, gotLowest, gotHighest, err := GetRange(littIdxCfg(dir))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, lowest, gotLowest)
	require.Equal(t, highest, gotHighest)
}

// replayAndRequireEachBlockOnce writes blocks [from, through], then checks that every block in
// [1, through] has block stats and is returned exactly once by GetReceipt, FilterLogs and IterateReceipts.
func replayAndRequireEachBlockOnce(t *testing.T, s *littReceiptStore, from, through uint64) {
	t.Helper()
	ctx, _ := newTestContext()
	for block := from; block <= through; block++ {
		//nolint:gosec // small test heights
		require.NoError(t, s.SetReceipts(ctx.WithBlockHeight(int64(block)),
			[]receipt.ReceiptRecord{crashRecord(block)}))
	}
	requireReceiptVersion(t, s, int64(through)) //nolint:gosec // small test heights

	var want []uint64
	for block := uint64(1); block <= through; block++ {
		want = append(want, block)
		got, err := s.GetReceipt(ctx, crashRecord(block).TxHash)
		require.NoErrorf(t, err, "block %d", block)
		require.Equal(t, block, got.BlockNumber)
		_, err = s.GetBlockStats(ctx, block)
		require.NoErrorf(t, err, "block %d's stats", block)
	}

	logs, err := s.FilterLogs(ctx, 1, through, filters.FilterCriteria{Addresses: []common.Address{crashAddr}}, nil)
	require.NoError(t, err)
	logBlocks := make([]uint64, 0, len(logs))
	for _, lg := range logs {
		logBlocks = append(logBlocks, lg.BlockNumber)
	}
	require.Equal(t, want, logBlocks)

	it, err := s.IterateReceipts(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()
	var walked []uint64
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		walked = append(walked, it.BlockNumber())
	}
	require.Equal(t, want, walked, "a walk must visit every block once, in order")
}

// When the index lost commits that litt kept, recovery deletes those receipts from litt. Otherwise
// replaying the blocks would write each one a second time.
func TestRecoveryRollsLittBackToTheIndexHead(t *testing.T) {
	dir := t.TempDir()
	writeLittIdxReceipts(t, dir, 6)
	require.NoError(t, LoseIndexAbove(dir, 4))

	head, err := GetLatestBlock(littIdxCfg(dir))
	require.NoError(t, err)
	require.Equal(t, uint64(4), head)

	require.NoError(t, PruneAfter(littIdxCfg(dir), head))
	requireStoredRange(t, dir, 1, 4)

	s := openStore(t, dir)
	require.Equal(t, int64(4), s.LatestVersion())
	replayAndRequireEachBlockOnce(t, s, 5, 6)
}

// When litt lost writes that the index kept, recovery rolls the index back to the litt head. Otherwise
// those blocks would never be replayed.
func TestRecoveryRollsTheIndexBackToTheLittHead(t *testing.T) {
	dir := t.TempDir()
	writeLittIdxReceipts(t, dir, 6)
	require.NoError(t, LoseLittAbove(dir, 4))

	head, err := GetLatestBlock(littIdxCfg(dir))
	require.NoError(t, err)
	require.Equal(t, uint64(4), head, "the head is the lower of the litt and index heads")

	require.NoError(t, PruneAfter(littIdxCfg(dir), head))
	s := openStore(t, dir)
	require.Equal(t, int64(4), s.LatestVersion())
	replayAndRequireEachBlockOnce(t, s, 5, 6)
}

// PruneAfter rolls the index back to the litt head even when highestBlockToKeep is above both heads.
func TestPruneAfterAboveBothHeadsRollsTheIndexBack(t *testing.T) {
	dir := t.TempDir()
	writeLittIdxReceipts(t, dir, 6)
	require.NoError(t, LoseLittAbove(dir, 4))

	require.NoError(t, PruneAfter(littIdxCfg(dir), 100))
	require.Equal(t, int64(4), openStore(t, dir).LatestVersion())
}

// With no index head, PruneAfter still deletes the litt receipts above highestBlockToKeep, which a replay
// would otherwise write a second time.
func TestPruneAfterRollsLittBackWhenTheIndexHasNoHead(t *testing.T) {
	dir := t.TempDir()
	writeLittIdxReceipts(t, dir, 6)
	withIndex(t, dir, func(index dbtypes.KeyValueDB) {
		require.NoError(t, rollBackIndex(index, 0))
		require.NoError(t, index.Delete(receiptLatestVersionKey, dbtypes.WriteOptions{}))
	})

	head, err := GetLatestBlock(littIdxCfg(dir))
	require.NoError(t, err)
	require.Zero(t, head)

	require.NoError(t, PruneAfter(littIdxCfg(dir), 3))
	requireStoredRange(t, dir, 1, 3)
}

// A rollback cannot fix an index head with no receipts in litt, so PruneAfter returns an error instead
// of letting the store open with an index that lists missing receipts.
func TestPruneAfterRefusesAnIndexHeadWithEmptyLitt(t *testing.T) {
	dir := t.TempDir()
	writeLittIdxReceipts(t, dir, 3)
	require.NoError(t, LoseLittAbove(dir, 0))

	require.ErrorContains(t, PruneAfter(littIdxCfg(dir), 3), "no receipts are present on disk")
}

// A write to a block above the head that already has a part in litt is refused, leaving the head and
// litt unchanged.
func TestWriteRefusesAnOrphanedPartAboveTheHead(t *testing.T) {
	cfg := littIdxCfg(t.TempDir())
	cfg.AsyncWriteBuffer = 0
	ctx, storeKey := newTestContext()
	store, err := NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	s := store.(*littReceiptStore)

	require.NoError(t, s.SetReceipts(ctx.WithBlockHeight(1), []receipt.ReceiptRecord{crashRecord(1)}))
	require.NoError(t, s.receipts.Put(littPartKey(2, 0), []byte{}))

	err = s.SetReceipts(ctx.WithBlockHeight(2), []receipt.ReceiptRecord{crashRecord(2)})
	require.ErrorContains(t, err, "above the store's head 1")
	require.Equal(t, int64(1), s.LatestVersion())

	exists, err := s.receipts.Exists(littPartKey(2, 1))
	require.NoError(t, err)
	require.False(t, exists, "a refused write must not append a second part")
}
