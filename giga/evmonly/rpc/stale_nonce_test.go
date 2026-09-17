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

func TestStaleTransactionsAreAbsentFromRPCBlocks(t *testing.T) {
	first, rawFirst := testSignedTransaction(t)
	second, rawSecond := secondSignedTransaction(t)
	missing := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 99})
	rawMissing, err := missing.MarshalBinary()
	require.NoError(t, err)
	prior := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 98})
	rawPrior, err := prior.MarshalBinary()
	require.NoError(t, err)
	store := evmonly.NewMemoryReceiptStore()
	ctx := sdk.Context{}.WithContext(t.Context())
	require.NoError(t, store.SetReceipts(ctx, []receipt.ReceiptRecord{
		{TxHash: prior.Hash(), Receipt: &evmtypes.Receipt{BlockNumber: 8, TransactionIndex: 0, CumulativeGasUsed: 21_000}},
		{TxHash: first.Hash(), Receipt: &evmtypes.Receipt{BlockNumber: 9, TransactionIndex: 2, CumulativeGasUsed: 21_000}},
		{TxHash: second.Hash(), Receipt: &evmtypes.Receipt{
			BlockNumber: 9, TransactionIndex: 4, CumulativeGasUsed: 42_000,
			Logs: []*evmtypes.Log{{Index: 0}},
		}},
	}))
	blockHash := common.HexToHash("0x9")
	blocks := map[int64]*coretypes.ResultBlock{
		8: {BlockID: tmtypes.BlockID{Hash: common.HexToHash("0x8").Bytes()}, Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: 8, Time: time.Unix(1_700_000_000, 0)},
			Data:   tmtypes.Data{Txs: tmtypes.Txs{rawPrior}},
		}},
		9: {BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()}, Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: 9, Time: time.Unix(1_700_000_001, 0)},
			Data:   tmtypes.Data{Txs: tmtypes.Txs{rawMissing, rawPrior, rawFirst, rawFirst, rawSecond, rawFirst, rawMissing}},
		}},
	}
	backend := fixedGasLimitBackend(t, 100_000, func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		if req.Height == nil {
			return blocks[9], nil
		}
		return blocks[int64(*req.Height)], nil
	})
	backend.blockByHash = func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
		return blocks[9], nil
	}
	backend.minGasPrice = func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil }
	blockRPC := &blockAPI{backend: backend, store: store}
	for _, full := range []bool{false, true} {
		byNumber, err := blockRPC.GetBlockByNumber(t.Context(), 9, full)
		require.NoError(t, err)
		byHash, err := blockRPC.GetBlockByHash(t.Context(), blockHash, full)
		require.NoError(t, err)
		require.Equal(t, byNumber, byHash)
		require.Equal(t, hexutil.Uint64(42_000), byNumber["gasUsed"])
		transactions := byNumber["transactions"].([]any)
		require.Len(t, transactions, 2)
		for i, hash := range []common.Hash{first.Hash(), second.Hash()} {
			if full {
				tx := transactions[i].(*export.RPCTransaction)
				require.Equal(t, hash, tx.Hash)
				require.Equal(t, hexutil.Uint64(i), *tx.TransactionIndex)
			} else {
				require.Equal(t, hash, transactions[i])
			}
		}
	}
	txRPC := &txAPI{backend: backend, store: store}
	for i, hash := range []common.Hash{first.Hash(), second.Hash()} {
		tx, err := txRPC.GetTransactionByHash(t.Context(), hash)
		require.NoError(t, err)
		require.Equal(t, hash, tx.Hash)
		require.Equal(t, hexutil.Uint64(i), *tx.TransactionIndex)
		got, err := txRPC.GetTransactionReceipt(t.Context(), hash)
		require.NoError(t, err)
		require.Equal(t, hexutil.Uint64(i), got["transactionIndex"])
		for _, log := range got["logs"].([]*ethtypes.Log) {
			require.Equal(t, uint(i), log.TxIndex)
		}
	}
	missingTx, err := txRPC.GetTransactionByHash(t.Context(), missing.Hash())
	require.NoError(t, err)
	require.Nil(t, missingTx)
	missingReceipt, err := txRPC.GetTransactionReceipt(t.Context(), missing.Hash())
	require.NoError(t, err)
	require.Nil(t, missingReceipt)
	original, err := txRPC.GetTransactionReceipt(t.Context(), prior.Hash())
	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(8), original["blockNumber"])
	history, err := (&infoAPI{backend: backend, store: store}).FeeHistory(t.Context(), 1, 9, nil)
	require.NoError(t, err)
	require.Equal(t, []float64{0.42}, history.GasUsedRatio)
	stored, err := store.GetReceipt(ctx, second.Hash())
	require.NoError(t, err)
	require.Equal(t, uint32(4), stored.TransactionIndex)
}
