package rpc

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
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

// TestRPCsOmitTransactionsWithoutReceipts serves a block whose second transaction the executor
// rejected and whose last replays the first, with the receipts the executor stores for it: only
// the two executed transactions are listed, counted and found, each at its receipt's index.
func TestRPCsOmitTransactionsWithoutReceipts(t *testing.T) {
	first, rawFirst := testSignedTransactionWithNonce(t, 0)
	rejected, rawRejected := testSignedTransactionWithNonce(t, 5)
	last, rawLast := testSignedTransactionWithNonce(t, 1)
	memory := evmonly.NewMemoryReceiptStore()
	require.NoError(t, memory.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{TxHash: first.Hash(), Receipt: &evmtypes.Receipt{
			TxHashHex: first.Hash().Hex(), BlockNumber: 9, TransactionIndex: 0, UnadjustedTransactionIndex: 0,
			GasUsed: 21_000, CumulativeGasUsed: 21_000,
		}},
		{TxHash: last.Hash(), Receipt: &evmtypes.Receipt{
			TxHashHex: last.Hash().Hex(), BlockNumber: 9, TransactionIndex: 1, UnadjustedTransactionIndex: 2,
			GasUsed: 21_000, CumulativeGasUsed: 42_000,
		}},
	}))
	blockHash := common.HexToHash("0x9")
	block := &coretypes.ResultBlock{BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()}, Block: &tmtypes.Block{
		Header: tmtypes.Header{Height: 9, Time: time.Unix(1_700_000_000, 0)},
		Data:   tmtypes.Data{Txs: tmtypes.Txs{rawFirst, rawRejected, rawLast, rawFirst}},
	}}
	backend := fixedGasLimitBackend(t, 100_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return block, nil
	})

	// A store that walks its receipts and one that is only looked up by hash list the same block.
	for _, store := range []receipt.ReceiptStore{memory, hashOnlyReceiptStore{ReceiptStore: memory}} {
		blocks := &blockAPI{backend: backend, store: store}
		hashes, err := blocks.GetBlockByNumber(t.Context(), 9, false)
		require.NoError(t, err)
		require.Equal(t, []any{first.Hash(), last.Hash()}, hashes["transactions"])
		require.Equal(t, hexutil.Uint64(42_000), hashes["gasUsed"])
		full, err := blocks.GetBlockByNumber(t.Context(), 9, true)
		require.NoError(t, err)
		transactions := full["transactions"].([]any)
		require.Len(t, transactions, 2)
		for i, want := range []common.Hash{first.Hash(), last.Hash()} {
			transaction := transactions[i].(*export.RPCTransaction)
			require.Equal(t, want, transaction.Hash)
			require.Equal(t, hexutil.Uint64(i), *transaction.TransactionIndex)
		}
		count, err := blocks.GetBlockTransactionCountByNumber(t.Context(), 9)
		require.NoError(t, err)
		require.Equal(t, hexutil.Uint(2), *count)
		byIndex, err := blocks.GetTransactionByBlockNumberAndIndex(t.Context(), 9, 1)
		require.NoError(t, err)
		require.Equal(t, last.Hash(), byIndex.Hash)
		require.Equal(t, hexutil.Uint64(1), *byIndex.TransactionIndex)
		pastList, err := blocks.GetTransactionByBlockNumberAndIndex(t.Context(), 9, 2)
		require.NoError(t, err)
		require.Nil(t, pastList, "an index inside the body but past the list answers null")
	}

	txs := &txAPI{backend: backend, store: memory}
	gotTx, err := txs.GetTransactionByHash(t.Context(), last.Hash())
	require.NoError(t, err)
	require.Equal(t, last.Hash(), gotTx.Hash)
	require.Equal(t, hexutil.Uint64(1), *gotTx.TransactionIndex)
	gotReceipt, err := txs.GetTransactionReceipt(t.Context(), last.Hash())
	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(1), gotReceipt["transactionIndex"])
	for field := range gotReceipt {
		require.NotContains(t, strings.ToLower(field), "unadjusted", "the unadjusted index is not surfaced")
	}
	missingTx, err := txs.GetTransactionByHash(t.Context(), rejected.Hash())
	require.NoError(t, err)
	require.Nil(t, missingTx)
	missingReceipt, err := txs.GetTransactionReceipt(t.Context(), rejected.Hash())
	require.NoError(t, err)
	require.Nil(t, missingReceipt)

	// Until the receipt store covers the block, its body is listed as it stands.
	pending := evmonly.NewMemoryReceiptStore()
	require.NoError(t, pending.SetLatestVersion(8))
	unindexed, err := (&blockAPI{backend: backend, store: pending}).GetBlockByNumber(t.Context(), 9, false)
	require.NoError(t, err)
	require.Equal(t, []any{first.Hash(), rejected.Hash(), last.Hash(), first.Hash()}, unindexed["transactions"])
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
