package parquet_v2

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func TestReplayWALRequiresConverter(t *testing.T) {
	store, err := NewStore(parquet.StoreConfig{DBDirectory: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	_, err = store.ReplayWAL(nil)
	require.ErrorContains(t, err, "converter is nil")
}

func TestReplayWALPublicDispatch(t *testing.T) {
	store := newDispatchStore(t)
	_, err := store.ReplayWAL(func(blockNumber uint64, receiptBytes []byte, logStartIndex uint) (ReplayReceipt, error) {
		return replayConverterForTest(blockNumber, receiptBytes, logStartIndex)
	})
	require.NoError(t, err)

	result, err := store.GetReceiptByTxHash(context.Background(), common.HexToHash("0x1"))
	require.NoError(t, err)
	require.Nil(t, result)
}
