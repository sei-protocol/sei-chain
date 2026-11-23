package rpc

import (
	"math/big"
	"testing"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

func TestClassifyBlockNumber(t *testing.T) {
	for _, tc := range []struct {
		name   string
		number ethrpc.BlockNumber
		want   blockNumberKind
	}{
		{name: "latest", number: ethrpc.LatestBlockNumber, want: headBlock},
		{name: "safe", number: ethrpc.SafeBlockNumber, want: headBlock},
		{name: "finalized", number: ethrpc.FinalizedBlockNumber, want: headBlock},
		{name: "pending", number: ethrpc.PendingBlockNumber, want: headBlock},
		{name: "earliest", number: ethrpc.EarliestBlockNumber, want: earliestBlock},
		{name: "0x0", number: 0, want: earliestBlock},
		{name: "height", number: 7, want: heightBlock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyBlockNumber(tc.number)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("unknown tag", func(t *testing.T) {
		_, err := classifyBlockNumber(-6)
		var invalid *invalidParamsError
		require.ErrorAs(t, err, &invalid)
		require.ErrorIs(t, err, errBlockTagUnsupported)
	})
}

func TestClassifyFilterBound(t *testing.T) {
	got, err := classifyFilterBound(nil)
	require.NoError(t, err)
	require.Equal(t, headBlock, got)

	got, err = classifyFilterBound(big.NewInt(ethrpc.EarliestBlockNumber.Int64()))
	require.NoError(t, err)
	require.Equal(t, earliestBlock, got)

	var invalid *invalidParamsError
	_, err = classifyFilterBound(new(big.Int).Lsh(big.NewInt(1), 70))
	require.ErrorAs(t, err, &invalid)
	_, err = classifyFilterBound(big.NewInt(-6))
	require.ErrorAs(t, err, &invalid)
}

func TestEarliestRetainedHeight(t *testing.T) {
	require.Equal(t, int64(1), earliestRetainedHeight(&testBackend{}, nil), "the first block of a chain whose genesis names none")
	require.Equal(t, int64(4), earliestRetainedHeight(&testBackend{initialHeight: 4}, nil), "a chain starting later")
	require.Equal(t, int64(4), earliestRetainedHeight(&testBackend{initialHeight: 4}, &floorReceiptStore{floors: []int64{2}}), "a floor below the first block")
	require.Equal(t, int64(6), earliestRetainedHeight(&testBackend{initialHeight: 4}, &floorReceiptStore{floors: []int64{6}}), "a pruned store")
}
