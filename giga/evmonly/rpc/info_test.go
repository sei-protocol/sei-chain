package rpc

import (
	"context"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gmath "github.com/ethereum/go-ethereum/common/math"
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

func TestGasPriceReturnsTenPercentAboveTheAdmissionFloor(t *testing.T) {
	backend := &testBackend{minGasPrice: func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil }}

	got, err := (&infoAPI{backend: backend}).GasPrice(t.Context())

	require.NoError(t, err)
	require.Equal(t, (*hexutil.Big)(big.NewInt(1_100_000_000)), got)
}

func TestGasPriceEndToEnd(t *testing.T) {
	backend := &testBackend{minGasPrice: func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Big
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_gasPrice"))
	require.Equal(t, *big.NewInt(1_100_000_000), big.Int(got))
}

// heightRangeBackend serves a synthetic chain of maxHeight empty blocks, for
// exercising eth_feeHistory's range walk and height resolution without
// per-transaction receipt data.
func heightRangeBackend(t *testing.T, maxHeight int64, gasLimit uint64, minGasPrice *big.Int) *testBackend {
	t.Helper()
	return &testBackend{
		gasLimit:    func() (uint64, error) { return gasLimit, nil },
		minGasPrice: func() (*big.Int, error) { return minGasPrice, nil },
		block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			height := maxHeight
			if req.Height != nil {
				height = int64(*req.Height)
			}
			if height < 1 {
				return nil, fmt.Errorf("%w: %d", coretypes.ErrZeroOrNegativeHeight, height)
			}
			if height > maxHeight {
				return nil, fmt.Errorf("%w: %d", coretypes.ErrHeightExceedsChainHead, height)
			}
			return &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: common.BigToHash(big.NewInt(height)).Bytes()},
				Block:   &tmtypes.Block{Header: tmtypes.Header{Height: height, Time: time.Unix(1_700_000_000, 0)}},
			}, nil
		},
	}
}

func TestFeeHistoryBlockCountLessThanOneReturnsEmptyResult(t *testing.T) {
	got, err := (&infoAPI{backend: &testBackend{}}).FeeHistory(t.Context(), 0, ethrpc.LatestBlockNumber, nil)

	require.NoError(t, err)
	require.Equal(t, &FeeHistoryResult{}, got)
}

func TestFeeHistoryRejectsDescendingPercentiles(t *testing.T) {
	_, err := (&infoAPI{backend: &testBackend{}}).FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{50, 10})
	require.Error(t, err)
}

func TestFeeHistoryRejectsOutOfRangePercentiles(t *testing.T) {
	_, err := (&infoAPI{backend: &testBackend{}}).FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{-1})
	require.Error(t, err)

	_, err = (&infoAPI{backend: &testBackend{}}).FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{101})
	require.Error(t, err)
}

func TestFeeHistoryCurrentStateTagsResolveToTheLatestBlock(t *testing.T) {
	for _, tag := range []ethrpc.BlockNumber{
		ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber,
	} {
		backend := heightRangeBackend(t, 9, 100_000, big.NewInt(1_000_000_000))
		got, err := (&infoAPI{backend: backend}).FeeHistory(t.Context(), 1, tag, nil)
		require.NoError(t, err)
		require.Equal(t, (*hexutil.Big)(big.NewInt(9)), got.OldestBlock)
	}
}

func TestFeeHistoryAcceptsAnExplicitHistoricalHeight(t *testing.T) {
	backend := heightRangeBackend(t, 9, 100_000, big.NewInt(1_000_000_000))

	got, err := (&infoAPI{backend: backend}).FeeHistory(t.Context(), 1, ethrpc.BlockNumber(3), nil)

	require.NoError(t, err)
	require.Equal(t, (*hexutil.Big)(big.NewInt(3)), got.OldestBlock)
}

func TestFeeHistoryEarliestReturnsEmptyResult(t *testing.T) {
	backend := heightRangeBackend(t, 9, 100_000, big.NewInt(1_000_000_000))

	got, err := (&infoAPI{backend: backend}).FeeHistory(t.Context(), 1, ethrpc.EarliestBlockNumber, nil)

	require.NoError(t, err)
	require.Equal(t, &FeeHistoryResult{}, got)
}

func TestFeeHistoryFutureHeightReturnsEmptyResult(t *testing.T) {
	backend := heightRangeBackend(t, 9, 100_000, big.NewInt(1_000_000_000))

	got, err := (&infoAPI{backend: backend}).FeeHistory(t.Context(), 1, ethrpc.BlockNumber(100), nil)

	require.NoError(t, err)
	require.Equal(t, &FeeHistoryResult{}, got)
}

func TestFeeHistoryTrimsRangeBelowTheChainHead(t *testing.T) {
	backend := heightRangeBackend(t, 3, 100_000, big.NewInt(1_000_000_000))

	got, err := (&infoAPI{backend: backend}).FeeHistory(t.Context(), 10, ethrpc.BlockNumber(3), nil)

	require.NoError(t, err)
	require.Equal(t, (*hexutil.Big)(big.NewInt(1)), got.OldestBlock)
	require.Len(t, got.GasUsedRatio, 3)
}

func TestFeeHistoryCapsBlockCountAtTheMaximum(t *testing.T) {
	backend := heightRangeBackend(t, 2000, 100_000, big.NewInt(1_000_000_000))

	got, err := (&infoAPI{backend: backend}).FeeHistory(t.Context(), 2000, ethrpc.BlockNumber(2000), nil)

	require.NoError(t, err)
	require.Len(t, got.GasUsedRatio, maxFeeHistoryBlockCount)
	require.Equal(t, (*hexutil.Big)(big.NewInt(2000-maxFeeHistoryBlockCount+1)), got.OldestBlock)
}

func TestFeeHistorySingleBlockReportsGasUsedRatioAndFixedReward(t *testing.T) {
	block, _, _, store := multiTxBlock(t, 9, common.HexToHash("0xabcd"), time.Unix(1_700_000_000, 0))
	backend := &testBackend{
		gasLimit:    func() (uint64, error) { return 100_000, nil },
		minGasPrice: func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil },
		block:       func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) { return block, nil },
	}

	got, err := (&infoAPI{backend: backend, store: store}).FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, []float64{10, 50, 90})

	require.NoError(t, err)
	require.Equal(t, (*hexutil.Big)(big.NewInt(9)), got.OldestBlock)
	require.Equal(t, []float64{43_500.0 / 100_000.0}, got.GasUsedRatio)
	require.Equal(t, []*hexutil.Big{(*hexutil.Big)(new(big.Int)), (*hexutil.Big)(new(big.Int))}, got.BaseFee)
	require.Equal(t, [][]*hexutil.Big{{
		(*hexutil.Big)(big.NewInt(1_000_000_000)),
		(*hexutil.Big)(big.NewInt(1_000_000_000)),
		(*hexutil.Big)(big.NewInt(1_000_000_000)),
	}}, got.Reward)
}

func TestFeeHistoryOmitsRewardWhenNoPercentilesAreRequested(t *testing.T) {
	block, _, _, store := multiTxBlock(t, 9, common.HexToHash("0xabcd"), time.Unix(1_700_000_000, 0))
	backend := &testBackend{
		gasLimit:    func() (uint64, error) { return 100_000, nil },
		minGasPrice: func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil },
		block:       func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) { return block, nil },
	}

	got, err := (&infoAPI{backend: backend, store: store}).FeeHistory(t.Context(), 1, ethrpc.LatestBlockNumber, nil)

	require.NoError(t, err)
	require.Nil(t, got.Reward)
}

func TestFeeHistoryMultiBlockRangeReportsPerBlockGasUsedRatio(t *testing.T) {
	tx1, raw1 := testSignedTransaction(t)
	tx2, raw2 := secondSignedTransaction(t)
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: tx1.Hash(), Receipt: &evmtypes.Receipt{TxHashHex: tx1.Hash().Hex(), BlockNumber: 8, CumulativeGasUsed: 10_000}},
		{TxHash: tx2.Hash(), Receipt: &evmtypes.Receipt{TxHashHex: tx2.Hash().Hex(), BlockNumber: 9, CumulativeGasUsed: 20_000}},
	}))
	blocks := map[int64]*coretypes.ResultBlock{
		8: {
			BlockID: tmtypes.BlockID{Hash: common.HexToHash("0x8").Bytes()},
			Block: &tmtypes.Block{
				Header: tmtypes.Header{Height: 8, Time: time.Unix(1_700_000_000, 0)},
				Data:   tmtypes.Data{Txs: tmtypes.Txs{raw1}},
			},
		},
		9: {
			BlockID: tmtypes.BlockID{Hash: common.HexToHash("0x9").Bytes()},
			Block: &tmtypes.Block{
				Header: tmtypes.Header{Height: 9, Time: time.Unix(1_700_000_001, 0)},
				Data:   tmtypes.Data{Txs: tmtypes.Txs{raw2}},
			},
		},
	}
	backend := &testBackend{
		gasLimit:    func() (uint64, error) { return 100_000, nil },
		minGasPrice: func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil },
		block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			height := int64(9)
			if req.Height != nil {
				height = int64(*req.Height)
			}
			block, ok := blocks[height]
			if !ok {
				return nil, fmt.Errorf("%w: %d", coretypes.ErrZeroOrNegativeHeight, height)
			}
			return block, nil
		},
	}

	got, err := (&infoAPI{backend: backend, store: store}).FeeHistory(t.Context(), 2, ethrpc.LatestBlockNumber, nil)

	require.NoError(t, err)
	require.Equal(t, (*hexutil.Big)(big.NewInt(8)), got.OldestBlock)
	require.Equal(t, []float64{0.1, 0.2}, got.GasUsedRatio)
	require.Len(t, got.BaseFee, 3)
}

func TestFeeHistoryEndToEnd(t *testing.T) {
	block, _, _, store := multiTxBlock(t, 9, common.HexToHash("0xabcd"), time.Unix(1_700_000_000, 0))
	backend := &testBackend{
		gasLimit:    func() (uint64, error) { return 100_000, nil },
		minGasPrice: func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil },
		block:       func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) { return block, nil },
	}
	handler, err := newHandler(backend, store)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got FeeHistoryResult
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_feeHistory", gmath.HexOrDecimal64(1), "latest", []float64{50}))
	require.Equal(t, (*hexutil.Big)(big.NewInt(9)), got.OldestBlock)
	require.Equal(t, []float64{43_500.0 / 100_000.0}, got.GasUsedRatio)
	require.Len(t, got.BaseFee, 2)
	require.Equal(t, [][]*hexutil.Big{{(*hexutil.Big)(big.NewInt(1_000_000_000))}}, got.Reward)
}
