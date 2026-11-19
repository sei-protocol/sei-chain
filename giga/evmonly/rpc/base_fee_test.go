package rpc

import (
	"errors"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
)

// A block's base fee is the one its stats recorded; without one, the latest committed block's,
// or the next block's past it.
func TestBlockBaseFee(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockBaseFee(t, store, 1, 7)
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()).WithBlockHeight(2), nil))
	backend := &testBackend{
		blockNumber: func() uint64 { return 2 },
		baseFee:     func() *big.Int { return big.NewInt(8) },
		nextBaseFee: func() *big.Int { return big.NewInt(9) },
	}

	for height, want := range map[int64]int64{1: 7, 2: 8, 3: 9} {
		got, err := blockBaseFee(t.Context(), backend, store, height)
		require.NoError(t, err)
		require.Equal(t, big.NewInt(want), got, "block %d", height)
	}
}

type failingBlockStatsStore struct {
	receipt.ReceiptStore
}

func (failingBlockStatsStore) GetBlockStats(sdk.Context, uint64) (receipt.BlockStats, error) {
	return receipt.BlockStats{}, errors.New("disk failed")
}

func TestBlockBaseFeeReturnsAStoreError(t *testing.T) {
	_, err := blockBaseFee(t.Context(), &testBackend{}, failingBlockStatsStore{}, 1)

	require.ErrorContains(t, err, "disk failed")
}
