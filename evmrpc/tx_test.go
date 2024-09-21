package evmrpc_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/go-bip39"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestGetTxReceipt(t *testing.T) {
	testGetTxReceipt(t, "eth")
}

func testGetTxReceipt(t *testing.T, namespace string) {
	receipt, err := EVMKeeper.GetReceipt(Ctx, common.HexToHash("0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e"))
	require.Nil(t, err)
	receipt.To = ""
	EVMKeeper.MockReceipt(Ctx, common.HexToHash("0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e"), receipt)

	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_getTransactionReceipt\",\"params\":[\"0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e\"],\"id\":\"test\"}", namespace)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	resObj = resObj["result"].(map[string]interface{})
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", resObj["blockHash"].(string))
	require.Equal(t, "0x8", resObj["blockNumber"].(string))
	require.Equal(t, "0x7b", resObj["cumulativeGasUsed"].(string))
	require.Equal(t, "0xa", resObj["effectiveGasPrice"].(string))
	require.Equal(t, "0x1234567890123456789012345678901234567890", resObj["from"].(string))
	require.Equal(t, "0x37", resObj["gasUsed"].(string))
	logs := resObj["logs"].([]interface{})
	require.Equal(t, 1, len(logs))
	log := logs[0].(map[string]interface{})
	require.Equal(t, "0x1111111111111111111111111111111111111111", log["address"].(string))
	topics := log["topics"].([]interface{})
	require.Equal(t, 2, len(topics))
	require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111111", topics[0].(string))
	require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111112", topics[1].(string))
	require.Equal(t, "0x8", log["blockNumber"].(string))
	require.Equal(t, "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e", log["transactionHash"].(string))
	require.Equal(t, "0x0", log["transactionIndex"].(string))
	require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", log["blockHash"].(string))
	require.Equal(t, "0x0", log["logIndex"].(string))
	require.False(t, log["removed"].(bool))
	require.Equal(t, "0x0", resObj["status"].(string))
	require.Equal(t, nil, resObj["to"])
	require.Equal(t, "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e", resObj["transactionHash"].(string))
	require.Equal(t, "0x0", resObj["transactionIndex"].(string))
	require.Equal(t, "0x1", resObj["type"].(string))
	require.Equal(t, "0x1234567890123456789012345678901234567890", resObj["contractAddress"].(string))

	receipt, err = EVMKeeper.GetReceipt(Ctx, common.HexToHash("0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e"))
	require.Nil(t, err)
	receipt.ContractAddress = ""
	receipt.To = "0x1234567890123456789012345678901234567890"
	EVMKeeper.MockReceipt(Ctx, common.HexToHash("0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e"), receipt)
	body = "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionReceipt\",\"params\":[\"0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e\"],\"id\":\"test\"}"
	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj = map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	resObj = resObj["result"].(map[string]interface{})
	require.Nil(t, resObj["contractAddress"])
	require.Equal(t, "0x1234567890123456789012345678901234567890", resObj["to"].(string))

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err = io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj = map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	resObj = resObj["error"].(map[string]interface{})
	require.Equal(t, float64(-32000), resObj["code"].(float64))
	require.Equal(t, "error block", resObj["message"].(string))

	resObj = sendRequestGood(t, "getTransactionReceipt", common.HexToHash("0x3030303030303030303030303030303030303030303030303030303030303031"))
	require.Nil(t, resObj["result"])
}

func TestGetTransaction(t *testing.T) {
	testGetTransaction(t, "eth")
}

func testGetTransaction(t *testing.T, namespace string) {
	bodyByBlockNumberAndIndex := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_getTransactionByBlockNumberAndIndex\",\"params\":[\"0x8\",\"0x0\"],\"id\":\"test\"}", namespace)
	bodyByBlockHashAndIndex := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_getTransactionByBlockHashAndIndex\",\"params\":[\"0x0000000000000000000000000000000000000000000000000000000000000001\",\"0x0\"],\"id\":\"test\"}", namespace)
	bodyByHash := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"%s_getTransactionByHash\",\"params\":[\"0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e\"],\"id\":\"test\"}", namespace)
	for _, body := range []string{bodyByBlockNumberAndIndex, bodyByBlockHashAndIndex, bodyByHash} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		resObj = resObj["result"].(map[string]interface{})
		require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", resObj["blockHash"].(string))
		require.Equal(t, "0x8", resObj["blockNumber"].(string))
		require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", resObj["from"].(string))
		require.Equal(t, "0x3e8", resObj["gas"].(string))
		require.Equal(t, "0xa", resObj["gasPrice"].(string))
		require.Equal(t, "0xa", resObj["maxFeePerGas"].(string))
		require.Equal(t, "0x0", resObj["maxPriorityFeePerGas"].(string))
		require.Equal(t, "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e", resObj["hash"].(string))
		require.Equal(t, "0x616263", resObj["input"].(string))
		require.Equal(t, "0x1", resObj["nonce"].(string))
		require.Equal(t, "0x0000000000000000000000000000000000010203", resObj["to"].(string))
		require.Equal(t, "0x0", resObj["transactionIndex"].(string))
		require.Equal(t, "0x3e8", resObj["value"].(string))
		require.Equal(t, "0x2", resObj["type"].(string))
		require.Equal(t, 0, len(resObj["accessList"].([]interface{})))
		require.Equal(t, "0xae3f3", resObj["chainId"].(string))
		require.Equal(t, "0x0", resObj["v"].(string))
		require.Equal(t, "0xa1ac0e5b8202742e54ae7af350ed855313cc4f9861c2d75a0e541b4aff7c981e", resObj["r"].(string))
		require.Equal(t, "0x288b16881aed9640cd360403b9db1ce3961b29af0b00158311856d1446670996", resObj["s"].(string))
		require.Equal(t, "0x0", resObj["yParity"].(string))
	}

	for _, body := range []string{bodyByBlockNumberAndIndex, bodyByBlockHashAndIndex, bodyByHash} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		require.Nil(t, resObj["result"])
	}
}

func TestGetPendingTransactionByHash(t *testing.T) {
	resObj := sendRequestGood(t, "getTransactionByHash", "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e")
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, "0x1", result["nonce"])
	require.Equal(t, "0x2", result["type"].(string))
}

func TestGetTransactionCount(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	// happy path
	bodyByNumber := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x1234567890123456789012345678901234567890\",\"0x8\"],\"id\":\"test\"}"
	bodyByHash := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x1234567890123456789012345678901234567890\",\"0x3030303030303030303030303030303030303030303030303030303030303031\"],\"id\":\"test\"}"

	for _, body := range []string{bodyByNumber, bodyByHash} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		count := resObj["result"].(string)
		require.Equal(t, "0x1", count)
	}

	// address that doesn't have tx
	strangerBody := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x0123456789012345678902345678901234567891\",\"0x8\"],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(strangerBody))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	resObj := map[string]interface{}{}
	require.Nil(t, json.Unmarshal(resBody, &resObj))
	count := resObj["result"].(string)
	require.Equal(t, "0x0", count) // no tx

	// error cases
	earliestBodyToBadPort := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"0x1234567890123456789012345678901234567890\",\"earliest\"],\"id\":\"test\"}"
	for body, errStr := range map[string]string{
		earliestBodyToBadPort: "error genesis",
	} {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestBadPort), strings.NewReader(body))
		require.Nil(t, err)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.Nil(t, err)
		resBody, err := io.ReadAll(res.Body)
		require.Nil(t, err)
		resObj := map[string]interface{}{}
		require.Nil(t, json.Unmarshal(resBody, &resObj))
		errMap := resObj["error"].(map[string]interface{})
		errMsg := errMap["message"].(string)
		require.Equal(t, errStr, errMsg)
	}
	Ctx = Ctx.WithBlockHeight(8)
}

func TestGetTransactionError(t *testing.T) {
	h := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	EVMKeeper.MockReceipt(Ctx, h, &types.Receipt{VmError: "test error"})
	resObj := sendRequestGood(t, "getTransactionErrorByHash", "0x1111111111111111111111111111111111111111111111111111111111111111")
	require.Equal(t, "test error", resObj["result"])

	resObj = sendRequestBad(t, "getTransactionReceipt", "0x1111111111111111111111111111111111111111111111111111111111111111")
	require.Equal(t, "error block", resObj["error"].(map[string]interface{})["message"])
}

func TestSign(t *testing.T) {
	homeDir := t.TempDir()
	txApi := evmrpc.NewTransactionAPI(nil, nil, nil, nil, homeDir, evmrpc.ConnectionTypeHTTP)
	infoApi := evmrpc.NewInfoAPI(nil, nil, nil, nil, homeDir, 1024, evmrpc.ConnectionTypeHTTP)
	clientCtx := client.Context{}.WithViper("").WithHomeDir(homeDir)
	clientCtx, err := config.ReadFromClientConfig(clientCtx)
	require.Nil(t, err)
	kb, err := client.NewKeyringFromBackend(clientCtx, keyring.BackendTest)
	require.Nil(t, err)
	entropySeed, err := bip39.NewEntropy(256)
	require.Nil(t, err)
	mnemonic, err := bip39.NewMnemonic(entropySeed)
	require.Nil(t, err)
	algos, _ := kb.SupportedAlgorithms()
	algo, err := keyring.NewSigningAlgoFromString(string(hd.Secp256k1Type), algos)
	require.Nil(t, err)
	_, err = kb.NewAccount("test", mnemonic, "", hd.CreateHDPath(sdk.GetConfig().GetCoinType(), 0, 0).String(), algo)
	require.Nil(t, err)
	accounts, _ := infoApi.Accounts()
	account := accounts[0]
	signed, err := txApi.Sign(account, []byte("data"))
	require.Nil(t, err)
	require.NotEmpty(t, signed)
}

func TestGetVMError(t *testing.T) {
	resObj := sendRequestGood(t, "getVMError", "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872e")
	require.Equal(t, "", resObj["result"].(string))
	resObj = sendRequestGood(t, "getVMError", "0xf02362077ac075a397344172496b28e913ce5294879d811bb0269b3be20a872f")
	require.Equal(t, "not found", resObj["error"].(map[string]interface{})["message"])
}
