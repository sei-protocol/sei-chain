package rpc

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
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

func TestBlockGasRPCsUseLastReceiptAtMatchingHeight(t *testing.T) {
	for _, tc := range []struct {
		name       string
		first      bool
		lastHeight uint64
		want       uint64
	}{
		{name: "last receipt matches", first: true, lastHeight: 9, want: 43_500},
		{name: "trailing receipt missing", first: true, want: 21_000},
		{name: "trailing receipt from earlier block", first: true, lastHeight: 8, want: 21_000},
		{name: "trailing receipt from later block", first: true, lastHeight: 10, want: 21_000},
		{name: "no matching receipts", lastHeight: 8},
		{name: "all receipts missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx1, raw1 := testSignedTransaction(t)
			tx2, raw2 := secondSignedTransaction(t)
			store := evmonly.NewMemoryReceiptStore()
			var records []receipt.ReceiptRecord
			if tc.first {
				records = append(records, receipt.ReceiptRecord{TxHash: tx1.Hash(), Receipt: &evmtypes.Receipt{
					TxHashHex: tx1.Hash().Hex(), BlockNumber: 9, CumulativeGasUsed: 21_000,
				}})
			}
			if tc.lastHeight != 0 {
				records = append(records, receipt.ReceiptRecord{TxHash: tx2.Hash(), Receipt: &evmtypes.Receipt{
					TxHashHex: tx2.Hash().Hex(), BlockNumber: tc.lastHeight, CumulativeGasUsed: 43_500,
				}})
			}
			require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), records))
			blockHash := common.HexToHash("0x9")
			block := &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
				Block: &tmtypes.Block{
					Header: tmtypes.Header{Height: 9, Time: time.Unix(1_700_000_000, 0)},
					Data:   tmtypes.Data{Txs: tmtypes.Txs{raw1, raw2, raw2}},
				},
			}
			backend := fixedGasLimitBackend(t, 100_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
				return block, nil
			})
			backend.blockByHash = func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
				return block, nil
			}
			backend.minGasPrice = func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil }
			blocks := &blockAPI{backend: backend, store: store}
			for _, fullTx := range []bool{false, true} {
				byNumber, err := blocks.GetBlockByNumber(t.Context(), 9, fullTx)
				require.NoError(t, err)
				require.Equal(t, hexutil.Uint64(tc.want), byNumber["gasUsed"])
				byHash, err := blocks.GetBlockByHash(t.Context(), blockHash, fullTx)
				require.NoError(t, err)
				require.Equal(t, hexutil.Uint64(tc.want), byHash["gasUsed"])
			}
			history, err := (&infoAPI{backend: backend, store: store}).FeeHistory(t.Context(), 1, ethrpc.BlockNumber(9), nil)
			require.NoError(t, err)
			require.Equal(t, []float64{float64(tc.want) / 100_000}, history.GasUsedRatio)
		})
	}
}

type failingGasReceiptStore struct {
	receipt.ReceiptStore
	hash common.Hash
	err  error
}

func (s failingGasReceiptStore) GetReceipt(ctx sdk.Context, hash common.Hash) (*evmtypes.Receipt, error) {
	if hash == s.hash {
		return nil, s.err
	}
	return s.ReceiptStore.GetReceipt(ctx, hash)
}

func TestBlockGasRPCsPropagateReceiptReadErrors(t *testing.T) {
	tx, raw := testSignedTransaction(t)
	_, trailing := secondSignedTransaction(t)
	readErr := errors.New("receipt read failed")
	store := failingGasReceiptStore{ReceiptStore: evmonly.NewMemoryReceiptStore(), hash: tx.Hash(), err: readErr}
	block := &coretypes.ResultBlock{Block: &tmtypes.Block{
		Header: tmtypes.Header{Height: 9, Time: time.Unix(1_700_000_000, 0)},
		Data:   tmtypes.Data{Txs: tmtypes.Txs{raw, trailing}},
	}}
	backend := fixedGasLimitBackend(t, 100_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return block, nil
	})
	backend.minGasPrice = func() (*big.Int, error) { return big.NewInt(1_000_000_000), nil }
	for _, fullTx := range []bool{false, true} {
		_, err := (&blockAPI{backend: backend, store: store}).GetBlockByNumber(t.Context(), 9, fullTx)
		require.ErrorIs(t, err, readErr)
	}
	_, err := (&infoAPI{backend: backend, store: store}).FeeHistory(t.Context(), 1, 9, nil)
	require.ErrorIs(t, err, readErr)
}
