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
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type countingReceiptStore struct {
	receiptdb.ReceiptStore
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
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipttypes.ReceiptRecord{{
		TxHash: tx.Hash(), Receipt: &evmtypes.Receipt{
			BlockNumber: 9, TransactionIndex: index, CumulativeGasUsed: 42_000,
			Status: uint32(ethtypes.ReceiptStatusFailed), VmError: "nonce too low",
		},
	}}))
	blockHash := common.HexToHash("0x9")
	block := &abevm.EvmBlock{
		Hash:   blockHash,
		Height: 9,
		Time:   time.Unix(1_700_000_000, 0),
		Txs:    make([]abevm.RawTx, index+1),
	}

	for i := range block.Txs {
		block.Txs[i] = raw
	}
	backend := fixedGasLimitBackend(t, 100_000, func(context.Context, *coretypes.RequestBlockInfo) (*abevm.EvmBlock, error) {
		return block, nil
	})
	backend.minGasPrice = func() *big.Int { return big.NewInt(1_000_000_000) }
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
	// Block gas comes from the stored block stats, which count the stale
	// transaction's zero gas rather than its cumulative total.
	require.Zero(t, store.reads)
	require.Len(t, gotBlock["transactions"], index+1)
	require.Equal(t, hexutil.Uint64(0), gotBlock["gasUsed"])

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
	require.NoError(t, memory.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipttypes.ReceiptRecord{{
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
		iterate: func(uint64) (receiptdb.ReceiptIterator, error) {
			return nil, receiptdb.ErrRangeQueryNotSupported
		},
	}
	block := &abevm.EvmBlock{
		Hash:   common.HexToHash("0xa"),
		Height: 10,
		Time:   time.Unix(1_700_000_000, 0),
		Txs:    []abevm.RawTx{raw, raw},
	}

	backend := fixedGasLimitBackend(t, 100_000, func(context.Context, *coretypes.RequestBlockInfo) (*abevm.EvmBlock, error) {
		return block, nil
	})
	backend.minGasPrice = func() *big.Int { return big.NewInt(1) }

	history, err := (&infoAPI{backend: backend, store: store}).FeeHistory(t.Context(), 1, 10, []float64{50})
	require.NoError(t, err)
	require.Equal(t, []float64{0}, history.GasUsedRatio)
	require.Len(t, history.Reward, 1)
	require.NotContains(t, toBigInts(history.Reward[0]), big.NewInt(7))
}
