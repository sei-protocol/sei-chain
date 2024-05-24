package evmrpc_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/evmrpc"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

func TestGetBlockByHash(t *testing.T) {
	resObj := sendRequestGood(t, "getBlockByHash", "0x0000000000000000000000000000000000000000000000000000000000000001", true)
	verifyBlockResult(t, resObj)
}

func TestGetBlockByNumber(t *testing.T) {
	for _, num := range []string{"0x8", "earliest", "latest", "pending", "finalized", "safe"} {
		resObj := sendRequestGood(t, "getBlockByNumber", num, true)
		verifyBlockResult(t, resObj)
	}

	resObj := sendRequestBad(t, "getBlockByNumber", "bad_num", true)
	require.Equal(t, "invalid argument 0: hex string without 0x prefix", resObj["error"].(map[string]interface{})["message"])
}

func TestGetBlockTransactionCount(t *testing.T) {
	// get by block number
	for _, num := range []string{"0x8", "earliest", "latest", "pending", "finalized", "safe"} {
		resObj := sendRequestGood(t, "getBlockTransactionCountByNumber", num)
		require.Equal(t, "0x1", resObj["result"])
	}

	// get error returns null
	for _, num := range []string{"0x8", "earliest", "latest", "pending", "finalized", "safe", "0x0000000000000000000000000000000000000000000000000000000000000001"} {
		resObj := sendRequestBad(t, "getBlockTransactionCountByNumber", num)
		require.Nil(t, resObj["result"])
	}

	// get by hash
	resObj := sendRequestGood(t, "getBlockTransactionCountByHash", "0x0000000000000000000000000000000000000000000000000000000000000001")
	require.Equal(t, "0x1", resObj["result"])
}

func TestGetBlockReceipts(t *testing.T) {
	resObj := sendRequestGood(t, "getBlockReceipts", "0x2")
	result := resObj["result"].([]interface{})
	require.Equal(t, 3, len(result))
	receipt1 := result[0].(map[string]interface{})
	require.Equal(t, "0x2", receipt1["blockNumber"])
	require.Equal(t, "0x0", receipt1["transactionIndex"])
	require.Equal(t, multiTxBlockTx1.Hash().Hex(), receipt1["transactionHash"])
	receipt2 := result[1].(map[string]interface{})
	require.Equal(t, "0x2", receipt2["blockNumber"])
	require.Equal(t, "0x1", receipt2["transactionIndex"])
	require.Equal(t, multiTxBlockTx2.Hash().Hex(), receipt2["transactionHash"])
	receipt3 := result[2].(map[string]interface{})
	require.Equal(t, "0x2", receipt3["blockNumber"])
	require.Equal(t, "0x2", receipt3["transactionIndex"])
	require.Equal(t, multiTxBlockTx3.Hash().Hex(), receipt3["transactionHash"])

}

func verifyBlockResult(t *testing.T, resObj map[string]interface{}) {
	resObj = resObj["result"].(map[string]interface{})
	require.Equal(t, "0x0", resObj["difficulty"])
	require.Equal(t, "0x", resObj["extraData"])
	require.Equal(t, "0xbebc200", resObj["gasLimit"])
	require.Equal(t, "0x5", resObj["gasUsed"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", resObj["hash"])
	// see setup_tests.go, which have one transaction for block 0x8 (latest)
	if resObj["number"] == "0x8" {
		require.Equal(t, "0x00002000000000000000000000000000000000000000000000000000000000080000000000000000000000000000000000000000000000000800000000000000000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000200000000000000000000000000000000000000000000000000000000000000400000000000000200000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000200000000000000", resObj["logsBloom"])
	} else {
		require.Equal(t, "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000", resObj["logsBloom"])
	}
	require.Equal(t, "0x0000000000000000000000000000000000000005", resObj["miner"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", resObj["mixHash"])
	require.Equal(t, "0x0000000000000000", resObj["nonce"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000006", resObj["parentHash"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000004", resObj["receiptsRoot"])
	require.Equal(t, "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347", resObj["sha3Uncles"])
	require.Equal(t, "0x276", resObj["size"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000003", resObj["stateRoot"])
	require.Equal(t, "0x65254651", resObj["timestamp"])
	tx := resObj["transactions"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", tx["blockHash"])
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", tx["from"])
	require.Equal(t, "0x3e8", tx["gas"])
	require.Equal(t, "0x0", tx["gasPrice"])
	require.Equal(t, "0xa", tx["maxFeePerGas"])
	require.Equal(t, "0x0", tx["maxPriorityFeePerGas"])
	require.Equal(t, "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e", tx["hash"])
	require.Equal(t, "0x616263", tx["input"])
	require.Equal(t, "0x1", tx["nonce"])
	require.Equal(t, "0x0000000000000000000000000000000000010203", tx["to"])
	require.Equal(t, "0x0", tx["transactionIndex"])
	require.Equal(t, "0x3e8", tx["value"])
	require.Equal(t, "0x2", tx["type"])
	require.Equal(t, []interface{}{}, tx["accessList"])
	require.Equal(t, "0xae3f3", tx["chainId"])
	require.Equal(t, "0x0", tx["v"])
	require.Equal(t, "0xa1ac0e5b8202742e54ae7af350ed855313cc4f9861c2d75a0e541b4aff7c981e", tx["r"])
	require.Equal(t, "0x288b16881aed9640cd360403b9db1ce3961b29af0b00158311856d1446670996", tx["s"])
	require.Equal(t, "0x0", tx["yParity"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", resObj["transactionsRoot"])
	require.Equal(t, []interface{}{}, resObj["uncles"])
	require.Equal(t, "0x0", resObj["baseFeePerGas"])
	require.Equal(t, "0x0", resObj["totalDifficulty"])
}

func TestEncodeTmBlock_EmptyTransactions(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	block := &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight),
			Data:   tmtypes.Data{},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight - 1,
			},
		},
	}
	blockRes := &coretypes.ResultBlockResults{
		TxsResults: []*abci.ExecTxResult{},
	}

	// Call EncodeTmBlock with empty transactions
	result, err := evmrpc.EncodeTmBlock(ctx, block, blockRes, k, Decoder, true)
	require.Nil(t, err)

	// Assert txHash is equal to ethtypes.EmptyTxsHash
	require.Equal(t, ethtypes.EmptyTxsHash, result["transactionsRoot"])
}

func TestEncodeBankMsg(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	fromSeiAddr, _ := testkeeper.MockAddressPair()
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(banktypes.NewMsgSend(fromSeiAddr, toSeiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))))
	tx := b.GetTx()
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{func() []byte {
					bz, _ := Encoder(tx)
					return bz
				}()},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight - 1,
			},
		},
	}
	resBlockRes := coretypes.ResultBlockResults{
		TxsResults: []*abci.ExecTxResult{
			{
				Data: func() []byte {
					bz, _ := Encoder(tx)
					return bz
				}(),
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(ctx, &resBlock, &resBlockRes, k, Decoder, true)
	require.Nil(t, err)
	txs := res["transactions"].([]interface{})
	require.Equal(t, 0, len(txs))
}
