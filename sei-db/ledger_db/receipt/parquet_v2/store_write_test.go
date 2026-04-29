package parquet_v2

import (
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
	})
	require.NoError(t, err)

	require.NoError(t, store.WriteReceipts([]parquet.ReceiptInput{
		testReceiptInput(1, common.HexToHash("0x1")),
		testReceiptInput(2, common.HexToHash("0x2")),
		testReceiptInput(3, common.HexToHash("0x3")),
	}))
	require.Equal(t, int64(3), store.LatestVersion())
	require.NoError(t, store.Close())

	reopened, err := NewStore(parquet.StoreConfig{
		DBDirectory:          dir,
		MaxBlocksPerFile:     500,
		PruneIntervalSeconds: 0,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), reopened.LatestVersion())
	require.Equal(t, uint64(4), reopened.FileStartBlock())
	require.NoError(t, reopened.Close())
}
