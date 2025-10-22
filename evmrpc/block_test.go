package evmrpc_test

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"sync"
	"testing"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	types2 "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/client"
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
	result, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, noopEarliestVersionFetcher, block, blockRes, k, true, false, false, nil, evmrpc.NewBlockCache(3000), &sync.Mutex{})
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
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, noopEarliestVersionFetcher, &resBlock, &resBlockRes, k, true, false, false, nil, evmrpc.NewBlockCache(3000), &sync.Mutex{})
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
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, noopEarliestVersionFetcher, &resBlock, &resBlockRes, k, true, false, true, nil, evmrpc.NewBlockCache(3000), &sync.Mutex{})
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
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, noopEarliestVersionFetcher, &resBlock, &resBlockRes, k, true, true, false, nil, evmrpc.NewBlockCache(3000), &sync.Mutex{})
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
	t.Skip()
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
	t.Skip()
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
