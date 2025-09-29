package ibctesting

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

	wasmd "github.com/CosmWasm/wasmd/app"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/golang/protobuf/proto" //nolint
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/rand"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

var wasmIdent = []byte("\x00\x61\x73\x6D")

// SeedNewContractInstance stores some wasm code and instantiates a new contract on this chain.
// This method can be called to prepare the store with some valid CodeInfo and ContractInfo. The returned
// Address is the contract address for this instance. Test should make use of this data and/or use NewIBCContractMockWasmer
// for using a contract mock in Go.
func (chain *TestChain) SeedNewContractInstance() sdk.AccAddress {
	pInstResp := chain.StoreCode(append(wasmIdent, rand.Bytes(10)...))
	codeID := pInstResp.CodeID

	anyAddressStr := chain.SenderAccount.GetAddress().String()
	initMsg := []byte(fmt.Sprintf(`{"verifier": %q, "beneficiary": %q}`, anyAddressStr, anyAddressStr))
	return chain.InstantiateContract(codeID, initMsg)
}

func (chain *TestChain) StoreCodeFile(filename string) types.MsgStoreCodeResponse {
	wasmCode, err := ioutil.ReadFile(filename)
	require.NoError(chain.t, err)
	if strings.HasSuffix(filename, "wasm") { // compress for gas limit
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, err := gz.Write(wasmCode)
		require.NoError(chain.t, err)
		err = gz.Close()
		require.NoError(chain.t, err)
		wasmCode = buf.Bytes()
	}
	return chain.StoreCode(wasmCode)
}

func (chain *TestChain) StoreCode(byteCode []byte) types.MsgStoreCodeResponse {
	storeMsg := &types.MsgStoreCode{
		Sender:       chain.SenderAccount.GetAddress().String(),
		WASMByteCode: byteCode,
	}
	r, err := chain.SendMsgs(storeMsg)
	require.NoError(chain.t, err)
	protoResult := chain.parseSDKResultData(r)
	require.Len(chain.t, protoResult.Data, 1)
	// unmarshal protobuf response from data
	var pInstResp types.MsgStoreCodeResponse
	require.NoError(chain.t, pInstResp.Unmarshal(protoResult.Data[0].Data))
	require.NotEmpty(chain.t, pInstResp.CodeID)
	return pInstResp
}

func (chain *TestChain) InstantiateContract(codeID uint64, initMsg []byte) sdk.AccAddress {
	instantiateMsg := &types.MsgInstantiateContract{
		Sender: chain.SenderAccount.GetAddress().String(),
		Admin:  chain.SenderAccount.GetAddress().String(),
		CodeID: codeID,
		Label:  "ibc-test",
		Msg:    initMsg,
		Funds:  sdk.Coins{TestCoin},
	}

	r, err := chain.SendMsgs(instantiateMsg)
	require.NoError(chain.t, err)
	protoResult := chain.parseSDKResultData(r)
	require.Len(chain.t, protoResult.Data, 1)

	var pExecResp types.MsgInstantiateContractResponse
	require.NoError(chain.t, pExecResp.Unmarshal(protoResult.Data[0].Data))
	a, err := sdk.AccAddressFromBech32(pExecResp.Address)
	require.NoError(chain.t, err)
	return a
}

// SmartQuery This will serialize the query message and submit it to the contract.
// The response is parsed into the provided interface.
// Usage: SmartQuery(addr, QueryMsg{Foo: 1}, &response)
func (chain *TestChain) SmartQuery(contractAddr string, queryMsg interface{}, response interface{}) error {
	msg, err := json.Marshal(queryMsg)
	if err != nil {
		return err
	}

	req := types.QuerySmartContractStateRequest{
		Address:   contractAddr,
		QueryData: msg,
	}
	reqBin, err := proto.Marshal(&req)
	if err != nil {
		return err
	}

	// TODO: what is the query?
	res, _ := chain.App.Query(context.Background(), &abci.RequestQuery{
		Path: "/cosmwasm.wasm.v1.Query/SmartContractState",
		Data: reqBin,
	})

	if res.Code != 0 {
		return fmt.Errorf("query failed: (%d) %s", res.Code, res.Log)
	}

	// unpack protobuf
	var resp types.QuerySmartContractStateResponse
	err = proto.Unmarshal(res.Value, &resp)
	if err != nil {
		return err
	}
	// unpack json content
	return json.Unmarshal(resp.Data, response)
}

func (chain *TestChain) parseSDKResultData(r *sdk.Result) sdk.TxMsgData {
	var protoResult sdk.TxMsgData
	require.NoError(chain.t, proto.Unmarshal(r.Data, &protoResult))
	return protoResult
}

// ContractInfo is a helper function to returns the ContractInfo for the given contract address
func (chain *TestChain) ContractInfo(contractAddr sdk.AccAddress) *types.ContractInfo {
	type testSupporter interface {
		TestSupport() *wasmd.TestSupport
	}
	return chain.App.(testSupporter).TestSupport().WasmKeeper().GetContractInfo(chain.GetContext(), contractAddr)
}
