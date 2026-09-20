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
	// No latest block at all: congestionReward has nothing to escalate from, so GasPrice falls
	// back to the fixed margin over the admission floor.
	api := &infoAPI{backend: testInfoBackend(0, 1_000_000_000), store: evmonly.NewMemoryReceiptStore()}
	price, err := api.GasPrice(t.Context())
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1_100_000_000), price.ToInt())
}

func TestGasPriceEscalatesWithCongestion(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	// gasLimit 1000, TotalGasUsed 900 -> gasUsedRatio 0.9, in the >=0.8 tier -> p90.
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: [32]byte{1}, Receipt: &evmtypes.Receipt{TxHashHex: "0x1", BlockNumber: 1, GasUsed: 900}, Reward: big.NewInt(500)},
	}))
	api := &infoAPI{backend: testInfoBackend(1000, 1_000_000_000), store: store}

	price, err := api.GasPrice(t.Context())
	require.NoError(t, err)
	require.Equal(t, big.NewInt(500), price.ToInt())
}

func TestGasPriceFallsBackWhenTheTierPercentileIsntStored(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	// A congested block (ratio 0.9 -> p90 tier) with no reward-eligible tx, so no percentile was
	// ever computed for it: GasPrice must fall back rather than guess at a different percentile.
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: [32]byte{1}, Receipt: &evmtypes.Receipt{TxHashHex: "0x1", BlockNumber: 1, GasUsed: 900}},
	}))
	api := &infoAPI{backend: testInfoBackend(1000, 1_000_000_000), store: store}

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

// TestFeeHistoryEarliestRespectsThePruneFloor guards a real review finding: "earliest" resolved
// to a hardcoded height 1 regardless of retention, so a range ending at "earliest" on a node that
// had pruned its early history would error instead of resolving to the oldest block still held.
func TestFeeHistoryEarliestRespectsThePruneFloor(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	for h := uint64(1); h <= 5; h++ {
		setBlockReceipt(t, store, h, 10, 100)
	}
	require.NoError(t, store.PruneHistory(3)) // blocks 1-2 pruned; 3 is the oldest retained

	api := &infoAPI{backend: testInfoBackend(1000, 1), store: store}
	result, err := api.FeeHistory(t.Context(), 1, ethrpc.EarliestBlockNumber, nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(3), result.OldestBlock.ToInt())
}

// TestFeeHistoryFallsBackToLastGoodRunOnATrailingHole guards a real review finding: the
// interior-hole restart above discards its accumulated rows on any hole, including one that
// extends through end — which left a real, usable prefix un-returned in favor of an error.
func TestFeeHistoryFallsBackToLastGoodRunOnATrailingHole(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 1, 10, 100)
	setBlockReceipt(t, store, 2, 20, 100)
	setBlockReceipt(t, store, 5, 50, 100) // pushes LatestVersion to 5; blocks 3-4 are holes through end
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: store}

	result, err := api.FeeHistory(t.Context(), 4, ethrpc.BlockNumber(4), nil)
	require.NoError(t, err, "a trailing hole through end must fall back to the prefix, not error")
	require.Equal(t, big.NewInt(1), result.OldestBlock.ToInt())
	require.Equal(t, []float64{0.01, 0.02}, result.GasUsedRatio)
}

// TestFeeHistoryRestartsAfterAnInteriorHole guards a real review finding: skipping an interior
// height with no stats in place, after rows have already been emitted, would silently misattribute
// every later row to the wrong height (eth_feeHistory's row i is block oldestBlock+i). The fix
// restarts the accumulation so the result is a contiguous run ending at end.
func TestFeeHistoryRestartsAfterAnInteriorHole(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 1, 10, 100)
	setBlockReceipt(t, store, 2, 20, 100)
	// Block 3 is never written: an interior hole between 2 and 4.
	setBlockReceipt(t, store, 4, 40, 100)
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: store}

	result, err := api.FeeHistory(t.Context(), 4, ethrpc.BlockNumber(4), nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(4), result.OldestBlock.ToInt(),
		"the run must restart after the hole, not report block 4's data as block 3's")
	require.Equal(t, []float64{0.04}, result.GasUsedRatio)
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
	setBlockReceipt(t, store, 1, 10, 999) // reward value is irrelevant to the fallback slot
	api := &infoAPI{backend: testInfoBackend(1000, 1_000_000_000), store: store}

	// 33 is not in receipt.DefaultRewardPercentiles, so its slot falls back to the fixed
	// suggested price.
	result, err := api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{33})
	require.NoError(t, err)
	require.Len(t, result.Reward, 1)
	require.Equal(t, []*big.Int{big.NewInt(1_100_000_000)}, toBigInts(result.Reward[0]))
}

// TestFeeHistoryPerPercentileFallbackKeepsStoredValuesForOthers guards the fix for a real review
// finding: a miss on one requested percentile must not discard the real stored values for the
// others in the same row.
func TestFeeHistoryPerPercentileFallbackKeepsStoredValuesForOthers(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: [32]byte{1}, Receipt: &evmtypes.Receipt{TxHashHex: "0x1", BlockNumber: 1, GasUsed: 10}, Reward: big.NewInt(100)},
		{TxHash: [32]byte{2}, Receipt: &evmtypes.Receipt{TxHashHex: "0x2", BlockNumber: 1, GasUsed: 10}, Reward: big.NewInt(300)},
	}))
	api := &infoAPI{backend: testInfoBackend(1000, 1_000_000_000), store: store}

	// 0 and 100 are stored (min/max); 33 is not.
	result, err := api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{0, 33, 100})
	require.NoError(t, err)
	require.Len(t, result.Reward, 1)
	require.Equal(t,
		[]*big.Int{big.NewInt(100), big.NewInt(1_100_000_000), big.NewInt(300)},
		toBigInts(result.Reward[0]))
}

// TestFeeHistoryEmptyBlockReturnsZeros is the case an executed block with no transactions must
// still answer correctly: zero gasUsedRatio and zero reward for every requested percentile
// (go-ethereum's eth_feeHistory: "all zeroes are returned if the block is empty"), not an error
// and not the guessed price.
func TestFeeHistoryEmptyBlockReturnsZeros(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()).WithBlockHeight(1), nil))
	api := &infoAPI{backend: testInfoBackend(1000, 1_000_000_000), store: store}

	result, err := api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{25, 50, 75})
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1), result.OldestBlock.ToInt())
	require.Equal(t, []float64{0}, result.GasUsedRatio)
	require.Len(t, result.Reward, 1)
	require.Equal(t, []*big.Int{big.NewInt(0), big.NewInt(0), big.NewInt(0)}, toBigInts(result.Reward[0]))
}

func toBigInts(row []*hexutil.Big) []*big.Int {
	out := make([]*big.Int, len(row))
	for i, v := range row {
		out[i] = v.ToInt()
	}
	return out
}
