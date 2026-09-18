package rpc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func testInfoBackend(gasLimit uint64, minGasPrice int64) *testBackend {
	return &testBackend{
		gasLimit:    func() (uint64, error) { return gasLimit, nil },
		minGasPrice: func() (*big.Int, error) { return big.NewInt(minGasPrice), nil },
	}
}

// setBlockReceipt writes one block with a single reward-eligible tx, so its stored BlockStats has
// GasUsed=gasUsed and every default percentile equal to reward.
func setBlockReceipt(t *testing.T, store receipt.ReceiptStore, blockNumber, gasUsed uint64, reward int64) {
	t.Helper()
	txHash := [32]byte{byte(blockNumber)}
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash:  txHash,
		Receipt: &evmtypes.Receipt{TxHashHex: "0x", BlockNumber: blockNumber, GasUsed: gasUsed},
		Reward:  big.NewInt(reward),
	}}))
}

func TestGasPriceScalesFloorUp(t *testing.T) {
	api := &infoAPI{backend: testInfoBackend(0, 1_000_000_000)}
	price, err := api.GasPrice(t.Context())
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1_100_000_000), price.ToInt())
}

func TestFeeHistoryEmptyBlockCountReturnsEmptyResult(t *testing.T) {
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: evmonly.NewMemoryReceiptStore()}
	result, err := api.FeeHistory(t.Context(), 0, ethrpc.LatestBlockNumber, nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(0), result.OldestBlock.ToInt())
	require.Empty(t, result.GasUsedRatio)
	require.Nil(t, result.Reward)
}

func TestFeeHistoryValidatesRewardPercentiles(t *testing.T) {
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: evmonly.NewMemoryReceiptStore()}
	_, err := api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{50, 25})
	require.ErrorContains(t, err, "ascending")
	_, err = api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{-1})
	require.ErrorContains(t, err, "ascending")
	_, err = api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{101})
	require.ErrorContains(t, err, "ascending")
}

func TestFeeHistoryRejectsAnUncommittedExplicitHeight(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 1, 10, 100)
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: store}
	_, err := api.FeeHistory(t.Context(), 1, ethrpc.BlockNumber(5), nil)
	require.ErrorContains(t, err, "not yet available")
}

func TestFeeHistoryUsesStoredGasUsedRatio(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 1, 10, 100)
	setBlockReceipt(t, store, 2, 20, 100)
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: store}

	result, err := api.FeeHistory(t.Context(), 2, ethrpc.LatestBlockNumber, nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1), result.OldestBlock.ToInt())
	require.Equal(t, []float64{0.01, 0.02}, result.GasUsedRatio)
	// baseFeePerGas is always zero for giga, one entry more than gasUsedRatio.
	require.Len(t, result.BaseFee, 3)
	for _, bf := range result.BaseFee {
		require.Equal(t, big.NewInt(0), bf.ToInt())
	}
	require.Nil(t, result.Reward)
}

func TestFeeHistorySkipsHeightsWithoutStats(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 3, 10, 100) // blocks 1-2 never written
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: store}

	result, err := api.FeeHistory(t.Context(), 3, ethrpc.BlockNumber(3), nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(3), result.OldestBlock.ToInt(), "the earliest height with stats, not the earliest requested")
	require.Equal(t, []float64{0.01}, result.GasUsedRatio)
}

func TestFeeHistoryRewardFromStoredPercentiles(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	// A single reward-eligible tx: every default percentile (0,10,25,50,75,90,100) equals its
	// one reward value.
	setBlockReceipt(t, store, 1, 10, 100)
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: store}

	result, err := api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{0, 50, 100})
	require.NoError(t, err)
	require.Len(t, result.Reward, 1)
	require.Equal(t, []*big.Int{big.NewInt(100), big.NewInt(100), big.NewInt(100)}, toBigInts(result.Reward[0]))
}

func TestFeeHistoryFallsBackToFixedRewardForAnUnstoredPercentile(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 1, 10, 999) // reward value is irrelevant to the fallback row
	api := &infoAPI{backend: testInfoBackend(1000, 1_000_000_000), store: store}

	// 33 is not in receipt.DefaultRewardPercentiles, so the whole row falls back to the fixed
	// suggested price rather than mixing a real value with a guess.
	result, err := api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{33})
	require.NoError(t, err)
	require.Len(t, result.Reward, 1)
	require.Equal(t, []*big.Int{big.NewInt(1_100_000_000)}, toBigInts(result.Reward[0]))
}

func toBigInts(row []*hexutil.Big) []*big.Int {
	out := make([]*big.Int, len(row))
	for i, v := range row {
		out[i] = v.ToInt()
	}
	return out
}
