package evmrpc_test

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// TestGetBlockReceiptsFiltersBlockOnce checks that eth_getBlockReceipts decodes the block once,
// however many receipts it returns, and that each body matches eth_getTransactionReceipt's encoder.
func TestGetBlockReceiptsFiltersBlockOnce(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).
		WithBlockHeight(parityTestHeight).
		WithBlockTime(time.Unix(1700000000, 0)).
		WithClosestUpgradeName("v6.0.0")

	const txCount = 6
	const missingReceiptTx = 2
	to := common.HexToAddress("0x010203")
	var txs []tmtypes.Tx
	var receipts []*types.Receipt
	for i := range txCount {
		builder, ethTx := buildTx(ethtypes.DynamicFeeTx{
			Nonce:     uint64(2000 + i), //nolint:gosec
			GasFeeCap: big.NewInt(10),
			Gas:       1000,
			To:        &to,
			Value:     big.NewInt(1000),
			Data:      []byte(fmt.Sprintf("single-pass-%d", i)),
			ChainID:   big.NewInt(config.DefaultChainID),
		})
		bz, err := Encoder(builder.GetTx())
		require.NoError(t, err)
		txs = append(txs, bz)
		if i == missingReceiptTx {
			continue
		}
		var logs []*types.Log
		for l := range i + 1 {
			logs = append(logs, &types.Log{Address: to.Hex(), Data: []byte{byte(l)}, Index: uint32(l)}) //nolint:gosec
		}
		bloom := ethtypes.CreateBloom(&ethtypes.Receipt{})
		receipt := &types.Receipt{
			From:              "0x1234567890123456789012345678901234567890",
			To:                to.Hex(),
			TransactionIndex:  uint32(i),                //nolint:gosec
			BlockNumber:       uint64(parityTestHeight), //nolint:gosec
			TxType:            2,
			TxHashHex:         ethTx.Hash().Hex(),
			GasUsed:           21000,
			Status:            1,
			EffectiveGasPrice: 1,
			LogsBloom:         bloom[:],
			Logs:              logs,
		}
		require.NoError(t, k.MockReceipt(ctx, ethTx.Hash(), receipt))
		receipts = append(receipts, receipt)
	}

	block := &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header:     mockBlockHeader(parityTestHeight),
			Data:       tmtypes.Data{Txs: txs},
			LastCommit: &tmtypes.Commit{Height: parityTestHeight - 1},
		},
	}
	ctxProvider := func(h int64) sdk.Context {
		if h == evmrpc.LatestCtxHeight {
			return ctx
		}
		return ctx.WithBlockHeight(h)
	}
	var decodes atomic.Int64
	txConfigProvider := func(int64) client.TxConfig {
		decodes.Add(1)
		return TxConfig
	}
	cache := evmrpc.NewBlockCache(3000)
	mu := &sync.Mutex{}
	tm := &parityTxCountTMClient{block: block}
	wm := evmrpc.NewWatermarkManager(tm, ctxProvider, nil, k.ReceiptStore())
	api := evmrpc.NewBlockAPI(tm, k, ctxProvider, txConfigProvider, evmrpc.ConnectionTypeHTTP, wm, cache, mu)

	number := rpc.BlockNumber(parityTestHeight)
	got, err := api.GetBlockReceipts(context.Background(), rpc.BlockNumberOrHash{BlockNumber: &number})
	require.NoError(t, err)
	require.Equal(t, int64(1), decodes.Load())
	require.Len(t, got, len(receipts))

	var logIndex uint
	for i, mocked := range receipts {
		receipt, err := k.GetReceipt(ctx, common.HexToHash(mocked.TxHashHex))
		require.NoError(t, err)
		want, err := evmrpc.EncodeReceiptForTest(ctxProvider, func(int64) client.TxConfig { return TxConfig }, receipt, k, block, cache, mu)
		require.NoError(t, err)
		require.Equal(t, want, got[i])
		require.Equal(t, hexutil.Uint64(i), got[i]["transactionIndex"]) //nolint:gosec
		for _, log := range got[i]["logs"].([]*ethtypes.Log) {
			require.Equal(t, logIndex, log.Index)
			require.Equal(t, uint(i), log.TxIndex) //nolint:gosec
			logIndex++
		}
	}
}
