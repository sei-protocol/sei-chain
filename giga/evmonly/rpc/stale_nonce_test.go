package rpc

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type countingReceiptStore struct {
	receipt.ReceiptStore
	reads int
}

func (s *countingReceiptStore) GetReceipt(ctx sdk.Context, hash common.Hash) (*evmtypes.Receipt, error) {
	s.reads++
	return s.ReceiptStore.GetReceipt(ctx, hash)
}

func TestStaleReceiptRPCsKeepStoredPositions(t *testing.T) {
	tx, raw := testSignedTransaction(t)
	const index = 99
	store := &countingReceiptStore{ReceiptStore: evmonly.NewMemoryReceiptStore()}
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: tx.Hash(), Receipt: &evmtypes.Receipt{
			BlockNumber: 9, TransactionIndex: index, CumulativeGasUsed: 42_000,
			Status: uint32(ethtypes.ReceiptStatusFailed), VmError: "nonce too low",
		},
	}}))
	blockHash := common.HexToHash("0x9")
	block := &coretypes.ResultBlock{BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()}, Block: &tmtypes.Block{
		Header: tmtypes.Header{Height: 9, Time: time.Unix(1_700_000_000, 0)},
		Data:   tmtypes.Data{Txs: make(tmtypes.Txs, index+1)},
	}}
	for i := range block.Block.Txs {
		block.Block.Txs[i] = raw
	}
	backend := fixedGasLimitBackend(t, 100_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return block, nil
	})
	backend.minGasPrice = func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil }
	txs := &txAPI{backend: backend, store: store}
	gotReceipt, err := txs.GetTransactionReceipt(t.Context(), tx.Hash())
	require.NoError(t, err)
	require.Equal(t, 1, store.reads)
	require.Equal(t, hexutil.Uint64(index), gotReceipt["transactionIndex"])
	require.Equal(t, hexutil.Uint64(ethtypes.ReceiptStatusFailed), gotReceipt["status"])
	require.Equal(t, hexutil.Uint64(0), gotReceipt["gasUsed"])

	store.reads = 0
	gotTx, err := txs.GetTransactionByHash(t.Context(), tx.Hash())
	require.NoError(t, err)
	require.Equal(t, 1, store.reads)
	require.Equal(t, hexutil.Uint64(index), *gotTx.TransactionIndex)

	store.reads = 0
	gotBlock, err := (&blockAPI{backend: backend, store: store}).GetBlockByNumber(t.Context(), 9, false)
	require.NoError(t, err)
	require.Equal(t, 1, store.reads)
	require.Len(t, gotBlock["transactions"], index+1)
	require.Equal(t, hexutil.Uint64(42_000), gotBlock["gasUsed"])

	fullBlock, err := (&blockAPI{backend: backend, store: store}).GetBlockByNumber(t.Context(), 9, true)
	require.NoError(t, err)
	transactions := fullBlock["transactions"].([]any)
	require.Len(t, transactions, index+1)
	for i, transaction := range transactions {
		require.Equal(t, hexutil.Uint64(i), *transaction.(*export.RPCTransaction).TransactionIndex)
	}

	store.reads = 0
	history, err := (&infoAPI{backend: backend, store: store}).FeeHistory(t.Context(), 1, 9, nil)
	require.NoError(t, err)
	require.Zero(t, store.reads)
	require.Equal(t, []float64{0}, history.GasUsedRatio)
}

func TestFeeHistoryRecomputeSkipsReceiptsKeptFromOtherBlocks(t *testing.T) {
	tx, raw := testSignedTransaction(t)
	memory := evmonly.NewMemoryReceiptStore()
	require.NoError(t, memory.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: tx.Hash(), Receipt: &evmtypes.Receipt{
			BlockNumber: 9, TransactionIndex: 0, GasUsed: 21_000, CumulativeGasUsed: 21_000, EffectiveGasPrice: 7,
		},
		Reward: big.NewInt(7),
	}}))
	// Block 10 replays the transaction twice and records no stats, so fee history recomputes
	// it from the block body.
	require.NoError(t, memory.SetLatestVersion(10))
	store := stubIteratingReceiptStore{
		ReceiptStore: memory,
		iterate: func(uint64) (receipt.ReceiptIterator, error) {
			return nil, receipt.ErrRangeQueryNotSupported
		},
	}
	block := &coretypes.ResultBlock{BlockID: tmtypes.BlockID{Hash: common.HexToHash("0xa").Bytes()}, Block: &tmtypes.Block{
		Header: tmtypes.Header{Height: 10, Time: time.Unix(1_700_000_000, 0)},
		Data:   tmtypes.Data{Txs: tmtypes.Txs{raw, raw}},
	}}
	backend := fixedGasLimitBackend(t, 100_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return block, nil
	})
	backend.minGasPrice = func() (*big.Int, error) { return big.NewInt(1), nil }

	history, err := (&infoAPI{backend: backend, store: store}).FeeHistory(t.Context(), 1, 10, []float64{50})
	require.NoError(t, err)
	require.Equal(t, []float64{0}, history.GasUsedRatio)
	require.Len(t, history.Reward, 1)
	require.NotContains(t, toBigInts(history.Reward[0]), big.NewInt(7))
}
