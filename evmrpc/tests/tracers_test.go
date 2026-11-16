package tests

import (
	"encoding/json"
	"math"
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/stretchr/testify/require"
)

func mustMarshalJSON(t *testing.T, v interface{}) string {
	t.Helper()
	bz, err := json.Marshal(v)
	require.NoError(t, err)
	return string(bz)
}

func TestTraceBlockByNumber(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	SetupTestServer(t, [][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceBlockByNumber", "0x2", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			blockHash := res["result"].([]interface{})[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["blockHash"]
			// assert that the block hash has been overwritten instead of the RLP hash.
			require.Equal(t, "0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe", blockHash.(string))
		},
	)
}

func TestTraceBlockByHash(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	SetupTestServer(t, [][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceBlockByHash", "0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			blockHash := res["result"].([]interface{})[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["blockHash"]
			// assert that the block hash has been overwritten instead of the RLP hash.
			require.Equal(t, "0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe", blockHash.(string))
		},
	)
}

func TestTraceHistoricalPrecompiles(t *testing.T) {
	from := getAddrWithMnemonic(mnemonic1)
	txData := jsonExtractAsBytesFromArray(0).(*ethtypes.DynamicFeeTx)
	SetupTestServer(t, [][][]byte{{}, {}, {}}, mnemonicInitializer(mnemonic1), mockUpgrade("v5.5.2", 1), mockUpgrade(app.LatestUpgrade, 3)).Run(
		func(port int) {
			args := export.TransactionArgs{
				From:     &from,
				To:       txData.To,
				Gas:      (*hexutil.Uint64)(&txData.Gas),
				GasPrice: (*hexutil.Big)(txData.GasFeeCap),
				Nonce:    (*hexutil.Uint64)(&txData.Nonce),
				Input:    (*hexutil.Bytes)(&txData.Data),
				ChainID:  (*hexutil.Big)(txData.ChainID),
			}
			bz, err := json.Marshal(args)
			require.Nil(t, err)
			res := sendRequestWithNamespace("debug", port, "traceCall", bz, "0x2", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			resultMap := res["result"].([]interface{})[0].(map[string]interface{})
			require.NotContains(t, resultMap, "error")

			res = sendRequestWithNamespace("debug", port, "traceCall", bz, "0x3", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			resultMap = res["result"].([]interface{})[0].(map[string]interface{})
			require.NotContains(t, resultMap, "error")
		},
	)
}

func TestTraceCallRunsOnStateAfterBlock(t *testing.T) {
	// Setup: blocks 2 and 3 each fund the recipient.
	recipient := common.HexToAddress("0x00000000000000000000000000000000000a0002")
	fund := func(nonce uint64, wei int64) []byte {
		return signAndEncodeTx(&ethtypes.DynamicFeeTx{
			Nonce:     nonce,
			GasFeeCap: big.NewInt(1000000000),
			Gas:       21000,
			To:        &recipient,
			Value:     big.NewInt(wei),
			ChainID:   chainId,
		}, mnemonic1)
	}
	ts := SetupTestServer(t, [][][]byte{{fund(0, 1_000_000_000_000)}, {fund(1, 2_000_000_000_000)}}, mnemonicInitializer(mnemonic1))
	ts.Run(func(port int) {
		ts.RequireTxSucceeded(t, 2, 0)
		ts.RequireTxSucceeded(t, 3, 0)
		for _, height := range []string{"0x2", "0x3"} {
			var balance string
			require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "eth_getBalance", recipient.Hex(), height), &balance))
			var prestate map[common.Address]struct {
				Balance string `json:"balance"`
			}
			require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "debug_traceCall",
				map[string]any{"from": mnemonic1Addr.Hex(), "to": recipient.Hex()}, height,
				map[string]any{"tracer": "prestateTracer"}), &prestate))

			// Verify: the call sees the state the block leaves behind, as eth_call does.
			require.Equal(t, balance, prestate[recipient].Balance, "block %s", height)
		}
	})
}

func TestTraceCallUsesBlockBaseFee(t *testing.T) {
	// Setup: blocks 2 and 3 each carry a transfer; a stub contract returns BASEFEE.
	recipient := common.HexToAddress("0x00000000000000000000000000000000000a0003")
	baseFeeReader := common.HexToAddress("0x00000000000000000000000000000000000a0004")
	fund := func(nonce uint64) []byte {
		return signAndEncodeTx(&ethtypes.DynamicFeeTx{
			Nonce:     nonce,
			GasFeeCap: big.NewInt(100000000000),
			Gas:       21000,
			To:        &recipient,
			Value:     big.NewInt(1),
			ChainID:   chainId,
		}, mnemonic1)
	}
	seedBaseFee := func(ctx sdk.Context, a *app.App) {
		a.EvmKeeper.SetNextBaseFeePerGas(ctx, sdk.NewDec(50000000000))
	}
	ts := SetupTestServer(t, [][][]byte{{fund(0)}, {fund(1)}}, mnemonicInitializer(mnemonic1), seedBaseFee)
	ts.Run(func(port int) {
		ts.RequireTxSucceeded(t, 2, 0)
		blockBaseFee := func(height string) *big.Int {
			var blk struct {
				BaseFeePerGas *hexutil.Big `json:"baseFeePerGas"`
			}
			require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "eth_getBlockByNumber", height, false), &blk))
			require.NotNil(t, blk.BaseFeePerGas, "block %s", height)
			return blk.BaseFeePerGas.ToInt()
		}
		want := blockBaseFee("0x2")
		require.NotEqual(t, want, blockBaseFee("0x3"), "base fee must change between blocks for this test to discriminate")

		args := map[string]any{
			"from":     mnemonic1Addr.Hex(),
			"to":       baseFeeReader.Hex(),
			"gasPrice": hexutil.EncodeBig(new(big.Int).Mul(want, big.NewInt(10))),
		}
		// BASEFEE PUSH1 0 MSTORE PUSH1 32 PUSH1 0 RETURN
		overrides := map[string]any{baseFeeReader.Hex(): map[string]any{"code": "0x4860005260206000f3"}}
		for name, txIndex := range map[string]any{"after block": nil, "txIndex 0": "0x0"} {
			config := map[string]any{"tracer": "callTracer", "stateOverrides": overrides}
			if txIndex != nil {
				config["txIndex"] = txIndex
			}
			var res struct {
				Output hexutil.Bytes `json:"output"`
			}
			require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "debug_traceCall", args, "0x2", config), &res))

			// Verify: the call sees the traced block's own base fee on both state paths.
			require.Equal(t, want, new(big.Int).SetBytes(res.Output), name)
		}
	})
}

func TestTraceMultipleTransactionsShouldNotHang(t *testing.T) {
	cwIter := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m" // hardcoded
	txBzList := make([][]byte, 100)
	for nonce := 1; nonce <= 100; nonce++ {
		txBzList[nonce-1] = signAndEncodeTx(sendErc20(uint64(nonce)), erc20DeployerMnemonics)
	}
	txBzList = append(txBzList, signAndEncodeTx(callWasmIter(0, cwIter), mnemonic1))
	SetupTestServer(t, [][][]byte{txBzList}, mnemonicInitializer(mnemonic1), multiCoinInitializer(mnemonic1), cwIterInitializer(mnemonic1), erc20Initializer()).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceBlockByHash", "0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			blockHash := res["result"].([]interface{})[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["blockHash"]
			// assert that the block hash has been overwritten instead of the RLP hash.
			require.Equal(t, "0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe", blockHash.(string))
		},
	)
}

func TestTraceStateAccess(t *testing.T) {
	txBz := signAndEncodeTx(send(0), mnemonic1)
	sdkTx, _ := testkeeper.EVMTestApp.GetTxConfig().TxDecoder()(txBz)
	evmTx, _ := sdkTx.GetMsgs()[0].(*types.MsgEVMTransaction).AsTransaction()
	hash := evmTx.Hash()
	SetupTestServer(t, [][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceStateAccess", hash.Hex())
			result := res["result"].(map[string]interface{})["app"].(map[string]interface{})["modules"].(map[string]interface{})
			require.Contains(t, result, "acc")
			require.Contains(t, result, "bank")
			require.Contains(t, result, "evm")
			require.Contains(t, result, "params")
			tmResult := res["result"].(map[string]interface{})["tendermint"].(map[string]interface{})["traces"].([]interface{})
			require.GreaterOrEqual(t, len(tmResult), 2)
		},
	)
}

func TestTraceTransactionProfile(t *testing.T) {
	cwIter := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m" // hardcoded
	txData := callWasmIter(0, cwIter)
	signedTx := signTxWithMnemonic(txData, mnemonic1)
	txBz := encodeEvmTx(txData, signedTx)

	SetupTestServer(t, [][][]byte{{txBz}}, mnemonicInitializer(mnemonic1), cwIterInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceTransactionProfile", signedTx.Hash().Hex(), map[string]interface{}{
				"timeout": "60s",
			})
			result := res["result"].(map[string]interface{})
			require.Contains(t, result, "trace")

			profile := result["profile"].(map[string]interface{})
			require.Greater(t, profile["totalNanos"].(float64), float64(0))
			require.Greater(t, profile["historicalDbLookupNanos"].(float64), float64(0))

			store := profile["store"].(map[string]interface{})
			modules := store["modules"].(map[string]interface{})
			foundIteratorTrace := false
			for _, module := range modules {
				moduleMap := module.(map[string]interface{})
				iterators, ok := moduleMap["iterators"].([]interface{})
				if ok && len(iterators) > 0 {
					foundIteratorTrace = true
				}
			}
			require.True(t, foundIteratorTrace, "expected at least one iterator sample in the store trace")
		},
	)
}

func TestTraceBlockWithFailureThenSuccess(t *testing.T) {
	maxUseiInWei := sdk.NewInt(math.MaxInt64).Mul(state.SdkUseiToSweiMultiplier).BigInt()
	insufficientFundsTx := signAndEncodeTx(sendAmount(0, maxUseiInWei), mnemonic1)
	successTx := signAndEncodeTx(send(1), mnemonic1)
	SetupTestServer(t, [][][]byte{{insufficientFundsTx, successTx}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceBlockByNumber", "0x2", map[string]interface{}{
				"timeout": "60s", "tracer": "flatCallTracer",
			})
			// the first tx should show a trace failure indicating insufficient funds
			require.Contains(t, res["result"].([]interface{})[0].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["error"].(string), "insufficient funds")
			// the second tx should show a trace success and a gas used of 21000 (0x5208)
			require.Equal(t, "0x5208", res["result"].([]interface{})[1].(map[string]interface{})["result"].([]interface{})[0].(map[string]interface{})["result"].(map[string]interface{})["gasUsed"])
		},
	)
}

func TestTraceBlockByNumberDefaultTracerDoesNotAbortOnFailedTx(t *testing.T) {
	maxUseiInWei := sdk.NewInt(math.MaxInt64).Mul(state.SdkUseiToSweiMultiplier).BigInt()
	insufficientFundsTx := signAndEncodeTx(sendAmount(0, maxUseiInWei), mnemonic1)
	successTx := signAndEncodeTx(send(1), mnemonic1)

	SetupTestServer(t, [][][]byte{{insufficientFundsTx, successTx}}, mnemonicInitializer(mnemonic1)).Run(
		func(port int) {
			res := sendRequestWithNamespace("debug", port, "traceBlockByNumber", "0x2", map[string]interface{}{
				"timeout": "60s",
			})

			require.NotContains(t, res, "error")
			txs := res["result"].([]interface{})
			require.Len(t, txs, 2)
			// Both txs should have per-tx entries (result or error);
			// the key assertion is that the block trace did NOT abort
			// with a top-level error.
			tx0 := txs[0].(map[string]interface{})
			require.True(t, tx0["result"] != nil || tx0["error"] != nil,
				"tx0 should have a result or error entry")
			tx1 := txs[1].(map[string]interface{})
			require.True(t, tx1["result"] != nil || tx1["error"] != nil,
				"tx1 should have a result or error entry")
		},
	)
}

func TestTraceBlockByNumberDefaultTracerMatchesTraceTransaction(t *testing.T) {
	cwIter := "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m" // hardcoded

	tx1Data := callWasmIter(0, cwIter)
	signedTx1 := signTxWithMnemonic(tx1Data, mnemonic1)
	tx1Bz := encodeEvmTx(tx1Data, signedTx1)

	tx2Data := callWasmIter(1, cwIter)
	signedTx2 := signTxWithMnemonic(tx2Data, mnemonic1)
	tx2Bz := encodeEvmTx(tx2Data, signedTx2)

	SetupTestServer(t, [][][]byte{{tx1Bz, tx2Bz}}, mnemonicInitializer(mnemonic1), cwIterInitializer(mnemonic1)).Run(
		func(port int) {
			blockRes := sendRequestWithNamespace("debug", port, "traceBlockByNumber", "0x2", map[string]interface{}{
				"timeout": "60s",
			})
			txRes := sendRequestWithNamespace("debug", port, "traceTransaction", signedTx2.Hash().Hex(), map[string]interface{}{
				"timeout": "60s",
			})

			require.NotContains(t, blockRes, "error")
			require.NotContains(t, txRes, "error")

			blockTxs := blockRes["result"].([]interface{})
			require.Len(t, blockTxs, 2)

			blockTrace := blockTxs[1].(map[string]interface{})["result"]
			txTrace := txRes["result"]
			require.JSONEq(t, mustMarshalJSON(t, txTrace), mustMarshalJSON(t, blockTrace))
		},
	)
}
