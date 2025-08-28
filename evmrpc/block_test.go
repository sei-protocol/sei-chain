package evmrpc_test

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	types2 "github.com/tendermint/tendermint/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
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
	// true for eth namespace
	verifyBlockResultWithBloom(t, resObj, true)
}

func TestGetSeiBlockByHash(t *testing.T) {
	resObj := sendSeiRequestGood(t, "getBlockByHash", "0x0000000000000000000000000000000000000000000000000000000000000001", true)
	// false for sei namespace
	verifyBlockResultWithBloom(t, resObj, false)
}

func TestGetSeiBlockByNumberExcludeTraceFail(t *testing.T) {
	resObj := sendSeiRequestGood(t, "getBlockByNumberExcludeTraceFail", "0x67", true)
	// first tx is not a panic tx, second tx is a panic tx, third tx is a synthetic tx
	expectedNumTxs := 1
	require.Equal(t, expectedNumTxs, len(resObj["result"].(map[string]interface{})["transactions"].([]interface{})))
}

func TestGetBlockByNumber(t *testing.T) {
	resObjEarliest := sendSeiRequestGood(t, "getBlockByNumber", "earliest", true)
	verifyGenesisBlockResult(t, resObjEarliest)
	for _, num := range []string{"0x8", "latest", "pending", "finalized", "safe"} {
		resObj := sendRequestGood(t, "getBlockByNumber", num, true)
		// true for eth namespace
		verifyBlockResultWithBloom(t, resObj, true)
	}

	resObj := sendRequestBad(t, "getBlockByNumber", "bad_num", true)
	require.Equal(t, "invalid argument 0: hex string without 0x prefix", resObj["error"].(map[string]interface{})["message"])
}

func TestGetSeiBlockByNumber(t *testing.T) {
	resObjEarliest := sendSeiRequestGood(t, "getBlockByNumber", "earliest", true)
	verifyGenesisBlockResult(t, resObjEarliest)
	for _, num := range []string{"0x8", "latest", "pending", "finalized", "safe"} {
		resObj := sendSeiRequestGood(t, "getBlockByNumber", num, true)
		// false for sei namespace
		verifyBlockResultWithBloom(t, resObj, false)
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
	require.Equal(t, multiTxBlockTx1.Hash().Hex(), receipt1["transactionHash"])
	receipt2 := result[1].(map[string]interface{})
	require.Equal(t, "0x2", receipt2["blockNumber"])
	require.Equal(t, multiTxBlockTx2.Hash().Hex(), receipt2["transactionHash"])
	receipt3 := result[2].(map[string]interface{})
	require.Equal(t, "0x2", receipt3["blockNumber"])
	require.Equal(t, multiTxBlockTx3.Hash().Hex(), receipt3["transactionHash"])

	resObjSei := sendSeiRequestGood(t, "getBlockReceipts", "0x2")
	result = resObjSei["result"].([]interface{})
	require.Equal(t, 5, len(result))

	// Query by block hash
	resObj2 := sendRequestGood(t, "getBlockReceipts", MultiTxBlockHash)
	result = resObj2["result"].([]interface{})
	require.Equal(t, 3, len(result))
	receipt1 = result[0].(map[string]interface{})
	require.Equal(t, "0x2", receipt1["blockNumber"])
	require.Equal(t, multiTxBlockTx1.Hash().Hex(), receipt1["transactionHash"])
	receipt2 = result[1].(map[string]interface{})
	require.Equal(t, "0x2", receipt2["blockNumber"])
	require.Equal(t, multiTxBlockTx2.Hash().Hex(), receipt2["transactionHash"])
	receipt3 = result[2].(map[string]interface{})
	require.Equal(t, "0x2", receipt3["blockNumber"])
	require.Equal(t, multiTxBlockTx3.Hash().Hex(), receipt3["transactionHash"])

	// Query by tag latest => retrieves block 8
	resObj3 := sendRequestGood(t, "getBlockReceipts", "latest")
	result = resObj3["result"].([]interface{})
	require.Equal(t, 1, len(result))
	receipt1 = result[0].(map[string]interface{})
	require.Equal(t, "0x8", receipt1["blockNumber"])
	require.Equal(t, tx1.Hash().Hex(), receipt1["transactionHash"])
}

func verifyGenesisBlockResult(t *testing.T, resObj map[string]interface{}) {
	resObj = resObj["result"].(map[string]interface{})
	require.Equal(t, "0x0", resObj["baseFeePerGas"])
	require.Equal(t, "0x0", resObj["difficulty"])
	require.Equal(t, "0x", resObj["extraData"])
	require.Equal(t, "0x0", resObj["gasLimit"])
	require.Equal(t, "0x0", resObj["gasUsed"])
	require.Equal(t, "0xF9D3845DF25B43B1C6926F3CEDA6845C17F5624E12212FD8847D0BA01DA1AB9E", resObj["hash"])
	require.Equal(t, "0x0000000000000000", resObj["nonce"])
	require.Equal(t, "0x0", resObj["number"])
}

func verifyBlockResult(t *testing.T, resObj map[string]interface{}) {
	// false means don't verify logBloom
	verifyBlockResultCommon(t, resObj, false)
}

func verifyBlockResultWithBloom(t *testing.T, resObj map[string]interface{}, isEthNamespace bool) {
	resObj = resObj["result"].(map[string]interface{})
	emptyBloom := "0x" + common.Bytes2Hex(ethtypes.Bloom{}.Bytes())

	if isEthNamespace {
		// Eth namespace now returns EVM-only bloom where present - should be non-empty
		require.NotEqual(t, emptyBloom, resObj["logsBloom"])
	} else {
		require.Equal(t, "0x00002000040000000000000000000080000000200000000000002000000000080000000000000000000000000000000000000000000000000800000000000000001000000000000000000000000000000000020000000000000000000000000100000000000000002000000000200000000000000000000000000000000000100000000000000000000000000400000000000000200000000000000000000000000000000000000100000000000000020000200000000000000000002000000000000000000000000000000000000000000000000000000000000000000200000000010000000002000000000000000000000000000000010200000000000000", resObj["logsBloom"])
	}
}

func verifyBlockResultCommon(t *testing.T, resObj map[string]interface{}, verifyBloom bool) {
	resObj = resObj["result"].(map[string]interface{})
	require.Equal(t, "0x0", resObj["difficulty"])
	require.Equal(t, "0x", resObj["extraData"])
	require.Equal(t, "0xbebc200", resObj["gasLimit"])
	require.Equal(t, "0x5", resObj["gasUsed"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", resObj["hash"])
	require.Equal(t, "0x0000000000000000000000000000000000000005", resObj["miner"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", resObj["mixHash"])
	require.Equal(t, "0x0000000000000000", resObj["nonce"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000006", resObj["parentHash"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000004", resObj["receiptsRoot"])
	require.Equal(t, "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347", resObj["sha3Uncles"])
	require.Equal(t, "0x279", resObj["size"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000003", resObj["stateRoot"])
	require.Equal(t, "0x65254651", resObj["timestamp"])
	tx := resObj["transactions"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", tx["blockHash"])
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", tx["from"])
	require.Equal(t, "0x3e8", tx["gas"])
	require.Equal(t, "0xa", tx["gasPrice"])
	require.Equal(t, "0xa", tx["maxFeePerGas"])
	require.Equal(t, "0x0", tx["maxPriorityFeePerGas"])
	require.Equal(t, "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32", tx["hash"])
	require.Equal(t, "0x616263", tx["input"])
	require.Equal(t, "0x1", tx["nonce"])
	require.Equal(t, "0x0000000000000000000000000000000000010203", tx["to"])
	require.Equal(t, "0x0", tx["transactionIndex"])
	require.Equal(t, "0x3e8", tx["value"])
	require.Equal(t, "0x2", tx["type"])
	require.Equal(t, []interface{}{}, tx["accessList"])
	require.Equal(t, "0xae3f2", tx["chainId"])
	require.Equal(t, "0x1", tx["v"])
	require.Equal(t, "0x2d9ec6f4c4ff4ab0ca8de6248f939e873d2aa9cb6156fa9368e34708dfb6c123", tx["r"])
	require.Equal(t, "0x35990bec00913db3cecd7f132b45c289280f4182751dab1c9c5ca609939319cb", tx["s"])
	require.Equal(t, "0x1", tx["yParity"])
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", resObj["transactionsRoot"])
	require.Equal(t, []interface{}{}, resObj["uncles"])
	require.Equal(t, "0x3b9aca00", resObj["baseFeePerGas"])
	require.Equal(t, "0x0", resObj["totalDifficulty"])
}

func TestEncodeTmBlock_EmptyTransactions(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	block := &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data:   tmtypes.Data{},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
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
	result, err := evmrpc.EncodeTmBlock(ctx, ctx, block, blockRes, ethtypes.Bloom{}, k, Decoder, true, false, false, nil)
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
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{func() []byte {
					bz, _ := Encoder(tx)
					return bz
				}()},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
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
	res, err := evmrpc.EncodeTmBlock(ctx, ctx, &resBlock, &resBlockRes, ethtypes.Bloom{}, k, Decoder, true, false, false, nil)
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
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{bz},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
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
	res, err := evmrpc.EncodeTmBlock(ctx, ctx, &resBlock, &resBlockRes, ethtypes.Bloom{}, k, Decoder, true, false, true, nil)
	require.Nil(t, err)
	txs := res["transactions"].([]interface{})
	require.Equal(t, 1, len(txs))
	ti := uint64(0)
	bh := common.HexToHash(MockBlockID.Hash.String())
	to := common.Address(toSeiAddr)
	require.Equal(t, &export.RPCTransaction{
		BlockHash:        &bh,
		BlockNumber:      (*hexutil.Big)(big.NewInt(MockHeight8)),
		From:             fromEvmAddr,
		To:               &to,
		Input:            []byte{1, 2, 3},
		Hash:             common.Hash(sha256.Sum256(bz)),
		TransactionIndex: (*hexutil.Uint64)(&ti),
		V:                nil,
		R:                nil,
		S:                nil,
	}, txs[0].(*export.RPCTransaction))
}

func TestEncodeBankTransferMsg(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	fromSeiAddr, fromEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, fromSeiAddr, fromEvmAddr)
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(&banktypes.MsgSend{
		FromAddress: fromSeiAddr.String(),
		ToAddress:   toSeiAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.OneInt())),
	})
	ti := uint64(0)
	tx := b.GetTx()
	bz, _ := Encoder(tx)
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{bz},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
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
	res, err := evmrpc.EncodeTmBlock(ctx, ctx, &resBlock, &resBlockRes, ethtypes.Bloom{}, k, Decoder, true, true, false, nil)
	require.Nil(t, err)
	txs := res["transactions"].([]interface{})
	require.Equal(t, 1, len(txs))
	bh := common.HexToHash(MockBlockID.Hash.String())
	to := common.Address(toSeiAddr)
	require.Equal(t, &export.RPCTransaction{
		BlockHash:        &bh,
		BlockNumber:      (*hexutil.Big)(big.NewInt(MockHeight8)),
		From:             fromEvmAddr,
		To:               &to,
		Value:            (*hexutil.Big)(big.NewInt(1_000_000_000_000)),
		Hash:             common.Hash(sha256.Sum256(bz)),
		TransactionIndex: (*hexutil.Uint64)(&ti),
		V:                nil,
		R:                nil,
		S:                nil,
	}, txs[0].(*export.RPCTransaction))
}

func TestGetBlockByNumber_LogBloomBehavior(t *testing.T) {
	emptyBloom := "0x" + common.Bytes2Hex(ethtypes.Bloom{}.Bytes())

	// Eth namespace should now be NON-empty at 0x8
	resObjEth := sendRequestGood(t, "getBlockByNumber", "0x8", true)
	resultEth := resObjEth["result"].(map[string]interface{})
	require.NotEqual(t, emptyBloom, resultEth["logsBloom"])

	// Sei namespace includes synthetic logs and should also be non-empty
	resObjSei := sendSeiRequestGood(t, "getBlockByNumber", "0x8", true)
	resultSei := resObjSei["result"].(map[string]interface{})
	require.NotEqual(t, emptyBloom, resultSei["logsBloom"])
}

func TestGetBlockByHash_LogBloomBehavior(t *testing.T) {
	emptyBloom := "0x" + common.Bytes2Hex(ethtypes.Bloom{}.Bytes())
	blockHash := "0x0000000000000000000000000000000000000000000000000000000000000001"

	// Eth: now non-empty
	resObjEth := sendRequestGood(t, "getBlockByHash", blockHash, true)
	resultEth := resObjEth["result"].(map[string]interface{})
	require.NotEqual(t, emptyBloom, resultEth["logsBloom"])

	// Sei: also non-empty (all logs)
	resObjSei := sendSeiRequestGood(t, "getBlockByHash", blockHash, true)
	resultSei := resObjSei["result"].(map[string]interface{})
	require.NotEqual(t, emptyBloom, resultSei["logsBloom"])
}

func TestEthBloom_NonEmptyWhenEvmLogsPresent(t *testing.T) {
	emptyBloom := "0x" + common.Bytes2Hex(ethtypes.Bloom{}.Bytes())

	// Non-empty at 0x8 (EVM-only bloom has been set in setupLogs)
	resObjNonEmpty := sendRequestGood(t, "getBlockByNumber", "0x8", true)
	resultNonEmpty := resObjNonEmpty["result"].(map[string]interface{})
	require.NotEqual(t, emptyBloom, resultNonEmpty["logsBloom"], "eth logsBloom should be non-empty when EVM logs exist")

	// Empty at genesis (earliest)
	resObjEmpty := sendRequestGood(t, "getBlockByNumber", "earliest", true)
	resultEmpty := resObjEmpty["result"].(map[string]interface{})
	require.Equal(t, emptyBloom, resultEmpty["logsBloom"], "eth logsBloom should be empty at genesis")
}

func mustParseBloomHex(t *testing.T, hexStr interface{}) ethtypes.Bloom {
	s := hexStr.(string)
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	require.NoError(t, err)
	var bloom ethtypes.Bloom
	copy(bloom[:], b)
	return bloom
}

func TestEthBloom_ExcludesSyntheticTopics(t *testing.T) {
	// Synthetic-only topic from setup
	syntheticTopic := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000234")
	// Topic present in real EVM logs
	evmTopic := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123")

	// eth_
	resEth := sendRequestGood(t, "getBlockByNumber", "0x8", true)
	bloomEth := mustParseBloomHex(t, resEth["result"].(map[string]interface{})["logsBloom"])
	require.False(t, ethtypes.BloomLookup(bloomEth, syntheticTopic), "eth bloom should exclude synthetic")
	require.True(t, ethtypes.BloomLookup(bloomEth, evmTopic), "eth bloom should include real EVM topic")

	// sei_
	resSei := sendSeiRequestGood(t, "getBlockByNumber", "0x8", true)
	bloomSei := mustParseBloomHex(t, resSei["result"].(map[string]interface{})["logsBloom"])
	require.True(t, ethtypes.BloomLookup(bloomSei, syntheticTopic), "sei bloom should include synthetic")
	require.True(t, ethtypes.BloomLookup(bloomSei, evmTopic), "sei bloom should include real EVM topic")
}

func TestGetLogs_SyntheticTopic_EthVsSei(t *testing.T) {
	// Synthetic-only topic
	synthTopic := common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000234")

	// Query a small range that has bloom + synthetic activity
	crit := map[string]interface{}{
		"fromBlock": "0x8",
		"toBlock":   "0x8",
		"topics":    [][]common.Hash{{synthTopic}},
	}

	// eth_: should exclude synthetic logs completely
	resEth := sendRequestGood(t, "getLogs", crit)
	logsEth, ok := resEth["result"].([]interface{})
	require.True(t, ok, "eth_getLogs: result should be an array")
	require.Equal(t, 0, len(logsEth), "eth_getLogs should NOT return synthetic-only logs")

	// sei_: should include synthetic logs
	resSei := sendSeiRequestGood(t, "getLogs", crit)
	logsSei, ok := resSei["result"].([]interface{})
	require.True(t, ok, "sei_getLogs: result should be an array")
	require.GreaterOrEqual(t, len(logsSei), 1, "sei_getLogs should include synthetic logs")

	first := logsSei[0].(map[string]interface{})
	topics := first["topics"].([]interface{})
	require.Equal(t, synthTopic.Hex(), topics[0].(string))
}
