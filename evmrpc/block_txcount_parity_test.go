package evmrpc_test

import (
	"sync"
	"testing"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	types2 "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const parityTestHeight int64 = 771

// Two EVM Tendermint txs, one mock receipt: old RPC counted decoded EVM blobs; EncodeTmBlock drops
// the rest via filterTransactions / GetReceipt. For eth_*, bank/wasm are usually absent from the
// block tx list anyway (includeBankTransfers false), so this is the minimal mismatch repro.
func TestBlockTransactionCountMatchesGetBlockByNumber(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).
		WithBlockHeight(parityTestHeight).
		WithBlockTime(time.Unix(1700000000, 0)).
		WithClosestUpgradeName("v6.0.0")

	bz1, err := Encoder(Tx1)
	require.NoError(t, err)
	bz2, err := Encoder(MultiTxBlockTx1)
	require.NoError(t, err)

	msg := Tx1.GetMsgs()[0].(*types.MsgEVMTransaction)
	eth1, _ := msg.AsTransaction()
	hash1 := eth1.Hash()

	bloom := ethtypes.CreateBloom(&ethtypes.Receipt{})
	require.NoError(t, k.MockReceipt(ctx, hash1, &types.Receipt{
		From:              "0x1234567890123456789012345678901234567890",
		To:                "0x1234567890123456789012345678901234567890",
		TransactionIndex:  0,
		BlockNumber:       uint64(parityTestHeight), //nolint:gosec
		TxType:            2,
		TxHashHex:         hash1.Hex(),
		GasUsed:           21000,
		Status:            1,
		EffectiveGasPrice: 1,
		LogsBloom:         bloom[:],
	}))

	block := &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(parityTestHeight),
			Data:   tmtypes.Data{Txs: []tmtypes.Tx{bz1, bz2}},
			LastCommit: &tmtypes.Commit{
				Height: parityTestHeight - 1,
			},
		},
	}
	blockRes := &coretypes.ResultBlockResults{
		TxsResults: []*abci.ExecTxResult{{Data: bz1}, {Data: bz2}},
		ConsensusParamUpdates: &types2.ConsensusParams{
			Block: &types2.BlockParams{
				MaxBytes: 100000000,
				MaxGas:   200000000,
			},
		},
	}

	ctxProvider := func(h int64) sdk.Context {
		if h == evmrpc.LatestCtxHeight {
			return ctx
		}
		return ctx.WithBlockHeight(h)
	}
	txConfigProvider := func(int64) client.TxConfig { return TxConfig }
	cache := evmrpc.NewBlockCache(3000)
	mu := &sync.Mutex{}

	decodeOnly := countDecodeOnlyEvmTxs(block.Block.Txs, TxConfig.TxDecoder())
	require.Equal(t, 2, decodeOnly)

	encoded, err := evmrpc.EncodeTmBlock(ctxProvider, txConfigProvider, block, blockRes, k, false, false, false, nil, cache, mu)
	require.NoError(t, err)
	list := encoded["transactions"].([]interface{})

	strict := evmrpc.CountEncodeTmBlockVisibleTransactions(ctxProvider, txConfigProvider, block, k, false, false, mu, cache)
	require.Equal(t, len(list), strict)
	require.Greater(t, decodeOnly, len(list))
}

func countDecodeOnlyEvmTxs(txs tmtypes.Txs, dec sdk.TxDecoder) int {
	n := 0
	for _, tx := range txs {
		decoded, err := dec(tx)
		if err != nil || len(decoded.GetMsgs()) != 1 {
			continue
		}
		evmTx, ok := decoded.GetMsgs()[0].(*types.MsgEVMTransaction)
		if !ok || evmTx.IsAssociateTx() {
			continue
		}
		n++
	}
	return n
}
