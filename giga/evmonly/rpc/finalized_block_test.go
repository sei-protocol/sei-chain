package rpc

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func TestFinalizedBlockTransactionLookupsAfterRejection(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			first, rawFirst := testSignedTransactionWithNonce(t, 0)
			second, rawSecond := testSignedTransactionWithNonce(t, 1)
			rejected, rawRejected := secondSignedTransaction(t)
			chainID := big.NewInt(713700)
			chainConfig := evmonly.DefaultChainConfig()
			chainConfig.ChainID = chainID
			sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(chainID), first)
			require.NoError(t, err)
			initial := evmonly.NewMemoryState()
			initial.SetBalance(sender, big.NewInt(1_000_000_000_000_000_000))
			store := evmonly.NewMemoryStore(initial)
			receipts := evmonly.NewMemoryReceiptStore()
			executor := evmonly.NewExecutor(evmonly.Config{ChainConfig: chainConfig, OCCWorkers: workers}, evmonly.WithStore(store), evmonly.WithReceiptStore(receipts))
			defer executor.Close()
			blockCtx := evmonly.BlockContext{Number: 1, Time: 1_700_000_000, GasLimit: 1_000_000, ChainID: chainID, BaseFee: new(big.Int), BlobBaseFee: big.NewInt(1), BlockHash: common.Hash{1}}
			result, err := executor.ExecuteBlock(t.Context(), evmonly.BlockRequest{Context: blockCtx, Txs: []abevm.RawTx{rawRejected, rawFirst, rawSecond}, TxsOrdered: true})
			require.NoError(t, err)
			defer result.Release()
			require.True(t, result.Txs[0].Rejected)
			block := &abevm.EvmBlock{Height: 1, Hash: blockCtx.BlockHash, Time: time.Unix(int64(blockCtx.Time), 0)}
			byHash := map[common.Hash]abevm.RawTx{first.Hash(): rawFirst, second.Hash(): rawSecond, rejected.Hash(): rawRejected}
			it, err := receipts.IterateReceipts(1)
			require.NoError(t, err)
			for {
				next, err := it.Next()
				require.NoError(t, err)
				if !next || it.BlockNumber() != 1 {
					break
				}
				block.Txs = append(block.Txs, byHash[it.TxHash()])
			}
			require.NoError(t, it.Close())
			require.Equal(t, []abevm.RawTx{rawFirst, rawSecond}, block.Txs)
			backend := fixedGasLimitBackend(t, blockCtx.GasLimit, func(context.Context, *coretypes.RequestBlockInfo) (*abevm.EvmBlock, error) { return block, nil })
			backend.blockNumber = func() uint64 { return 1 }
			backend.chainConfig = func() *params.ChainConfig { return chainConfig }
			backend.blockContext = func(*ethtypes.Header) (vm.BlockContext, error) { return evmonly.EVMBlockContext(blockCtx), nil }
			backend.stateBeforeTx = func(ctx context.Context, _ *ethtypes.Header, txs []abevm.RawTx) (vm.SeiStateDB, func(), error) {
				view := evmonly.NewMemoryStore(initial).OpenView()
				state, err := executor.ReplayState(ctx, view, evmonly.BlockRequest{Context: blockCtx, Txs: txs, TxsOrdered: true})
				return state, view.Close, err
			}
			blocks := &blockAPI{backend: backend, store: receipts}
			txs := &txAPI{backend: backend, store: receipts}
			traces := &traceBackend{backend: backend, blocks: blocks, txs: txs}
			for i, expected := range []*ethtypes.Transaction{first, second} {
				got, err := txs.GetTransactionByHash(t.Context(), expected.Hash())
				require.NoError(t, err)
				require.Equal(t, expected.Hash(), got.Hash)
				require.Equal(t, hexutil.Uint64(i), *got.TransactionIndex)
				atIndex, err := blocks.GetTransactionByBlockNumberAndIndex(t.Context(), 1, hexutil.Uint(i))
				require.NoError(t, err)
				require.Equal(t, expected.Hash(), atIndex.Hash)
				receipt, err := txs.GetTransactionReceipt(t.Context(), expected.Hash())
				require.NoError(t, err)
				require.Equal(t, hexutil.Uint64(i), receipt["transactionIndex"])
				found, traced, _, _, index, err := traces.GetTransaction(t.Context(), expected.Hash())
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, expected.Hash(), traced.Hash())
				require.Equal(t, uint64(i), index)
			}
			missing, err := txs.GetTransactionByHash(t.Context(), rejected.Hash())
			require.NoError(t, err)
			require.Nil(t, missing)
			ethBlock, _, err := traces.BlockWithTraceMetadataByNumber(t.Context(), ethrpc.BlockNumber(1))
			require.NoError(t, err)
			require.Len(t, ethBlock.Transactions(), 2)
			traced, _, before, release, err := traces.StateAtTransaction(t.Context(), ethBlock, 1)
			require.NoError(t, err)
			defer release()
			require.Equal(t, second.Hash(), traced.Hash())
			require.Equal(t, uint64(1), before.GetNonce(sender))
		})
	}
}
