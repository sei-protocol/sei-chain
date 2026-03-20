package tests

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type RpcResponse struct {
	Jsonrpc string                     `json:"jsonrpc"`
	Id      int                        `json:"id"`
	Result  evmrpc.StateAccessResponse `json:"result"`
}

func mockStatesFromBlockJson(ctx sdk.Context, blockNum uint64, a *app.App, client *MockClient) int64 {
	filepath := fmt.Sprintf("mock_data/blocks/%d.json", blockNum)
	return mockStatesFromJsonFile(ctx, filepath, a, client)
}

func mockStatesFromTxJson(ctx sdk.Context, hash string, a *app.App, client *MockClient) int64 {
	filepath := fmt.Sprintf("mock_data/transactions/%s.json", hash)
	return mockStatesFromJsonFile(ctx, filepath, a, client)
}

func mockStatesFromJsonFile(ctx sdk.Context, filepath string, a *app.App, client *MockClient) int64 {
	file, err := os.Open(filepath) //nolint:gosec
	check(err)
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	check(err)
	response := &RpcResponse{}
	err = json.Unmarshal(data, &response)
	check(err)
	blockNum := mockTendermintStateFromJson(response.Result.TendermintState, client)
	ctx = ctx.WithBlockHeight(blockNum)
	mockStateFromJson(ctx, a, response.Result.AppState)
	mockReceipt(ctx, a, response.Result.Receipt)
	return blockNum
}

func mockTendermintStateFromJson(tmStateRaw json.RawMessage, client *MockClient) int64 {
	client.mockedBlockResults = map[int64]*coretypes.ResultBlock{}
	client.mockedBlockByHashResults = map[string]*coretypes.ResultBlock{}
	client.mockedBlockResultsResults = map[int64]*coretypes.ResultBlockResults{}
	client.mockedValidators = map[int64]*coretypes.ResultValidators{}

	typed := &evmrpc.TendermintTraces{}
	err := json.Unmarshal(tmStateRaw, &typed)
	check(err)
	for _, trace := range typed.Traces {
		switch trace.Endpoint {
		case "Block":
			blockNum := parseInt64(trace.Arguments[0])
			client.mockedBlockResults[blockNum] = &coretypes.ResultBlock{}
			err = json.Unmarshal(trace.Response, client.mockedBlockResults[blockNum])
			check(err)
		case "BlockByHash":
			client.mockedBlockByHashResults[trace.Arguments[0]] = &coretypes.ResultBlock{}
			err = json.Unmarshal(trace.Response, client.mockedBlockByHashResults[trace.Arguments[0]])
			check(err)
		case "BlockResults":
			blockNum := parseInt64(trace.Arguments[0])
			client.mockedBlockResultsResults[blockNum] = &coretypes.ResultBlockResults{}
			err = json.Unmarshal(trace.Response, client.mockedBlockResultsResults[blockNum])
			check(err)
		case "Validators":
			blockNum := parseInt64(trace.Arguments[0])
			client.mockedValidators[blockNum] = &coretypes.ResultValidators{}
			err = json.Unmarshal(trace.Response, client.mockedValidators[blockNum])
			check(err)
		case "Genesis":
			gen := &coretypes.ResultGenesis{}
			err = json.Unmarshal(trace.Response, gen)
			check(err)
			client.mockedGenesis = gen
		}
	}
	blockNum, err := strconv.ParseInt(typed.Traces[0].Arguments[0], 10, 64)
	check(err)
	return blockNum
}

func mockStateFromJson(ctx sdk.Context, a *app.App, stateRaw json.RawMessage) {
	typed := map[string]interface{}{}
	err := json.Unmarshal(stateRaw, &typed)
	check(err)
	typed = typed["modules"].(map[string]interface{})
	// initialize WASM code
	if wasmModule, ok := typed["wasm"]; ok {
		for key, val := range wasmModule.(map[string]interface{})["reads"].(map[string]interface{}) {
			if key[:2] == "01" {
				codeIDBz, err := hex.DecodeString(key[2:])
				check(err)
				codeID := binary.BigEndian.Uint64(codeIDBz)
				code, err := os.ReadFile(fmt.Sprintf("mock_data/%d.code", codeID))
				check(err)
				codeInfo := &wasmtypes.CodeInfo{}
				valBz, err := hex.DecodeString(val.(string))
				check(err)
				a.AppCodec().MustUnmarshal(valBz, codeInfo)
				err = a.WasmKeeper.ImportCode(ctx, codeID, *codeInfo, code)
				check(err)
			}
		}
	}
	for moduleName, data := range typed {
		if moduleName == "evm_transient" {
			continue
		}
		var storeKey sdk.StoreKey
		kvStoreKey := a.GetKey(moduleName)
		if kvStoreKey != nil {
			storeKey = kvStoreKey
		} else {
			storeKey = a.GetMemKey(moduleName)
		}
		store := ctx.KVStore(storeKey)
		typedData := data.(map[string]interface{})
		for _, key := range typedData["has"].([]interface{}) {
			bz, err := hex.DecodeString(key.(string))
			check(err)
			store.Set(bz, []byte{1})
		}
		for key, value := range typedData["reads"].(map[string]interface{}) {
			if value == "" {
				continue
			}
			kbz, err := hex.DecodeString(key)
			check(err)
			vbz, err := hex.DecodeString(value.(string))
			check(err)
			store.Set(kbz, vbz)
		}
	}
}

func mockReceipt(ctx sdk.Context, a *app.App, receipts json.RawMessage) {
	traces := &evmrpc.ReceiptTraces{}
	err := json.Unmarshal(receipts, traces)
	check(err)
	for _, parsed := range traces.Traces {
		typed := types.Receipt{
			TxType:            uint32(parsed.Type), //nolint:gosec
			CumulativeGasUsed: uint64(parsed.CumulativeGasUsed),
			TxHashHex:         parsed.TransactionHash.Hex(),
			GasUsed:           uint64(parsed.GasUsed),
			EffectiveGasPrice: parsed.EffectiveGasPrice.ToInt().Uint64(),
			BlockNumber:       uint64(parsed.BlockNumber),
			TransactionIndex:  uint32(parsed.TransactionIndex), //nolint:gosec
			Status:            uint32(parsed.Status),           //nolint:gosec
			From:              parsed.From.Hex(),
		}
		if parsed.ContractAddress != nil {
			typed.ContractAddress = parsed.ContractAddress.Hex()
		}
		if parsed.To != nil {
			typed.To = parsed.To.Hex()
		}
		err = a.EvmKeeper.MockReceipt(ctx, parsed.TransactionHash, &typed)
		check(err)
	}
}

func parseInt64(arg string) int64 {
	if arg == "" {
		return -1
	}
	parsed, err := strconv.ParseInt(arg, 10, 64)
	check(err)
	return parsed
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}
