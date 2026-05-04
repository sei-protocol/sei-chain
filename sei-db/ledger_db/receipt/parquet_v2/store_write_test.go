package parquet_v2

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestWriteReceiptsUpdatesLatestAndReopens(t *testing.T) {
	dir := t.TempDir()

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:          dir,
		MaxBlocksPerFile:     500,
		BlockFlushInterval:   100,
		PruneIntervalSeconds: 0,
	}, ReplayHooks{})
	require.NoError(t, err)

	for block := uint64(1); block <= 3; block++ {
		require.NoError(t, store.WriteReceipts(block, []parquet.ReceiptInput{
			testReceiptInput(block, common.BigToHash(new(big.Int).SetUint64(block))),
		}))
	}
	require.Equal(t, int64(3), store.LatestVersion())
	require.NoError(t, store.Close())

	reopened, err := NewStore(parquet.StoreConfig{
		DBDirectory:          dir,
		MaxBlocksPerFile:     500,
		PruneIntervalSeconds: 0,
	}, ReplayHooks{})
	require.NoError(t, err)
	require.Equal(t, int64(3), reopened.LatestVersion())
	require.Equal(t, uint64(4), reopened.FileStartBlock())
	require.NoError(t, reopened.Close())
}
