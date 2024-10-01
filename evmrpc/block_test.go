package evmrpc_test

import (
	"crypto/sha256"
	"math/big"
	"testing"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	types2 "github.com/tendermint/tendermint/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/sei-protocol/sei-chain/evmrpc"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

func TestGetBlockByHash(t *testing.T) {
	resObj := sendRequestGood(t, "getBlockByHash", "0x0000000000000000000000000000000000000000000000000000000000000001", true)
	verifyBlockResult(t, resObj)
}

func TestGetSeiBlockByHash(t *testing.T) {
	resObj := sendSeiRequestGood(t, "getBlockByHash", "0x0000000000000000000000000000000000000000000000000000000000000001", true)
	verifyBlockResult(t, resObj)
}

func TestGetBlockByNumber(t *testing.T) {
	resObjEarliest := sendSeiRequestGood(t, "getBlockByNumber", "earliest", true)
	verifyGenesisBlockResult(t, resObjEarliest)
	for _, num := range []string{"0x8", "latest", "pending", "finalized", "safe"} {
		resObj := sendRequestGood(t, "getBlockByNumber", num, true)
		verifyBlockResult(t, resObj)
	}

	resObj := sendRequestBad(t, "getBlockByNumber", "bad_num", true)
	require.Equal(t, "invalid argument 0: hex string without 0x prefix", resObj["error"].(map[string]interface{})["message"])
}

func TestGetSeiBlockByNumber(t *testing.T) {
	resObjEarliest := sendSeiRequestGood(t, "getBlockByNumber", "earliest", true)
	verifyGenesisBlockResult(t, resObjEarliest)
	for _, num := range []string{"0x8", "latest", "pending", "finalized", "safe"} {
		resObj := sendSeiRequestGood(t, "getBlockByNumber", num, true)
		verifyBlockResult(t, resObj)
	}

	resObj := sendSeiRequestBad(t, "getBlockByNumber", "bad_num", true)
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
	// Query by block height
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

	// Query by block hash
	resObj2 := sendRequestGood(t, "getBlockReceipts", "0x0000000000000000000000000000000000000000000000000000000000000002")
	result = resObj2["result"].([]interface{})
	require.Equal(t, 3, len(result))
	receipt1 = result[0].(map[string]interface{})
	require.Equal(t, "0x2", receipt1["blockNumber"])
	require.Equal(t, "0x0", receipt1["transactionIndex"])
	require.Equal(t, multiTxBlockTx1.Hash().Hex(), receipt1["transactionHash"])
	receipt2 = result[1].(map[string]interface{})
	require.Equal(t, "0x2", receipt2["blockNumber"])
	require.Equal(t, "0x1", receipt2["transactionIndex"])
	require.Equal(t, multiTxBlockTx2.Hash().Hex(), receipt2["transactionHash"])
	receipt3 = result[2].(map[string]interface{})
	require.Equal(t, "0x2", receipt3["blockNumber"])
	require.Equal(t, "0x2", receipt3["transactionIndex"])
	require.Equal(t, multiTxBlockTx3.Hash().Hex(), receipt3["transactionHash"])

	// Query by tag latest => retrieves block 8
	resObj3 := sendRequestGood(t, "getBlockReceipts", "latest")
	result = resObj3["result"].([]interface{})
	require.Equal(t, 2, len(result))
	receipt1 = result[0].(map[string]interface{})
	require.Equal(t, "0x8", receipt1["blockNumber"])
	require.Equal(t, "0x0", receipt1["transactionIndex"])
	require.Equal(t, multiTxBlockTx4.Hash().Hex(), receipt1["transactionHash"])
	receiptWithSyntheticLog := result[1].(map[string]interface{})
	require.Equal(t, "0x8", receiptWithSyntheticLog["blockNumber"])
	logs := receiptWithSyntheticLog["logs"].([]interface{})
	firstLog := logs[0].(map[string]interface{})
	topics := firstLog["topics"].([]interface{})
	syntheticLogFirstTopic := "0x0000000000000000000000000000000000000000000000000000000000000234"
	require.Equal(t, syntheticLogFirstTopic, topics[0].(string))
}

func verifyGenesisBlockResult(t *testing.T, resObj map[string]interface{}) {
	resObj = resObj["result"].(map[string]interface{})
	require.Equal(t, "0x0", resObj["baseFeePerGas"])
	require.Equal(t, "0x0", resObj["difficulty"])
	require.Equal(t, "0x", resObj["extraData"])
	require.Equal(t, "0x0", resObj["gasLimit"])
	require.Equal(t, "0x0", resObj["gasUsed"])
	require.Equal(t, "0xf9d3845df25b43b1c6926f3ceda6845c17f5624e12212fd8847d0ba01da1ab9e", resObj["hash"])
	require.Equal(t, "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000", resObj["logsBloom"])
	require.Equal(t, "0x0000000000000000000000000000000000000000", resObj["miner"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", resObj["mixHash"])
	require.Equal(t, "0x0000000000000000", resObj["nonce"])
	require.Equal(t, "0x0", resObj["number"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", resObj["parentHash"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", resObj["receiptsRoot"])
	require.Equal(t, "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347", resObj["sha3Uncles"])
	require.Equal(t, "0x0", resObj["size"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", resObj["stateRoot"])
	require.Equal(t, "0x0", resObj["timestamp"])
	require.Equal(t, []interface{}{}, resObj["transactions"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", resObj["transactionsRoot"])
	require.Equal(t, []interface{}{}, resObj["uncles"])
}

func verifyBlockResult(t *testing.T, resObj map[string]interface{}) {
	resObj = resObj["result"].(map[string]interface{})
	require.Equal(t, "0x0", resObj["difficulty"])
	require.Equal(t, "0x", resObj["extraData"])
	require.Equal(t, "0xbebc200", resObj["gasLimit"])
	require.Equal(t, "0x5", resObj["gasUsed"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", resObj["hash"])
	// see setup_tests.go, which have one transaction for block 0x8 (latest)
	require.Equal(t, "0x00002000040000000000000000000080000000200000000000000000000000080000000000000000000000000000000000000000000000000800000000000000001000000000000000000000000000000000000000000000000000000000000100000000000000000000000000200000000000000000000000000000000000000000000000000000000000000400000000000000200000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000010200000000000000", resObj["logsBloom"])
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
	require.Equal(t, "0xa", tx["gasPrice"])
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
	require.Equal(t, "0x174876e800", resObj["baseFeePerGas"])
	require.Equal(t, "0x0", resObj["totalDifficulty"])
}

func TestEncodeTmBlock_EmptyTransactions(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
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
		ConsensusParamUpdates: &types2.ConsensusParams{
			Block: &types2.BlockParams{
				MaxBytes: 100000000,
				MaxGas:   200000000,
			},
		},
	}

	// Call EncodeTmBlock with empty transactions
	result, err := evmrpc.EncodeTmBlock(ctx, block, blockRes, ethtypes.Bloom{}, k, Decoder, true, false)
	require.Nil(t, err)

	// Assert txHash is equal to ethtypes.EmptyTxsHash
	require.Equal(t, ethtypes.EmptyTxsHash, result["transactionsRoot"])
}

func TestEncodeBankMsg(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
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
		ConsensusParamUpdates: &types2.ConsensusParams{
			Block: &types2.BlockParams{
				MaxBytes: 100000000,
				MaxGas:   200000000,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(ctx, &resBlock, &resBlockRes, ethtypes.Bloom{}, k, Decoder, true, false)
	require.Nil(t, err)
	txs := res["transactions"].([]interface{})
	require.Equal(t, 0, len(txs))
}

func TestEncodeWasmExecuteMsg(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	fromSeiAddr, fromEvmAddr := testkeeper.MockAddressPair()
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(&wasmtypes.MsgExecuteContract{
		Sender:   fromSeiAddr.String(),
		Contract: toSeiAddr.String(),
		Msg:      []byte{1, 2, 3},
	})
	tx := b.GetTx()
	bz, _ := Encoder(tx)
	k.MockReceipt(ctx, sha256.Sum256(bz), &types.Receipt{
		TransactionIndex: 1,
		From:             fromEvmAddr.Hex(),
	})
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{bz},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight - 1,
			},
		},
	}
	resBlockRes := coretypes.ResultBlockResults{
		TxsResults: []*abci.ExecTxResult{
			{
				Data: bz,
			},
		},
		ConsensusParamUpdates: &types2.ConsensusParams{
			Block: &types2.BlockParams{
				MaxBytes: 100000000,
				MaxGas:   200000000,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(ctx, &resBlock, &resBlockRes, ethtypes.Bloom{}, k, Decoder, true, true)
	require.Nil(t, err)
	txs := res["transactions"].([]interface{})
	require.Equal(t, 1, len(txs))
	ti := uint64(1)
	bh := common.HexToHash(MockBlockID.Hash.String())
	to := common.Address(toSeiAddr)
	require.Equal(t, &ethapi.RPCTransaction{
		BlockHash:        &bh,
		BlockNumber:      (*hexutil.Big)(big.NewInt(MockHeight)),
		From:             fromEvmAddr,
		To:               &to,
		Input:            []byte{1, 2, 3},
		Hash:             common.Hash(sha256.Sum256(bz)),
		TransactionIndex: (*hexutil.Uint64)(&ti),
		V:                nil,
		R:                nil,
		S:                nil,
	}, txs[0].(*ethapi.RPCTransaction))
}
