package receipt

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/stretchr/testify/require"
)

// littIdxCfg returns the config the offline entry points expect for a littidx store at dir.
func littIdxCfg(dir string) dbconfig.ReceiptStoreConfig {
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = receiptBackendLittIdx
	cfg.DBDirectory = dir
	return cfg
}

// writeLittIdxReceipts builds a littidx store at dir, writes one receipt for each block in [1, blocks],
// and closes it, leaving well-formed version metadata behind in the index.
func writeLittIdxReceipts(t *testing.T, dir string, blocks uint64) {
	t.Helper()
	ctx, storeKey := newTestContext()
	store, err := NewReceiptStore(littIdxCfg(dir), storeKey)
	require.NoError(t, err)

	addr := common.HexToAddress("0xc0de")
	topic := common.HexToHash("0xdead")
	for block := uint64(1); block <= blocks; block++ {
		txHash := common.BytesToHash([]byte{byte(block)})
		record := ReceiptRecord{TxHash: txHash, Receipt: makeTestReceipt(txHash, block, 0, addr,
			[]common.Hash{topic})}
		//nolint:gosec // small test heights
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{record}))
	}
	require.NoError(t, store.Close())
}

// overwriteMeta replaces a version-metadata value in the store's index, standing in for metadata this
// build cannot read: a corrupt value, or one written by a format it does not know.
func overwriteMeta(t *testing.T, dir string, key []byte, value []byte) {
	t.Helper()
	indexCfg := pebbledb.DefaultConfig()
	indexCfg.DataDir = filepath.Join(dir, littIndexDirName)
	index, err := pebbledb.Open(context.Background(), &indexCfg)
	require.NoError(t, err)
	require.NoError(t, index.Set(key, value, dbtypes.WriteOptions{}))
	require.NoError(t, index.Close())
}

// TestPruneAfterRejectsUnreadableLatest verifies that a head PruneAfter cannot read stops the rollback.
// Read as zero it would take the nothing-to-do path, returning nil and reporting success for a rollback
// that never happened.
func TestPruneAfterRejectsUnreadableLatest(t *testing.T) {
	dir := t.TempDir()
	writeLittIdxReceipts(t, dir, 5)
	overwriteMeta(t, dir, receiptLatestVersionKey, []byte{0x01, 0x02, 0x03})

	require.ErrorContains(t, PruneAfter(littIdxCfg(dir), 2), "holds 3 bytes")
}

// TestPruneAfterRejectsUnreadableEarliest verifies the same for the retention floor. Read as zero it
// would silently disable the below-the-floor refusal and let the rollback proceed.
func TestPruneAfterRejectsUnreadableEarliest(t *testing.T) {
	dir := t.TempDir()
	writeLittIdxReceipts(t, dir, 5)
	overwriteMeta(t, dir, receiptEarliestVersionKey, []byte{0x01, 0x02, 0x03})

	cfg := littIdxCfg(dir)
	require.ErrorContains(t, PruneAfter(cfg, 2), "holds 3 bytes")

	// The refusal has to land before any receipt is touched, which is what the surviving range shows.
	ok, lowestBlock, highestBlock, err := GetRange(cfg)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), lowestBlock)
	require.Equal(t, uint64(5), highestBlock)
}

// TestPruneAfterAcceptsAbsentMetadata verifies that an absent key stays benign: a store that has never
// recorded a version has nothing to roll back, which is not an error.
func TestPruneAfterAcceptsAbsentMetadata(t *testing.T) {
	dir := t.TempDir()
	openBackend(t, dir, receiptBackendLittIdx)

	require.NoError(t, PruneAfter(littIdxCfg(dir), 100))
}
