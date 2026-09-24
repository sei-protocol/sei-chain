package rpc

import (
	"context"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestBlockNumber(t *testing.T) {
	backend := &testBackend{blockNumber: func() uint64 { return 42 }}
	api := &infoAPI{backend: backend}
	require.Equal(t, hexutil.Uint64(42), api.BlockNumber(t.Context()))
}

func TestBlockNumberEndToEnd(t *testing.T) {
	backend := &testBackend{blockNumber: func() uint64 { return 42 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Uint64
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_blockNumber"))
	require.Equal(t, hexutil.Uint64(42), got)
}

func TestChainId(t *testing.T) {
	backend := &testBackend{chainID: func() uint64 { return 713715 }}
	api := &infoAPI{backend: backend}
	require.Equal(t, (*hexutil.Big)(big.NewInt(713715)), api.ChainId(t.Context()))
}

func TestChainIdEndToEnd(t *testing.T) {
	backend := &testBackend{chainID: func() uint64 { return 713715 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Big
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_chainId"))
	require.Equal(t, *big.NewInt(713715), big.Int(got))
}

func testInfoBackend(gasLimit uint64, minGasPrice int64) *testBackend {
	return &testBackend{
		gasLimit:    func() (uint64, error) { return gasLimit, nil },
		minGasPrice: func() (*big.Int, error) { return big.NewInt(minGasPrice), nil },
	}
}

// emptyBlockBackend answers Block with a real, empty block for any height: the shape a height
// with no recorded BlockStats actually has (its receipts, if any, are just as retrievable).
func emptyBlockBackend(gasLimit uint64, minGasPrice int64) *testBackend {
	backend := testInfoBackend(gasLimit, minGasPrice)
	backend.block = func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{Block: &tmtypes.Block{}}, nil
	}
	return backend
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
	// gasLimit 1000, TotalGasUsed 900 -> 90% > the 80% congestion threshold -> median reward,
	// which is above the floor here so the escalation is what's under test, not the floor clamp.
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: [32]byte{1}, Receipt: &evmtypes.Receipt{TxHashHex: "0x1", BlockNumber: 1, GasUsed: 900}, Reward: big.NewInt(500)},
	}))
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: store}

	price, err := api.GasPrice(t.Context())
	require.NoError(t, err)
	require.Equal(t, big.NewInt(500), price.ToInt())
}

// TestGasPriceFallsBackWhenTheMedianIsBelowTheFloor verifies GasPrice falls back to the margin
// when the congested-block median is below the current floor.
func TestGasPriceFallsBackWhenTheMedianIsBelowTheFloor(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	// gasLimit 1000, TotalGasUsed 900 -> congested, but the one included tx's reward (50) is
	// below the current floor (100).
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: [32]byte{1}, Receipt: &evmtypes.Receipt{TxHashHex: "0x1", BlockNumber: 1, GasUsed: 900}, Reward: big.NewInt(50)},
	}))
	api := &infoAPI{backend: testInfoBackend(1000, 100), store: store}

	price, err := api.GasPrice(t.Context())
	require.NoError(t, err)
	require.Equal(t, big.NewInt(110), price.ToInt())
}

// TestGasPriceCongestionNeverAnswersBelowTheMargin verifies a congested reward equal to the
// floor falls back to the margin instead of answering below it.
func TestGasPriceCongestionNeverAnswersBelowTheMargin(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	// gasLimit 1000, TotalGasUsed 900 -> congested; the one included tx's reward (1000) equals
	// the floor (1000), which is below the floor's own 10% margin (1100).
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: [32]byte{1}, Receipt: &evmtypes.Receipt{TxHashHex: "0x1", BlockNumber: 1, GasUsed: 900}, Reward: big.NewInt(1000)},
	}))
	api := &infoAPI{backend: testInfoBackend(1000, 1000), store: store}

	price, err := api.GasPrice(t.Context())
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1100), price.ToInt())
}

// TestGasPriceNotCongestedAtExactlyTheThreshold verifies a block exactly at the 80% threshold
// is not treated as congested.
func TestGasPriceNotCongestedAtExactlyTheThreshold(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	// gasLimit 1000, TotalGasUsed 800 -> exactly 80%, not > the threshold.
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: [32]byte{1}, Receipt: &evmtypes.Receipt{TxHashHex: "0x1", BlockNumber: 1, GasUsed: 800}, Reward: big.NewInt(500)},
	}))
	api := &infoAPI{backend: testInfoBackend(1000, 1_000_000_000), store: store}

	price, err := api.GasPrice(t.Context())
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1_100_000_000), price.ToInt())
}

func TestGasPriceFallsBackWhenTheMedianIsntStored(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	// A congested block (90% > the 80% threshold) with no reward-eligible tx, so no percentile
	// was ever computed for it: GasPrice must fall back rather than guess at one.
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

func TestFeeHistorySkipsPrunedHeights(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 3, 10, 100)
	require.NoError(t, store.PruneHistory(3)) // blocks 1-2 pruned; 3 is the oldest retained
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: store}

	result, err := api.FeeHistory(t.Context(), 3, ethrpc.BlockNumber(3), nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(3), result.OldestBlock.ToInt(), "the earliest retained height, not the earliest requested")
	require.Equal(t, []float64{0.01}, result.GasUsedRatio)
}

// TestFeeHistoryEarliestRespectsThePruneFloor verifies "earliest" resolves to the oldest
// retained height, not a hardcoded 1, once history has been pruned.
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

// TestFeeHistoryRestartsAfterAGenuineInteriorHole verifies an interior ErrNotFound hole restarts
// the accumulation instead of misattributing a later block's data.
func TestFeeHistoryRestartsAfterAGenuineInteriorHole(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	for h := uint64(1); h <= 4; h++ {
		setBlockReceipt(t, store, h, h*10, 100)
	}
	holeStore := stubBlockStatsStore{ReceiptStore: store, holeHeights: map[uint64]bool{3: true}}
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: holeStore}

	result, err := api.FeeHistory(t.Context(), 4, ethrpc.BlockNumber(4), nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(4), result.OldestBlock.ToInt(),
		"the run must restart after the hole, not report block 4's data as block 3's")
	require.Equal(t, []float64{0.04}, result.GasUsedRatio)
}

// TestFeeHistoryFallsBackToLastGoodRunOnAGenuineTrailingHole verifies a trailing ErrNotFound
// hole through end falls back to the last good prefix instead of erroring.
func TestFeeHistoryFallsBackToLastGoodRunOnAGenuineTrailingHole(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	for h := uint64(1); h <= 4; h++ {
		setBlockReceipt(t, store, h, h*10, 100)
	}
	holeStore := stubBlockStatsStore{ReceiptStore: store, holeHeights: map[uint64]bool{3: true, 4: true}}
	api := &infoAPI{backend: testInfoBackend(1000, 1), store: holeStore}

	result, err := api.FeeHistory(t.Context(), 4, ethrpc.BlockNumber(4), nil)
	require.NoError(t, err, "a trailing hole through end must fall back to the prefix, not error")
	require.Equal(t, big.NewInt(1), result.OldestBlock.ToInt())
	require.Equal(t, []float64{0.01, 0.02}, result.GasUsedRatio)
}

// TestFeeHistoryRecomputesATrailingUnstatedHeight verifies an ErrBlockStatsNotSupported height
// recomputes from receipts instead of failing as an unrecoverable hole.
func TestFeeHistoryRecomputesATrailingUnstatedHeight(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 1, 10, 100)
	setBlockReceipt(t, store, 2, 20, 100)
	setBlockReceipt(t, store, 5, 50, 100) // pushes LatestVersion to 5; blocks 3-4 have no stats
	api := &infoAPI{backend: emptyBlockBackend(1000, 1), store: store}

	result, err := api.FeeHistory(t.Context(), 4, ethrpc.BlockNumber(4), nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1), result.OldestBlock.ToInt())
	require.Equal(t, []float64{0.01, 0.02, 0, 0}, result.GasUsedRatio)
}

// TestFeeHistoryErrorsRatherThanPanicsWhenTheBlockBodyIsGone verifies a nil block or block.Block
// during recompute returns an error, not a panic.
func TestFeeHistoryErrorsRatherThanPanicsWhenTheBlockBodyIsGone(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 2, 20, 100) // pushes LatestVersion to 2; block 1 has no BlockStats

	backend := testInfoBackend(1000, 1)
	backend.block = func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{Block: nil}, nil
	}
	api := &infoAPI{backend: backend, store: stubIteratingReceiptStore{
		ReceiptStore: store,
		iterate: func(uint64) (receipt.ReceiptIterator, error) {
			return nil, receipt.ErrRangeQueryNotSupported
		},
	}}

	_, err := api.FeeHistory(t.Context(), 1, ethrpc.BlockNumber(1), nil)
	require.ErrorContains(t, err, "not available")
}

// TestFeeHistoryRecomputesAnInteriorUnstatedHeight verifies an interior unstated height
// recomputes and takes its own row instead of being skipped.
func TestFeeHistoryRecomputesAnInteriorUnstatedHeight(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	setBlockReceipt(t, store, 1, 10, 100)
	setBlockReceipt(t, store, 2, 20, 100)
	// Block 3 has no BlockStats recorded, unlike its neighbors.
	setBlockReceipt(t, store, 4, 40, 100)
	api := &infoAPI{backend: emptyBlockBackend(1000, 1), store: store}

	result, err := api.FeeHistory(t.Context(), 4, ethrpc.BlockNumber(4), nil)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1), result.OldestBlock.ToInt())
	require.Equal(t, []float64{0.01, 0.02, 0, 0.04}, result.GasUsedRatio)
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

// TestFeeHistoryRecomputesViaIterateReceiptsWhenSupported verifies the recompute path prefers
// IterateReceipts over decoding the block and fetching receipts one at a time: backend.block is
// left nil, so a fall-through to that path would panic.
func TestFeeHistoryRecomputesViaIterateReceiptsWhenSupported(t *testing.T) {
	baseStore := evmonly.NewMemoryReceiptStore()
	tx, _ := testSignedTransaction(t)
	receiptRecord := &evmtypes.Receipt{TxHashHex: tx.Hash().Hex(), BlockNumber: 1, GasUsed: 10, EffectiveGasPrice: 999}
	require.NoError(t, baseStore.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash:  tx.Hash(),
		Receipt: receiptRecord,
		Reward:  big.NewInt(999),
	}}))
	store := stubIteratingReceiptStore{
		ReceiptStore: baseStore,
		iterate: func(uint64) (receipt.ReceiptIterator, error) {
			return &fakeReceiptIterator{entries: []fakeReceiptEntry{
				{blockNumber: 1, txHash: tx.Hash(), receipt: receiptRecord},
			}}, nil
		},
	}
	api := &infoAPI{backend: testInfoBackend(1000, 1_000_000_000), store: store}

	// 33 is not in receipt.DefaultRewardPercentiles, so this recomputes.
	result, err := api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{33})
	require.NoError(t, err)
	require.Len(t, result.Reward, 1)
	require.Equal(t, []*big.Int{big.NewInt(999)}, toBigInts(result.Reward[0]))
}

// TestFeeHistoryRecomputesAnUncachedPercentileFromReceipts guards the feeHistory contract: a
// percentile the cache doesn't cover must be recomputed from receipts, never guessed.
func TestFeeHistoryRecomputesAnUncachedPercentileFromReceipts(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	tx, raw := testSignedTransaction(t)
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash:  tx.Hash(),
		Receipt: &evmtypes.Receipt{TxHashHex: tx.Hash().Hex(), BlockNumber: 1, GasUsed: 10, EffectiveGasPrice: 999},
		Reward:  big.NewInt(999),
	}}))
	backend := testInfoBackend(1000, 1_000_000_000)
	backend.block = func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{Block: &tmtypes.Block{Data: tmtypes.Data{Txs: tmtypes.Txs{tmtypes.Tx(raw)}}}}, nil
	}
	api := &infoAPI{backend: backend, store: store}

	// 33 is not in receipt.DefaultRewardPercentiles, so this recomputes from receipts.
	result, err := api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{33})
	require.NoError(t, err)
	require.Len(t, result.Reward, 1)
	require.Equal(t, []*big.Int{big.NewInt(999)}, toBigInts(result.Reward[0]))
}

// TestFeeHistoryRecomputesWholeRowWhenOnePercentileIsUncached guards the coverage rule: a miss on
// one percentile recomputes the whole row from receipts, not a mix of cached and guessed values.
func TestFeeHistoryRecomputesWholeRowWhenOnePercentileIsUncached(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	tx1, raw1 := testSignedTransactionWithNonce(t, 0)
	tx2, raw2 := testSignedTransactionWithNonce(t, 1)
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: tx1.Hash(), Receipt: &evmtypes.Receipt{TxHashHex: tx1.Hash().Hex(), BlockNumber: 1, GasUsed: 10, EffectiveGasPrice: 100}, Reward: big.NewInt(100)},
		{TxHash: tx2.Hash(), Receipt: &evmtypes.Receipt{TxHashHex: tx2.Hash().Hex(), BlockNumber: 1, GasUsed: 10, EffectiveGasPrice: 300}, Reward: big.NewInt(300)},
	}))
	backend := testInfoBackend(1000, 1_000_000_000)
	backend.block = func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{Block: &tmtypes.Block{Data: tmtypes.Data{Txs: tmtypes.Txs{tmtypes.Tx(raw1), tmtypes.Tx(raw2)}}}}, nil
	}
	api := &infoAPI{backend: backend, store: store}

	// 0 and 100 are cached (min/max); 33 is not, so the whole row recomputes from receipts.
	result, err := api.FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{0, 33, 100})
	require.NoError(t, err)
	require.Len(t, result.Reward, 1)
	require.Equal(t,
		[]*big.Int{big.NewInt(100), big.NewInt(100), big.NewInt(300)},
		toBigInts(result.Reward[0]))
}

// TestFeeHistoryEmptyBlockReturnsZeros verifies an empty block answers zero gasUsedRatio and
// zero reward for every requested percentile.
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
