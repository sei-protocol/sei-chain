package tests

// Behavior tests pinning debug_trace* tracer output over JSON-RPC.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/stretchr/testify/require"
)

// behaviorMnemonic is the Anvil test mnemonic; the Sei HD path yields behaviorAddr, not Anvil account 0.
const behaviorMnemonic = "test test test test test test test test test test test junk"

var (
	behaviorAddr  = common.HexToAddress("0xF5a6EAD936fb47f342Bb63E676479bDdf26EbE1d")
	mnemonic1Addr = common.HexToAddress("0x5B4eba929F3811980f5AE0c5D04fa200f837DF4E")

	jsonPrecompileAddr = common.HexToAddress("0x0000000000000000000000000000000000001003")
	sha256BuiltinAddr  = common.HexToAddress("0x0000000000000000000000000000000000000002")

	jsonProxyAddr   = common.HexToAddress("0x00000000000000000000000000000000000b0001") // -> Sei JSON precompile
	sha256ProxyAddr = common.HexToAddress("0x00000000000000000000000000000000000b0002") // -> builtin sha256
	outerProxyAddr  = common.HexToAddress("0x00000000000000000000000000000000000b0003") // -> jsonProxyAddr

	behaviorRecipient = common.HexToAddress("0x00000000000000000000000000000000000a0001")

	// mockedBlockHash is the hash the harness assigns to block 2.
	mockedBlockHash = "0x6f2168eb453152b1f68874fe32cea6fcb199bfd63836acb72a8eb33e666613fe"
)

// mnemonic1AcctNum is mnemonic1's cosmos account number when it is the first initializer.
const mnemonic1AcctNum = 5

// rpcResult is a JSON-RPC response with the raw result.
type rpcResult struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// callRPC sends a single JSON-RPC request with JSON-marshalled params.
func callRPC(t *testing.T, port int, method string, params ...any) rpcResult {
	t.Helper()
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	require.NoError(t, err)
	res, err := http.Post(fmt.Sprintf("http://%s:%d", testAddr, port), "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	bz, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	var out rpcResult
	require.NoError(t, json.Unmarshal(bz, &out), string(bz))
	return out
}

func requireRPCResult(t *testing.T, port int, method string, params ...any) json.RawMessage {
	t.Helper()
	r := callRPC(t, port, method, params...)
	require.Nil(t, r.Error, "unexpected error for %s: %+v", method, r.Error)
	return r.Result
}

// proxyCode returns runtime code that forwards calldata to target and returns its output.
func proxyCode(target common.Address) []byte {
	code := []byte{
		0x36, 0x60, 0x00, 0x60, 0x00, 0x37, // CALLDATACOPY(0, 0, CALLDATASIZE)
		0x60, 0x00, 0x60, 0x00, 0x36, 0x60, 0x00, 0x60, 0x00, // retSize retOffset argsSize argsOffset value
		0x73, // PUSH20 target
	}
	code = append(code, target.Bytes()...)
	return append(code,
		0x5a, 0xf1, 0x50, // GAS CALL POP
		0x3d, 0x60, 0x00, 0x60, 0x00, 0x3e, // RETURNDATACOPY(0, 0, RETURNDATASIZE)
		0x3d, 0x60, 0x00, 0xf3, // RETURN(0, RETURNDATASIZE)
	)
}

func behaviorProxyInitializer() func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		a.EvmKeeper.SetCode(ctx, jsonProxyAddr, proxyCode(jsonPrecompileAddr))
		a.EvmKeeper.SetCode(ctx, sha256ProxyAddr, proxyCode(sha256BuiltinAddr))
		a.EvmKeeper.SetCode(ctx, outerProxyAddr, proxyCode(jsonProxyAddr))
	}
}

// jsonExtractInput is calldata for JSON precompile extractAsBytesFromArray(`["1"]`, 0).
func jsonExtractInput(t *testing.T) []byte {
	t.Helper()
	abiBz, err := os.ReadFile("../../precompiles/json/abi.json")
	require.NoError(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.NoError(t, err)
	input, err := newAbi.Pack("extractAsBytesFromArray", []byte("[\"1\"]"), uint16(0))
	require.NoError(t, err)
	return input
}

// jsonExtractOutput is the ABI-encoded bytes("1") returned by jsonExtractInput.
const jsonExtractOutput = "0x000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000013100000000000000000000000000000000000000000000000000000000000000"

// sha256("hello")
const sha256HelloOutput = "0x2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func behaviorCallTx(nonce uint64, to common.Address, data []byte) ethtypes.TxData {
	return &ethtypes.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       200000,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      data,
		ChainID:   chainId,
	}
}

func behaviorSendTx(nonce uint64, to common.Address) ethtypes.TxData {
	return &ethtypes.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(2000),
		Data:      []byte{},
		ChainID:   chainId,
	}
}

// mixedBlock is block 2 of the mixed cosmos/EVM fixture:
//
//	idx 0: cosmos bank send 1_000_000usei mnemonic1 -> behaviorMnemonic's sei address
//	idx 1: EVM send of 2000wei from behaviorAddr (only funded by idx 0)
//	idx 2: EVM call mnemonic1 -> jsonProxyAddr -> Sei JSON precompile
//	idx 3: EVM call mnemonic1 -> sha256ProxyAddr -> builtin sha256 precompile
type mixedBlock struct {
	block              [][]byte
	sendTx, jsonTx, sh *ethtypes.Transaction
}

func buildMixedBlock(t *testing.T) mixedBlock {
	cosmosTx := signAndEncodeCosmosTx(&banktypes.MsgSend{
		FromAddress: getSeiAddrWithMnemonic(mnemonic1).String(),
		ToAddress:   getSeiAddrWithMnemonic(behaviorMnemonic).String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000))),
	}, mnemonic1, mnemonic1AcctNum, 0)
	d1 := behaviorSendTx(0, behaviorRecipient)
	tx1 := signTxWithMnemonic(d1, behaviorMnemonic)
	d2 := behaviorCallTx(0, jsonProxyAddr, jsonExtractInput(t))
	tx2 := signTxWithMnemonic(d2, mnemonic1)
	d3 := behaviorCallTx(1, sha256ProxyAddr, []byte("hello"))
	tx3 := signTxWithMnemonic(d3, mnemonic1)
	return mixedBlock{
		block:  [][]byte{cosmosTx, encodeEvmTx(d1, tx1), encodeEvmTx(d2, tx2), encodeEvmTx(d3, tx3)},
		sendTx: tx1, jsonTx: tx2, sh: tx3,
	}
}

func setupMixedBlockServer(t *testing.T) (TestServer, mixedBlock) {
	mb := buildMixedBlock(t)
	ts := SetupTestServer(t, [][][]byte{mb.block}, mnemonicInitializer(mnemonic1), behaviorProxyInitializer())
	return ts, mb
}

// expectedCallFrames returns callTracer frames for the mixed block's EVM txs.
func expectedCallFrames(t *testing.T, mb mixedBlock) (sendFrame, jsonFrame, shaFrame string) {
	in := hexutil.Encode(jsonExtractInput(t))
	sendFrame = fmt.Sprintf(`{"from":"%s","gas":"0x5208","gasUsed":"0x5208","to":"%s","input":"0x","value":"0x7d0","type":"CALL"}`,
		strings.ToLower(behaviorAddr.Hex()), strings.ToLower(behaviorRecipient.Hex()))
	jsonFrame = fmt.Sprintf(`{"from":"%s","gas":"0x30d40","gasUsed":"0x6683","to":"%s","input":"%s","output":"%s",
		"calls":[{"from":"%s","gas":"0x2a387","gasUsed":"0x768","to":"%s","input":"%s","output":"%s","value":"0x0","type":"CALL"}],
		"value":"0x0","type":"CALL"}`,
		strings.ToLower(mnemonic1Addr.Hex()), strings.ToLower(jsonProxyAddr.Hex()), in, jsonExtractOutput,
		strings.ToLower(jsonProxyAddr.Hex()), strings.ToLower(jsonPrecompileAddr.Hex()), in, jsonExtractOutput)
	shaFrame = fmt.Sprintf(`{"from":"%s","gas":"0x30d40","gasUsed":"0x533d","to":"%s","input":"0x68656c6c6f","output":"%s",
		"calls":[{"from":"%s","gas":"0x2af77","gasUsed":"0x48","to":"%s","input":"0x68656c6c6f","output":"%s","value":"0x0","type":"CALL"}],
		"value":"0x0","type":"CALL"}`,
		strings.ToLower(mnemonic1Addr.Hex()), strings.ToLower(sha256ProxyAddr.Hex()), sha256HelloOutput,
		strings.ToLower(sha256ProxyAddr.Hex()), strings.ToLower(sha256BuiltinAddr.Hex()), sha256HelloOutput)
	return
}

// debug_traceBlockBy{Number,Hash} on a mixed cosmos/EVM block traces only EVM txs.
func TestBehaviorTraceBlockMixedCosmosAndEVM(t *testing.T) {
	ts, mb := setupMixedBlockServer(t)
	ts.Run(func(port int) {
		// every tx (cosmos and EVM) committed successfully
		for i := 0; i < 4; i++ {
			ts.RequireTxSucceeded(t, 2, i)
		}
		require.Equal(t, behaviorAddr, getAddrWithMnemonic(behaviorMnemonic))
		require.Equal(t, mnemonic1Addr, getAddrWithMnemonic(mnemonic1))

		sendFrame, jsonFrame, shaFrame := expectedCallFrames(t, mb)
		expected := fmt.Sprintf(`[{"txHash":"%s","result":%s},{"txHash":"%s","result":%s},{"txHash":"%s","result":%s}]`,
			mb.sendTx.Hash().Hex(), sendFrame, mb.jsonTx.Hash().Hex(), jsonFrame, mb.sh.Hash().Hex(), shaFrame)

		// only the 3 EVM txs, in block order
		byNumber := requireRPCResult(t, port, "debug_traceBlockByNumber", "0x2", map[string]any{"tracer": "callTracer"})
		require.JSONEq(t, expected, string(byNumber))
		byHash := requireRPCResult(t, port, "debug_traceBlockByHash", mockedBlockHash, map[string]any{"tracer": "callTracer"})
		require.JSONEq(t, expected, string(byHash))

		// eth_getBlockByNumber lists the same hashes
		var blk struct {
			Transactions []string `json:"transactions"`
		}
		require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "eth_getBlockByNumber", "0x2", false), &blk))
		require.Equal(t, []string{mb.sendTx.Hash().Hex(), mb.jsonTx.Hash().Hex(), mb.sh.Hash().Hex()}, blk.Transactions)

		// receipts use the EVM-only index
		for i, tx := range []*ethtypes.Transaction{mb.sendTx, mb.jsonTx, mb.sh} {
			var rcpt struct {
				TransactionIndex string `json:"transactionIndex"`
				Status           string `json:"status"`
			}
			require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "eth_getTransactionReceipt", tx.Hash().Hex()), &rcpt))
			require.Equal(t, hexutil.EncodeUint64(uint64(i)), rcpt.TransactionIndex)
			require.Equal(t, "0x1", rcpt.Status)
		}

		// behaviorAddr prestate is the 1_000_000usei (1e18 wei) from the cosmos tx
		var pre []struct {
			TxHash string                     `json:"txHash"`
			Result map[string]json.RawMessage `json:"result"`
		}
		require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "debug_traceBlockByNumber", "0x2", map[string]any{"tracer": "prestateTracer"}), &pre))
		require.Len(t, pre, 3)
		require.Equal(t, mb.sendTx.Hash().Hex(), pre[0].TxHash)
		require.JSONEq(t, `{"balance":"0xde0b6b3a7640000"}`, string(pre[0].Result[strings.ToLower(behaviorAddr.Hex())]))
	})
}

// flatCallTracer transactionPosition for block vs single-tx tracing in a mixed block.
func TestBehaviorFlatCallTracerTransactionPositionMixedBlock(t *testing.T) {
	ts, mb := setupMixedBlockServer(t)
	ts.Run(func(port int) {
		type flatFrame struct {
			TransactionHash     string `json:"transactionHash"`
			TransactionPosition int    `json:"transactionPosition"`
			BlockNumber         int    `json:"blockNumber"`
			BlockHash           string `json:"blockHash"`
			Subtraces           int    `json:"subtraces"`
		}
		var blockRes []struct {
			TxHash string      `json:"txHash"`
			Result []flatFrame `json:"result"`
		}
		require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "debug_traceBlockByNumber", "0x2", map[string]any{"tracer": "flatCallTracer"}), &blockRes))
		require.Len(t, blockRes, 3)
		txs := []*ethtypes.Transaction{mb.sendTx, mb.jsonTx, mb.sh}
		for i, tx := range txs {
			require.Equal(t, tx.Hash().Hex(), blockRes[i].TxHash)
			require.Len(t, blockRes[i].Result, 1)
			require.Equal(t, tx.Hash().Hex(), blockRes[i].Result[0].TransactionHash)
			require.Equal(t, mockedBlockHash, blockRes[i].Result[0].BlockHash)
			require.Equal(t, 2, blockRes[i].Result[0].BlockNumber)
			// EVM-only index
			require.Equal(t, i, blockRes[i].Result[0].TransactionPosition)
		}
		for i, tx := range txs {
			var single []flatFrame
			require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "debug_traceTransaction", tx.Hash().Hex(), map[string]any{"tracer": "flatCallTracer"}), &single))
			require.Len(t, single, 1)
			// possible bug: raw Tendermint tx index, unlike block tracing and receipts
			require.Equal(t, i+1, single[0].TransactionPosition)
		}
	})
}

// Tracer output for calls into Sei and builtin precompiles.
func TestBehaviorTracersPrecompileFrames(t *testing.T) {
	ts, mb := setupMixedBlockServer(t)
	ts.Run(func(port int) {
		_, jsonFrame, shaFrame := expectedCallFrames(t, mb)

		// callTracer keeps nested precompile frames
		res := requireRPCResult(t, port, "debug_traceTransaction", mb.jsonTx.Hash().Hex(), map[string]any{"tracer": "callTracer"})
		require.JSONEq(t, jsonFrame, string(res))
		res = requireRPCResult(t, port, "debug_traceTransaction", mb.sh.Hash().Hex(), map[string]any{"tracer": "callTracer"})
		require.JSONEq(t, shaFrame, string(res))

		// flatCallTracer drops nested precompile frames
		in := hexutil.Encode(jsonExtractInput(t))
		res = requireRPCResult(t, port, "debug_traceTransaction", mb.jsonTx.Hash().Hex(), map[string]any{"tracer": "flatCallTracer"})
		require.JSONEq(t, fmt.Sprintf(`[{"action":{"callType":"call","from":"%s","gas":"0x30d40","input":"%s","to":"%s","value":"0x0"},
			"blockHash":"%s","blockNumber":2,"result":{"gasUsed":"0x6683","output":"%s"},"subtraces":0,"traceAddress":[],
			"transactionHash":"%s","transactionPosition":2,"type":"call"}]`,
			strings.ToLower(mnemonic1Addr.Hex()), in, strings.ToLower(jsonProxyAddr.Hex()), mockedBlockHash, jsonExtractOutput, mb.jsonTx.Hash().Hex()), string(res))
		res = requireRPCResult(t, port, "debug_traceTransaction", mb.sh.Hash().Hex(), map[string]any{"tracer": "flatCallTracer"})
		require.JSONEq(t, fmt.Sprintf(`[{"action":{"callType":"call","from":"%s","gas":"0x30d40","input":"0x68656c6c6f","to":"%s","value":"0x0"},
			"blockHash":"%s","blockNumber":2,"result":{"gasUsed":"0x533d","output":"%s"},"subtraces":0,"traceAddress":[],
			"transactionHash":"%s","transactionPosition":3,"type":"call"}]`,
			strings.ToLower(mnemonic1Addr.Hex()), strings.ToLower(sha256ProxyAddr.Hex()), mockedBlockHash, sha256HelloOutput, mb.sh.Hash().Hex()), string(res))

		// 4byteTracer skips precompile calls
		res = requireRPCResult(t, port, "debug_traceTransaction", mb.jsonTx.Hash().Hex(), map[string]any{"tracer": "4byteTracer"})
		require.JSONEq(t, `{"0xb0bf8a47-128":1}`, string(res))
		res = requireRPCResult(t, port, "debug_traceTransaction", mb.sh.Hash().Hex(), map[string]any{"tracer": "4byteTracer"})
		require.JSONEq(t, `{"0x68656c6c-1":1}`, string(res))

		// prestateTracer omits empty accounts (incl. precompiles) and adds codeHash (go-ethereum v1.17.7)
		res = requireRPCResult(t, port, "debug_traceTransaction", mb.jsonTx.Hash().Hex(), map[string]any{"tracer": "prestateTracer"})
		require.JSONEq(t, fmt.Sprintf(`{
			"0x00000000000000000000000000000000000b0001":{"balance":"0x0","code":"%s","codeHash":"%s"},
			"0x27f7b8b8b5a4e71e8e9aa671f4e4031e3773303f":{"balance":"0x1319718a5000"},
			"%s":{"balance":"0x21dfe1f5c5363780000"}}`,
			hexutil.Encode(proxyCode(jsonPrecompileAddr)), crypto.Keccak256Hash(proxyCode(jsonPrecompileAddr)).Hex(),
			strings.ToLower(mnemonic1Addr.Hex())), string(res))

		// direct precompile calls
		from := mnemonic1Addr.Hex()
		direct := map[string]any{"from": from, "to": jsonPrecompileAddr.Hex(), "input": in, "gas": "0x30d40"}
		res = requireRPCResult(t, port, "debug_traceCall", direct, "latest", map[string]any{"tracer": "4byteTracer"})
		require.JSONEq(t, `{}`, string(res), "top-level call to a Sei precompile is skipped by 4byteTracer")
		res = requireRPCResult(t, port, "debug_traceCall", direct, "latest", map[string]any{"tracer": "callTracer"})
		require.JSONEq(t, fmt.Sprintf(`{"from":"%s","gas":"0x30d40","gasUsed":"0x5c04","to":"%s","input":"%s","output":"%s","value":"0x0","type":"CALL"}`,
			strings.ToLower(from), strings.ToLower(jsonPrecompileAddr.Hex()), in, jsonExtractOutput), string(res))
		res = requireRPCResult(t, port, "debug_traceCall", direct, "latest", map[string]any{"tracer": "flatCallTracer"})
		require.JSONEq(t, fmt.Sprintf(`[{"action":{"callType":"call","from":"%s","gas":"0x30d40","input":"%s","to":"%s","value":"0x0"},
			"blockHash":null,"blockNumber":0,"result":{"gasUsed":"0x5c04","output":"%s"},"subtraces":0,"traceAddress":[],
			"transactionHash":null,"transactionPosition":0,"type":"call"}]`,
			strings.ToLower(from), in, strings.ToLower(jsonPrecompileAddr.Hex()), jsonExtractOutput), string(res))
		res = requireRPCResult(t, port, "debug_traceCall",
			map[string]any{"from": from, "to": sha256BuiltinAddr.Hex(), "input": "0x68656c6c6f", "gas": "0x30d40"},
			"latest", map[string]any{"tracer": "flatCallTracer"})
		require.JSONEq(t, fmt.Sprintf(`[{"action":{"callType":"call","from":"%s","gas":"0x30d40","input":"0x68656c6c6f","to":"%s","value":"0x0"},
			"blockHash":null,"blockNumber":0,"result":{"gasUsed":"0x52d0","output":"%s"},"subtraces":0,"traceAddress":[],
			"transactionHash":null,"transactionPosition":0,"type":"call"}]`,
			strings.ToLower(from), strings.ToLower(sha256BuiltinAddr.Hex()), sha256HelloOutput), string(res))
	})
}

// debug_traceCall applies code and balance stateOverrides.
func TestBehaviorTraceCallStateOverrides(t *testing.T) {
	ts, _ := setupMixedBlockServer(t)
	ts.Run(func(port int) {
		in := hexutil.Encode(jsonExtractInput(t))
		from := strings.ToLower(mnemonic1Addr.Hex())

		// code override: proxy -> Sei JSON precompile
		ov := common.HexToAddress("0x00000000000000000000000000000000000c0001")
		ovLower := strings.ToLower(ov.Hex())
		args := map[string]any{"from": mnemonic1Addr.Hex(), "to": ov.Hex(), "input": in, "gas": "0x30d40"}
		overrides := map[string]any{ov.Hex(): map[string]any{"code": hexutil.Encode(proxyCode(jsonPrecompileAddr))}}

		res := requireRPCResult(t, port, "debug_traceCall", args, "latest", map[string]any{"tracer": "callTracer", "stateOverrides": overrides})
		require.JSONEq(t, fmt.Sprintf(`{"from":"%s","gas":"0x30d40","gasUsed":"0x6683","to":"%s","input":"%s","output":"%s",
			"calls":[{"from":"%s","gas":"0x2a387","gasUsed":"0x768","to":"0x0000000000000000000000000000000000001003","input":"%s","output":"%s","value":"0x0","type":"CALL"}],
			"value":"0x0","type":"CALL"}`, from, ovLower, in, jsonExtractOutput, ovLower, in, jsonExtractOutput), string(res))

		res = requireRPCResult(t, port, "debug_traceCall", args, "latest", map[string]any{"tracer": "flatCallTracer", "stateOverrides": overrides})
		require.JSONEq(t, fmt.Sprintf(`[{"action":{"callType":"call","from":"%s","gas":"0x30d40","input":"%s","to":"%s","value":"0x0"},
			"blockHash":null,"blockNumber":0,"result":{"gasUsed":"0x6683","output":"%s"},"subtraces":0,"traceAddress":[],
			"transactionHash":null,"transactionPosition":0,"type":"call"}]`, from, in, ovLower, jsonExtractOutput), string(res))

		// struct logger with the override
		var structRes struct {
			Gas         uint64            `json:"gas"`
			Failed      bool              `json:"failed"`
			ReturnValue string            `json:"returnValue"`
			StructLogs  []json.RawMessage `json:"structLogs"`
		}
		require.NoError(t, json.Unmarshal(requireRPCResult(t, port, "debug_traceCall", args, "latest", map[string]any{"stateOverrides": overrides}), &structRes))
		require.Equal(t, uint64(26243), structRes.Gas)
		require.False(t, structRes.Failed)
		require.Equal(t, jsonExtractOutput, structRes.ReturnValue)
		require.Len(t, structRes.StructLogs, 20)
		require.JSONEq(t, `{"pc":37,"op":"CALL","gas":178280,"gasCost":175535,"depth":1,"stack":["0x0","0x0","0x84","0x0","0x0","0x1003","0x2b868"]}`, string(structRes.StructLogs[11]))

		// no override: plain transfer
		res = requireRPCResult(t, port, "debug_traceCall", args, "latest", map[string]any{"tracer": "callTracer"})
		require.JSONEq(t, fmt.Sprintf(`{"from":"%s","gas":"0x30d40","gasUsed":"0x587a","to":"%s","input":"%s","value":"0x0","type":"CALL"}`, from, ovLower, in), string(res))

		// balance override on an unfunded sender
		poor := common.HexToAddress("0x00000000000000000000000000000000000e0001")
		tgt := common.HexToAddress("0x00000000000000000000000000000000000f0001")
		valueArgs := map[string]any{"from": poor.Hex(), "to": tgt.Hex(), "value": "0x64"}
		res = requireRPCResult(t, port, "debug_traceCall", valueArgs, "latest", map[string]any{
			"tracer": "prestateTracer", "stateOverrides": map[string]any{poor.Hex(): map[string]any{"balance": "0xde0b6b3a7640000"}},
		})
		// empty accounts are omitted (go-ethereum v1.17.7)
		require.JSONEq(t, `{"0x00000000000000000000000000000000000e0001":{"balance":"0xde0b6b3a7640000"}}`, string(res))
		res = requireRPCResult(t, port, "debug_traceCall", valueArgs, "latest", map[string]any{"tracer": "callTracer"})
		require.JSONEq(t, `{"error":"insufficient funds for gas * price + value: address 0x00000000000000000000000000000000000E0001 have 0 want 100",
			"from":"0x00000000000000000000000000000000000E0001","gas":"0x989680","gasUsed":"0x0","input":"0x",
			"to":"0x00000000000000000000000000000000000F0001","type":"CALL","value":"0x64"}`, string(res))
	})
}
